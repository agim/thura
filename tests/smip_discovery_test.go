package tests

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	q "thura/db/queries/gen"
	"thura/internal/federation"
	"thura/internal/smip"
	"thura/schema"
)

// discoveryPair is two unpaired Thura servers that trust each other only
// through discovery: each pins the other's origin as a private deployment
// (the saved pinned origin replaces `_smip.<domain>` DNS, exactly as for
// SMIP/0.2 mail) and trusts its test certificate. No operator pairing.
type discoveryPair struct {
	a, b       *smipApp
	tlsA, tlsB *httptest.Server
	// Personal addresses at each server's SMIP domain.
	ownerA, memberB, sharedB string
}

func newDiscoveryPair(t *testing.T, label string) *discoveryPair {
	t.Helper()
	a := newUnpairedSmipApp(t, label+"-a.example")
	b := newUnpairedSmipApp(t, label+"-b.example")
	p := &discoveryPair{a: a, b: b, tlsA: smipTLS(t, a), tlsB: smipTLS(t, b)}
	smipTrustRoots(t, a, p.tlsB)
	smipTrustRoots(t, b, p.tlsA)
	ctx := a.server.Context()
	pool := db.From(ctx)
	p.ownerA, p.memberB, p.sharedB = "owner@"+a.config.Domain, "member@"+b.config.Domain, "team@"+b.config.Domain
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO smip_pinned_origin(domain,origin) VALUES($1,$2),($3,$4)", b.config.Domain, p.tlsB.URL, a.config.Domain, p.tlsA.URL)
	exec("INSERT INTO mailbox(workspace_id,name,address,config_prefix,private,owner_subject) VALUES($1,'Owner',$2,'',true,$3)", a.workspace.ID, p.ownerA, a.owner.Subject)
	exec("INSERT INTO mailbox(workspace_id,name,address,config_prefix,private,owner_subject) VALUES($1,'Member',$2,'',true,$3)", b.workspace.ID, p.memberB, b.member.Subject)
	exec("INSERT INTO mailbox(workspace_id,name,address,config_prefix) VALUES($1,'Team',$2,'')", b.workspace.ID, p.sharedB)
	// Registered after the fixtures, so this runs before their cleanup.
	t.Cleanup(func() {
		for _, w := range []string{a.workspace.ID, b.workspace.ID} {
			for _, sql := range []string{"DELETE FROM job WHERE kind LIKE 'thura.smip.dispatch%' AND payload->>'id' IN (SELECT id::text FROM smip_outbox WHERE workspace_id=$1)", "DELETE FROM smip_outbox WHERE workspace_id=$1", "DELETE FROM smip_inbox WHERE workspace_id=$1", "DELETE FROM smip_conversation WHERE workspace_id=$1", "DELETE FROM smip_policy WHERE workspace_id=$1", "DELETE FROM smip_policy_domain WHERE workspace_id=$1", "DELETE FROM smip_blocked_domain WHERE workspace_id=$1", "DELETE FROM mailbox WHERE workspace_id=$1"} {
				if _, err := pool.Exec(ctx, sql, w); err != nil {
					t.Error(err)
				}
			}
		}
		for _, d := range []string{a.config.Domain, b.config.Domain} {
			for _, sql := range []string{"DELETE FROM smip_pinned_origin WHERE domain=$1", "DELETE FROM smip_mail_domain WHERE origin=$1", "DELETE FROM audit_event WHERE resource LIKE 'smip-%'||$1::text"} {
				if _, err := pool.Exec(ctx, sql, d); err != nil {
					t.Error(err)
				}
			}
		}
	})
	return p
}

// signedIn remembers who each fixture server's client is signed in as, so
// switching users signs in only when it changes (sign-in is throttled).
var signedIn = map[*smipApp]string{}

func signIn(t *testing.T, f *smipApp, p auth.Profile) {
	t.Helper()
	if signedIn[f] == p.Subject {
		return
	}
	f.login(t, p)
	signedIn[f] = p.Subject
	t.Cleanup(func() { delete(signedIn, f) })
}

