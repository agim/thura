package tests

import (
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/federation"
	"thura/internal/smip"
	"thura/schema"
)

// smipTrustRoots rebuilds the sender's frozen gateway with the peer's test
// certificate as its CA bundle, so an operator-pinned private origin can
// verify. It configures no peer and no endpoint: pairing has to do that.
func smipTrustRoots(t *testing.T, f *smipApp, targets ...*httptest.Server) {
	t.Helper()
	var bundle []byte
	for _, target := range targets {
		bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})...)
	}
	ca := filepath.Join(t.TempDir(), "peer-ca.pem")
	if err := os.WriteFile(ca, bundle, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := f.config
	cfg.CAFile = ca
	gateway, err := federation.NewGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lidza.Provide(f.server.Services, gateway)
}

// adminForm posts one admin page form as a browser does and returns the
// redirect's flash: the saved message, or the error.
func adminForm(t *testing.T, f *smipApp, action string, v url.Values) (saved, failure string) {
	t.Helper()
	request, err := http.NewRequest("POST", f.server.URL+"/admin/smip-peers/"+action, strings.NewReader(v.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	client := *f.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
		t.Fatalf("%s got %d", action, response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return location.Query().Get("saved"), location.Query().Get("error")
}
func adminPage(t *testing.T, f *smipApp) (int, string) {
	t.Helper()
	response, err := f.server.Client().Get(f.server.URL + "/admin/smip-peers")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}
func smipKeyID(f *smipApp) string { return smip.KeyID(f.private.Public().(ed25519.PublicKey)) }

// pairServers runs the operator flow on local for peer: fetch the identity
// through the pinned test origin, check the page shows the fingerprint and
// nothing secret, then confirm exactly the shown key.
func pairServers(t *testing.T, local, peer *smipApp, origin string) {
	t.Helper()
	local.login(t, local.owner)
	saved, failure := adminForm(t, local, "pair", url.Values{"domain": {peer.config.Domain}, "origin": {origin}})
	if failure != "" || !strings.Contains(saved, "Fetched the identity of "+peer.config.Domain) {
		t.Fatalf("pair: saved=%q error=%q", saved, failure)
	}
	status, page := adminPage(t, local)
	if status != 200 || !strings.Contains(page, federation.Fingerprint(smipKeyID(peer))) || !strings.Contains(page, "I verified this fingerprint") {
		t.Fatalf("proposal page %d lacks the peer fingerprint", status)
	}
	for _, secret := range []string{peer.config.Seed, local.config.Seed} {
		if strings.Contains(page, secret) {
			t.Fatal("seed rendered on the peers page")
		}
	}
	if _, failure = adminForm(t, local, "confirm", url.Values{"domain": {peer.config.Domain}, "key": {smipKeyID(peer)}}); failure == "" {
		t.Fatal("pairing stored without the verification confirmation")
	}
	if _, failure = adminForm(t, local, "confirm", url.Values{"domain": {peer.config.Domain}, "key": {strings.Repeat("0", 64)}, "verified": {"on"}}); failure == "" {
		t.Fatal("pairing stored with a fingerprint that is not the fetched one")
	}
	saved, failure = adminForm(t, local, "confirm", url.Values{"domain": {peer.config.Domain}, "key": {smipKeyID(peer)}, "verified": {"on"}})
	if failure != "" || !strings.Contains(saved, "Paired "+peer.config.Domain) {
		t.Fatalf("confirm: saved=%q error=%q", saved, failure)
	}
}

func TestSmipAdminPairingFetchConfirmSendAndRevoke(t *testing.T) {
	started := time.Now().Add(-time.Second)
	a := newUnpairedSmipApp(t, "pair-a.example")
	b := newUnpairedSmipApp(t, "pair-b.example")
	t.Setenv("ADMIN_USERS", a.owner.Subject+","+b.owner.Subject)
	tlsA, tlsB := smipTLS(t, a), smipTLS(t, b)
	smipTrustRoots(t, a, tlsB)
	smipTrustRoots(t, b, tlsA)
	// The identity document is public, signed, served only over TLS 1.3 and
	// carries no seed.
	plain := a.server.JSON(t, "GET", federation.IdentityPath, nil, nil)
	if plain.StatusCode != 400 {
		t.Fatalf("identity without TLS 1.3 got %d", plain.StatusCode)
	}
	response, err := tlsA.Client().Get(tlsA.URL + federation.IdentityPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var document federation.Identity
	if response.StatusCode != 200 || json.Unmarshal(raw, &document) != nil || document.Verify(a.config.Domain, lidza.Now(a.server.Context())) != nil || strings.Contains(string(raw), a.config.Seed) {
		t.Fatalf("identity document %d: %s", response.StatusCode, raw)
	}
	// Nobody but the server administrator reaches the page or its forms.
	a.login(t, a.member)
	if status, _ := adminPage(t, a); status != 403 {
		t.Fatalf("member reached the peers page: %d", status)
	}
	stream := uuid.NewString()
	// Before pairing, b refuses a's signature and a cannot bind b.
	c := smipClient(t, tlsB, b)
	smipFailure(t, c, a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("unpaired")), 401)
	a.login(t, a.owner)
	a.request(t, "POST", a.base()+"/bindings", schema.SmipBindingInput{RequestID: uuid.NewString(), Peer: b.config.Domain, Stream: stream, Sender: "team@" + b.config.Domain, Recipient: "team@" + a.config.Domain}, nil, 422)
	// A fetched proposal alone trusts nothing.
	b.login(t, b.owner)
	if saved, failure := adminForm(t, b, "pair", url.Values{"domain": {a.config.Domain}, "origin": {tlsA.URL}}); failure != "" || saved == "" {
		t.Fatalf("b could not fetch a: %q", failure)
	}
	smipFailure(t, c, a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("pending")), 401)
	if _, failure := adminForm(t, b, "discard", url.Values{"domain": {a.config.Domain}}); failure != "" {
		t.Fatal(failure)
	}
	// Both operators pair each other through the admin flow.
	pairServers(t, b, a, tlsA.URL)
	pairServers(t, a, b, tlsB.URL)
	if _, failure := adminForm(t, a, "pair", url.Values{"domain": {b.config.Domain}, "origin": {tlsB.URL}}); !strings.Contains(failure, "already paired") {
		t.Fatalf("re-fetching a paired identity did not say so: %q", failure)
	}
	var audits int
	if err := db.From(a.server.Context()).QueryRow(a.server.Context(), "SELECT COUNT(*) FROM audit_event WHERE action IN ('smip.peer.propose','smip.peer.pair','smip.peer.discard') AND resource IN ($1,$2) AND at>=$3", "smip-peer/"+a.config.Domain, "smip-peer/"+b.config.Domain, started).Scan(&audits); err != nil || audits != 6 {
		t.Fatal("pairing was not audited", audits, err)
	}
	binding := a.binding(t, b.config.Domain, stream)
	b.binding(t, a.config.Domain, stream)
	var list schema.SmipBindingList
	a.request(t, "GET", a.base()+"/bindings", nil, &list, 200)
	if len(list.SendablePeers) != 1 || list.SendablePeers[0] != b.config.Domain {
		t.Fatal("paired endpoint is not sendable", list.SendablePeers)
	}
	// Chat and a file travel through the durable outbox to the paired peer
	// without any restart or JSON edit.
	var queued schema.SmipOutbound
	a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("paired chat")}, &queued, 200)
	smipDispatch(t, a, queued.ID)
	file := smipUpload(t, a, []byte{0, 255, 7, 19, 200})
	var copied schema.SmipOutbound
	a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, FileID: &file.ID}, &copied, 200)
	smipDispatch(t, a, copied.ID)
	var outbox schema.SmipOutboxList
	a.request(t, "GET", a.base()+"/outbox", nil, &outbox, 200)
	for _, item := range outbox.Items {
		if item.State != smip.Accepted {
			t.Fatalf("send %s is %s (%s)", item.Kind, item.State, item.Reason)
		}
	}
	var inbox schema.SmipInboxList
	b.login(t, b.owner)
	b.request(t, "GET", b.base()+"/inbox", nil, &inbox, 200)
	if len(inbox.Items) != 2 {
		t.Fatal("paired peer did not receive chat and file", len(inbox.Items))
	}
	// Revocation on b refuses a's next packet immediately; the queued send on
	// a's side blocks before any network once a revokes b.
	if saved, failure := adminForm(t, b, "revoke", url.Values{"domain": {a.config.Domain}, "confirm": {"revoke"}}); failure != "" || saved == "" {
		t.Fatalf("revoke: %q", failure)
	}
	smipFailure(t, c, a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("after revocation")), 401)
	if _, failure := adminForm(t, b, "pair", url.Values{"domain": {a.config.Domain}, "origin": {tlsA.URL}}); !strings.Contains(failure, "revoked") {
		t.Fatalf("revoked key could be proposed again: %q", failure)
	}
	a.login(t, a.owner)
	a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("queued before revocation")}, &queued, 200)
	if _, failure := adminForm(t, a, "revoke", url.Values{"domain": {b.config.Domain}, "confirm": {"revoke"}}); failure != "" {
		t.Fatal(failure)
	}
	smipDispatch(t, a, queued.ID)
	a.request(t, "GET", a.base()+"/outbox", nil, &outbox, 200)
	for _, item := range outbox.Items {
		if item.ID == queued.ID && (item.State != smip.Blocked || item.Reason != "peer_unconfigured" || item.Attempts != 0) {
			t.Fatalf("revoked peer was attempted: %s %s", item.State, item.Reason)
		}
	}
	status, page := adminPage(t, a)
	if status != 200 || !strings.Contains(page, "revoked") {
		t.Fatal("revocation is not shown")
	}
	var revoked int
	if err := db.From(a.server.Context()).QueryRow(a.server.Context(), "SELECT COUNT(*) FROM audit_event WHERE action='smip.peer.revoke' AND resource IN ($1,$2) AND at>=$3", "smip-peer/"+a.config.Domain, "smip-peer/"+b.config.Domain, started).Scan(&revoked); err != nil || revoked != 2 {
		t.Fatal("revocation audit missing", revoked, err)
	}
}