// policy saves a workspace's external sharing policy as its owner.
func (f *smipApp) policy(t *testing.T, chat, files schema.SmipSharingMode, domains ...string) {
	t.Helper()
	signIn(t, f, f.owner)
	if domains == nil {
		domains = []string{}
	}
	f.request(t, "PUT", f.base()+"/policy", schema.SmipPolicyInput{Chat: chat, Files: files, Domains: domains}, nil, 200)
}

// directPacket signs a packet from one of from's addresses to to, as the
// origin server would, for the receiver's admission checks.
func directPacket(t *testing.T, from *smipApp, sender, recipient, to, kind string, data []byte) smip.Packet {
	t.Helper()
	now := time.Now().Unix()
	e := smip.Envelope{Version: smip.Version, ID: uuid.NewString(), From: from.config.Domain, To: to, Sender: sender, Recipient: recipient, Stream: "s-" + uuid.NewString(), Kind: kind, Created: now, Expires: now + 3600, Payload: smip.Encode(data)}
	if kind == "file" {
		e.Name = smip.Encode([]byte("shared.bin"))
	}
	p, err := smip.Sign(e, from.private)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *smipApp) conversations(t *testing.T) schema.SmipConversationList {
	t.Helper()
	var out schema.SmipConversationList
	f.request(t, "GET", f.base()+"/conversations", nil, &out, 200)
	return out
}

func lastOutbound(t *testing.T, f *smipApp, id string) (string, string) {
	t.Helper()
	var state, reason string
	if err := db.From(f.server.Context()).QueryRow(f.server.Context(), "SELECT state,reason FROM smip_outbox WHERE id=$1", id).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	return state, reason
}

func TestSmipDiscoveryTrustedRequestLifecycleAndFiles(t *testing.T) {
	p := newDiscoveryPair(t, "disc-life")
	a, b := p.a, p.b
	// The safe default refuses: "Only listed domains" with an empty list.
	signIn(t, a, a.owner)
	var list schema.SmipConversationList
	a.request(t, "GET", a.base()+"/conversations", nil, &list, 200)
	if !list.Configured || list.Chat != schema.SmipSharingModeListed || list.Address != p.ownerA {
		t.Fatalf("default list %+v", list)
	}
	start := schema.SmipConversationInput{RequestID: uuid.NewString(), Address: p.memberB}
	if r := a.server.JSON(t, "POST", a.base()+"/conversations", start, nil); r.StatusCode != 403 {
		t.Fatalf("default policy allowed an external chat: %d", r.StatusCode)
	}
	// Members cannot set the policy.
	signIn(t, a, a.member)
	a.request(t, "PUT", a.base()+"/policy", schema.SmipPolicyInput{Chat: schema.SmipSharingModeAny, Files: schema.SmipSharingModeAny, Domains: []string{}}, nil, 403)
	a.request(t, "GET", a.base()+"/policy", nil, nil, 403)
	a.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeListed, strings.ToUpper(b.config.Domain))
	b.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeListed, a.config.Domain)
	var view schema.SmipPolicyView
	b.request(t, "GET", b.base()+"/policy", nil, &view, 200)
	if len(view.Domains) != 1 || view.Domains[0] != a.config.Domain {
		t.Fatal("domain list not normalized", view.Domains)
	}
	b.request(t, "PUT", b.base()+"/policy", schema.SmipPolicyInput{Chat: schema.SmipSharingModeListed, Files: schema.SmipSharingModeListed, Domains: []string{"not a host"}}, nil, 422)

	// No pairing: the partner verifies through discovery.
	signIn(t, a, a.owner)
	var started schema.SmipConversationDetail
	a.request(t, "POST", a.base()+"/conversations", start, &started, 200)
	if started.Conversation.State != federation.ConversationAccepted || started.Conversation.Verified != federation.VerifiedDiscovery || started.Conversation.RemoteAddress != p.memberB {
		t.Fatalf("started %+v", started.Conversation)
	}
	var again schema.SmipConversationDetail
	a.request(t, "POST", a.base()+"/conversations", start, &again, 200)
	if again.Conversation.ID != started.Conversation.ID {
		t.Fatal("starting twice opened two conversations")
	}
	conversation := a.base() + "/conversations/" + started.Conversation.ID
	var first schema.SmipConversationItem
	a.request(t, "POST", conversation+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), Body: smipText("hello from a")}, &first, 200)
	smipDispatch(t, a, first.ID)
	if state, reason := lastOutbound(t, a, first.ID); state != smip.Accepted {
		t.Fatalf("discovered partner not delivered: %s %s", state, reason)
	}

	// b's member sees a pending chat request; nobody else does.
	signIn(t, b, b.member)
	requests := b.conversations(t)
	if len(requests.Items) != 1 || requests.Items[0].State != federation.ConversationPending || requests.Items[0].Preview != "hello from a" || requests.Items[0].Verified != federation.VerifiedDiscovery || requests.Items[0].RemoteAddress != p.ownerA {
		t.Fatalf("request %+v", requests.Items)
	}
	request := b.base() + "/conversations/" + requests.Items[0].ID
	b.request(t, "POST", request+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), Body: smipText("too early")}, nil, 409)
	signIn(t, b, b.owner)
	if got := b.conversations(t); len(got.Items) != 0 {
		t.Fatal("another member saw a private chat request", got.Items)
	}
	b.request(t, "GET", request, nil, nil, 404)
	b.request(t, "POST", request+"/accept", nil, nil, 404)
	signIn(t, a, a.owner)
	a.request(t, "GET", "/api/v1/workspaces/"+b.workspace.ID+"/smip/conversations/"+requests.Items[0].ID, nil, nil, 404)
	a.request(t, "GET", a.base()+"/conversations/"+requests.Items[0].ID, nil, nil, 404)

	// Accept: the conversation opens and replies flow back.
	signIn(t, b, b.member)
	var accepted schema.SmipConversationDetail
	b.request(t, "POST", request+"/accept", nil, &accepted, 200)
	if accepted.Conversation.State != federation.ConversationAccepted || len(accepted.Items) != 1 {
		t.Fatalf("accept %+v", accepted)
	}
	var reply schema.SmipConversationItem
	b.request(t, "POST", request+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), Body: smipText("hi back")}, &reply, 200)
	smipDispatch(t, b, reply.ID)
	if state, reason := lastOutbound(t, b, reply.ID); state != smip.Accepted {
		t.Fatalf("reply not delivered: %s %s", state, reason)
	}
	signIn(t, a, a.owner)
	var thread schema.SmipConversationDetail
	a.request(t, "GET", conversation, nil, &thread, 200)
	if len(thread.Items) != 2 || thread.Items[1].Direction != "in" || thread.Items[1].Body != "hi back" {
		t.Fatalf("reply did not reach the started conversation: %+v", thread.Items)
	}

	// Files: an incoming share is saved to Drive once, or declined.
	for i := 0; i < 2; i++ {
		file := smipUpload(t, a, []byte{byte(i), 1, 2, 3})
		var sent schema.SmipConversationItem
		a.request(t, "POST", conversation+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), FileID: &file.ID}, &sent, 200)
		smipDispatch(t, a, sent.ID)
		if state, reason := lastOutbound(t, a, sent.ID); state != smip.Accepted {
			t.Fatalf("file not delivered: %s %s", state, reason)
		}
	}
	signIn(t, b, b.member)
	var shared schema.SmipConversationDetail
	b.request(t, "GET", request, nil, &shared, 200)
	var files []schema.SmipConversationItem
	for _, item := range shared.Items {
		if item.Kind == "file" && item.Direction == "in" {
			files = append(files, item)
		}
	}
	if len(files) != 2 || files[0].State != "pending" {
		t.Fatalf("incoming shares %+v", files)
	}
	var saved, savedAgain schema.DriveFile
	b.request(t, "POST", request+"/files/"+files[0].ID+"/save", schema.SmipImportInput{}, &saved, 200)
	b.request(t, "POST", request+"/files/"+files[0].ID+"/save", schema.SmipImportInput{}, &savedAgain, 200)
	if saved.ID == "" || savedAgain.ID != saved.ID || saved.Name != "copied.bin" {
		t.Fatal("save to Drive not idempotent", saved, savedAgain)
	}
	b.request(t, "POST", request+"/files/"+files[0].ID+"/decline", nil, nil, 409)
	b.request(t, "POST", request+"/files/"+files[1].ID+"/decline", nil, nil, 200)
	b.request(t, "POST", request+"/files/"+files[1].ID+"/save", schema.SmipImportInput{}, nil, 409)
	b.request(t, "GET", request, nil, &shared, 200)
	for _, item := range shared.Items {
		if item.ID == files[0].ID && (item.State != "saved" || item.FileID == nil || *item.FileID != saved.ID) || item.ID == files[1].ID && item.State != "declined" {
			t.Fatalf("file states %+v", item)
		}
	}
	// The stream review never shows a member's conversation.
	signIn(t, b, b.owner)
	var review schema.SmipInboxList
	b.request(t, "GET", b.base()+"/inbox", nil, &review, 200)
	if len(review.Items) != 0 {
		t.Fatal("stream review listed conversation content")
	}
	b.request(t, "POST", b.base()+"/inbox/"+files[1].ID+"/import", schema.SmipImportInput{}, nil, 404)

	// Decline: the sender is refused from then on.
	signIn(t, b, b.member)
	b.request(t, "POST", request+"/decline", nil, nil, 200)
	signIn(t, a, a.owner)
	var refused schema.SmipConversationItem
	a.request(t, "POST", conversation+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), Body: smipText("after decline")}, &refused, 200)
	smipDispatch(t, a, refused.ID)
	if state, reason := lastOutbound(t, a, refused.ID); state != smip.Blocked || reason != "peer_http_403" {
		t.Fatalf("declined sender delivered: %s %s", state, reason)
	}
	var audits int
	if err := db.From(b.server.Context()).QueryRow(b.server.Context(), "SELECT COUNT(*) FROM audit_event WHERE scope=$1 AND action IN ('smip.request.accept','smip.request.decline','smip.policy.update','smip.file.save','smip.file.decline')", b.workspace.ID).Scan(&audits); err != nil || audits != 5 {
		t.Fatal("decisions were not audited", audits, err)
	}
}

func TestSmipDiscoveryPolicyModesAndBlockedDomain(t *testing.T) {
	p := newDiscoveryPair(t, "disc-policy")
	a, b := p.a, p.b
	c := smipClient(t, p.tlsB, b)
	chat := func() smip.Packet {
		return directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("policy"))
	}
	file := func() smip.Packet {
		return directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "file", []byte{1, 2})
	}
	smipFailure(t, c, chat(), 403) // no policy saved: only listed domains, none listed
	b.policy(t, schema.SmipSharingModeOff, schema.SmipSharingModeOff, a.config.Domain)
	smipFailure(t, c, chat(), 403)
	b.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeListed, "other.example")
	smipFailure(t, c, chat(), 403)
	b.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeOff, a.config.Domain)
	if _, err := c.Send(context.Background(), chat()); err != nil {
		t.Fatal("listed domain refused", err)
	}
	smipFailure(t, c, file(), 403) // file sharing is off
	b.policy(t, schema.SmipSharingModeAny, schema.SmipSharingModeAny)
	if _, err := c.Send(context.Background(), file()); err != nil {
		t.Fatal("any verified domain refused a file", err)
	}
	// A shared address is a request every member sees.
	if _, err := c.Send(context.Background(), directPacket(t, a, p.ownerA, p.sharedB, b.config.Domain, "chat", []byte("to the team"))); err != nil {
		t.Fatal(err)
	}
	signIn(t, b, b.owner)
	got := b.conversations(t)
	if len(got.Items) != 1 || !got.Items[0].Shared || got.Items[0].LocalAddress != p.sharedB {
		t.Fatalf("shared request %+v", got.Items)
	}
	// Outbound refusals name the reason.
	b.policy(t, schema.SmipSharingModeOff, schema.SmipSharingModeOff)
	signIn(t, b, b.member)
	r := b.server.JSON(t, "POST", b.base()+"/conversations", schema.SmipConversationInput{RequestID: uuid.NewString(), Address: p.ownerA}, nil)
	if r.StatusCode != 403 {
		t.Fatalf("Off allowed an outbound chat: %d", r.StatusCode)
	}
	// A member cannot block a domain; an admin can, and it wins over "any".
	b.policy(t, schema.SmipSharingModeAny, schema.SmipSharingModeAny)
	signIn(t, b, b.member)
	member := b.conversations(t)
	var mine string
	for _, item := range member.Items {
		if !item.Shared {
			mine = item.ID
		}
	}
	b.request(t, "POST", b.base()+"/conversations/"+mine+"/block", nil, nil, 403)
	b.request(t, "POST", b.base()+"/conversations/"+mine+"/spam", nil, nil, 200)
	smipFailure(t, c, chat(), 403) // marked as spam
	signIn(t, b, b.owner)
	b.request(t, "POST", b.base()+"/conversations/"+got.Items[0].ID+"/block", nil, nil, 200)
	smipFailure(t, c, directPacket(t, a, "someone@"+a.config.Domain, p.sharedB, b.config.Domain, "chat", []byte("blocked")), 403)
	var view schema.SmipPolicyView
	b.request(t, "POST", b.base()+"/policy/unblock", schema.SmipDomainInput{Domain: a.config.Domain}, &view, 200)
	if len(view.Blocked) != 0 || len(view.Conversations) != 2 {
		t.Fatalf("unblock or overview %+v", view)
	}
	if _, err := c.Send(context.Background(), directPacket(t, a, "someone@"+a.config.Domain, p.sharedB, b.config.Domain, "chat", []byte("unblocked"))); err != nil {
		t.Fatal("unblocked domain refused", err)
	}
}

func TestSmipDiscoveryUnverifiedDowngradeAndRotation(t *testing.T) {
	p := newDiscoveryPair(t, "disc-down")
	a, b := p.a, p.b
	b.policy(t, schema.SmipSharingModeAny, schema.SmipSharingModeAny)
	c := smipClient(t, p.tlsB, b)
	// Both servers keep one clock, as synchronized deployments do: signed
	// documents carry their issuer's time.
	advance := func(d time.Duration) { a.server.Clock.Advance(d); b.server.Clock.Advance(d) }
	ctx := b.server.Context()
	queries := q.New(db.From(ctx))
	// A domain that publishes no record is unknown: 401, never accepted.
	now := time.Now()
	absent := "absent-" + uuid.NewString()[:8] + ".example"
	if err := queries.WriteSmipDomain(ctx, q.WriteSmipDomainParams{Origin: b.config.Domain, Profile: smip.Version, Domain: absent, State: "absent", Reason: "no_smip_record", CheckedAt: now, ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.From(ctx).Exec(ctx, "DELETE FROM smip_pinned_origin WHERE domain LIKE 'forged-%'"); err != nil {
			t.Error(err)
		}
	})
	seed := sha256.Sum256([]byte("PUBLIC SMIP discovery fixture " + absent))
	stranger := &smipApp{config: federation.Config{Domain: absent}, private: ed25519.NewKeyFromSeed(seed[:])}
	smipFailure(t, c, directPacket(t, stranger, "x@"+absent, p.memberB, b.config.Domain, "chat", []byte("unknown")), 401)
	// A document that does not verify for the domain is held, not accepted.
	forged := "forged-" + uuid.NewString()[:8] + ".example"
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO smip_pinned_origin(domain,origin) VALUES($1,$2)", forged, p.tlsA.URL); err != nil {
		t.Fatal(err)
	}
	forger := &smipApp{config: federation.Config{Domain: forged}, private: a.private}
	smipFailure(t, c, directPacket(t, forger, "x@"+forged, p.memberB, b.config.Domain, "chat", []byte("forged")), 503)

	// a verifies once, then loses its record: held (503), never downgraded.
	if _, err := c.Send(context.Background(), directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("verified"))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.From(ctx).Exec(ctx, "UPDATE smip_pinned_origin SET origin='https://127.0.0.1:1' WHERE domain=$1", a.config.Domain); err != nil {
		t.Fatal(err)
	}
	advance(6 * time.Minute)
	smipFailure(t, c, directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("after loss")), 503)
	row, err := queries.GetSmipDomain(ctx, q.GetSmipDomainParams{Origin: b.config.Domain, Profile: smip.Version, Domain: a.config.Domain})
	if err != nil || !row.EverVerified || row.State != "error" {
		t.Fatalf("sticky verification lost: %+v %v", row, err)
	}
	signIn(t, b, b.member)
	if r := b.server.JSON(t, "POST", b.base()+"/conversations", schema.SmipConversationInput{RequestID: uuid.NewString(), Address: p.ownerA}, nil); r.StatusCode != 503 {
		t.Fatalf("a domain that lost verification read as %d, not held", r.StatusCode)
	}
	if _, err := db.From(ctx).Exec(ctx, "UPDATE smip_pinned_origin SET origin=$2 WHERE domain=$1", a.config.Domain, p.tlsA.URL); err != nil {
		t.Fatal(err)
	}
	advance(time.Minute) // the "error" entry retries after 15 seconds
	if _, err := c.Send(context.Background(), directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("verified again"))); err != nil {
		t.Fatal("restored domain refused", err)
	}

	// Key continuity: a rotates to a new key. Until b fetches a document that
	// publishes it, the new key is refused; once the verified document
	// expires and the new one lists it, it is trusted. These are the SMIP/0.2
	// mail rules: trust is the current valid signed document.
	rotatedSeed := sha256.Sum256([]byte("PUBLIC SMIP discovery rotation " + a.config.Domain))
	rotated := ed25519.NewKeyFromSeed(rotatedSeed[:])
	cfg := a.config
	old := a.private.Public().(ed25519.PublicKey)
	cfg.Seed = smip.Encode(rotatedSeed[:])
	cfg.OriginKeys = map[string]smip.Key{smip.KeyID(old): {Public: old, NotBefore: cfg.NotBefore, NotAfter: cfg.NotAfter}}
	gateway, err := federation.NewGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lidza.Provide(a.server.Services, gateway)
	next := &smipApp{config: a.config, private: rotated}
	smipFailure(t, c, directPacket(t, next, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("unpublished key")), 401)
	advance(6 * time.Minute)
	if _, err := c.Send(context.Background(), directPacket(t, next, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("new key"))); err != nil {
		t.Fatal("rotated key published in the identity was refused", err)
	}
	if _, err := c.Send(context.Background(), directPacket(t, a, p.ownerA, p.memberB, b.config.Domain, "chat", []byte("old key still listed"))); err != nil {
		t.Fatal("retained historical key was refused", err)
	}
}

func TestSmipPinnedPartnerConversation(t *testing.T) {
	p := newDiscoveryPair(t, "disc-pin")
	a, b := p.a, p.b
	// Without the saved pinned origins, only operator pairing makes them
	// partners (a private deployment without public DNS).
	if _, err := db.From(a.server.Context()).Exec(a.server.Context(), "DELETE FROM smip_pinned_origin WHERE domain IN ($1,$2)", a.config.Domain, b.config.Domain); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ADMIN_USERS", a.owner.Subject+","+b.owner.Subject)
	pairServers(t, b, a, p.tlsA.URL)
	pairServers(t, a, b, p.tlsB.URL)
	signedIn[a], signedIn[b] = a.owner.Subject, b.owner.Subject
	a.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeListed, b.config.Domain)
	b.policy(t, schema.SmipSharingModeListed, schema.SmipSharingModeListed, a.config.Domain)
	signIn(t, a, a.owner)
	var started schema.SmipConversationDetail
	a.request(t, "POST", a.base()+"/conversations", schema.SmipConversationInput{RequestID: uuid.NewString(), Address: p.memberB}, &started, 200)
	if started.Conversation.Verified != federation.VerifiedPinned {
		t.Fatalf("pinned partner shown as %q", started.Conversation.Verified)
	}
	var sent schema.SmipConversationItem
	a.request(t, "POST", a.base()+"/conversations/"+started.Conversation.ID+"/messages", schema.SmipConversationMessageInput{TransactionID: uuid.NewString(), Body: smipText("pinned hello")}, &sent, 200)
	smipDispatch(t, a, sent.ID)
	if state, reason := lastOutbound(t, a, sent.ID); state != smip.Accepted {
		t.Fatalf("pinned partner not delivered: %s %s", state, reason)
	}
	signIn(t, b, b.member)
	if got := b.conversations(t); len(got.Items) != 1 || got.Items[0].Verified != federation.VerifiedPinned || got.Items[0].State != federation.ConversationPending {
		t.Fatalf("pinned request %+v", got.Items)
	}
}
