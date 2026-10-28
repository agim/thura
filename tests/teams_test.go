package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/internal/smip"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

type teamProviderRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *teamProviderRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req.URL.Host+" "+req.Header.Get("Authorization"))
	r.mu.Unlock()
	status, body := 200, `{"id":"fixture-provider-message","message":"Queued"}`
	if strings.Contains(req.URL.Host, "sendgrid") {
		status = 202
		body = ""
	}
	return &http.Response{StatusCode: status, Header: http.Header{"X-Message-Id": []string{"fixture-sendgrid-message"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func TestCentralAdministratorTeamsDomainsProvidersAndHostIsolation(t *testing.T) {
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("ad", 32))
	t.Setenv("JOBS_WORKERS", "0")
	t.Setenv("AUTH_LOGIN_BURST", "100")
	t.Setenv("AUTH_SIGNIN_BURST", "100")
	t.Setenv("SMIP_MAIL_ENABLED", "false")
	recorder := &teamProviderRecorder{}
	application := app.New(nil)
	start := application.OnStart
	application.OnStart = func(ctx context.Context, s *lidza.Services) error {
		if e := start(ctx, s); e != nil {
			return e
		}
		lidza.Provide[http.RoundTripper](s, recorder)
		return nil
	}
	srv := startConfigured(t, application)
	ctx := srv.Context()
	suffix := fmt.Sprint(time.Now().UnixNano())
	email := "operator-" + suffix + "@teams.example"
	password := "maple waterfall orbit cedar 62819"
	owner, e := auth.From(ctx).CreateUser(ctx, email, "Operator", password)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("ADMIN_USERS", owner.Subject)
	var accessToken string
	login := func(address string) {
		t.Helper()
		var signed struct {
			AccessToken string `json:"accessToken"`
		}
		if res := srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: address, Password: password}, &signed); res.StatusCode != 200 {
			t.Fatalf("login %d", res.StatusCode)
		}
		accessToken = signed.AccessToken
	}
	login(email)
	request := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s got %d want %d", method, path, res.StatusCode, want)
		}
	}
	var teams [2]schema.Workspace
	var domains [2]schema.TeamDomainView
	var boxes [2]schema.Mailbox
	for i := range teams {
		request("POST", "/api/v1/workspaces", schema.NewTeamInput{Name: fmt.Sprintf("Team %d", i)}, &teams[i], 201)
		base := "/api/v1/workspaces/" + teams[i].ID
		name := fmt.Sprintf("team%d-%s.example", i, suffix)
		request("POST", base+"/domains", schema.TeamDomainInput{Name: name, Web: true, Mail: true}, &domains[i], 201)
		if domains[i].Status != "verified" {
			t.Fatal("administrator assignment unexpectedly requires DNS")
		}
		key := "PUBLIC-fixture-team-key-" + fmt.Sprint(i)
		provider := []string{"mailgun", "sendgrid"}[i]
		var config schema.TeamMailView
		request("PUT", base+"/domains/"+domains[i].ID+"/mail", schema.TeamMailInput{Provider: provider, APIKey: &key, Region: "us", SmtpSecurity: teamString("starttls"), Enabled: true, Smip: false}, &config, 200)
		serialized, _ := json.Marshal(config)
		if strings.Contains(string(serialized), key) || !config.HasAPIKey || config.KeyID == "" {
			t.Fatal("mail settings exposed or lost credentials/identity")
		}
		request("POST", base+"/mailboxes", schema.TeamMailboxInput{Name: "Inbox", Address: "hello@" + name}, &boxes[i], 201)
		request("POST", base+"/team-setup/publish", schema.TeamPublishInput{PrimaryDomain: name}, nil, 200)
	}
	// Cleanup only this fixture's rows, in FK order.
	t.Cleanup(func() {
		for _, team := range teams {
			if team.ID == "" {
				continue
			}
			for _, statement := range []string{"DELETE FROM mail_delivery WHERE transmission_id IN(SELECT id FROM mail_transmission WHERE workspace_id=$1)", "DELETE FROM mail_transmission WHERE workspace_id=$1", "DELETE FROM mail_screening WHERE workspace_id=$1", "DELETE FROM mail_intake_rate WHERE workspace_id=$1", "DELETE FROM mail_item WHERE mailbox_id IN(SELECT id FROM mailbox WHERE workspace_id=$1)", "DELETE FROM mailbox WHERE workspace_id=$1", "DELETE FROM team_mail_config WHERE workspace_id=$1", "DELETE FROM team_domain_binding WHERE workspace_id=$1", "DELETE FROM team_domain WHERE workspace_id=$1", "DELETE FROM team_setup WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1::text", "DELETE FROM workspace WHERE id=$1"} {
				if _, err := db.From(ctx).Exec(ctx, statement, team.ID); err != nil {
					t.Error(err)
				}
			}
		}
		if err := auth.From(ctx).DeleteUser(ctx, owner.Subject); err != nil {
			t.Error(err)
		}
	})
	baseA := "/api/v1/workspaces/" + teams[0].ID
	baseB := "/api/v1/workspaces/" + teams[1].ID
	request("POST", baseB+"/domains", schema.TeamDomainInput{Name: domains[0].Name, Web: true}, nil, 409)
	request("POST", baseB+"/domains", schema.TeamDomainInput{Name: "smip." + domains[0].Name, Web: true}, nil, 409)
	request("POST", baseA+"/mailboxes", schema.TeamMailboxInput{Name: "Wrong", Address: "other@" + domains[1].Name}, nil, 409)
	// A user belonging to both teams still cannot use team B through team A's host.
	hostRequest := func(host, path string, want int) string {
		t.Helper()
		req, e := http.NewRequest("GET", srv.URL+path, nil)
		if e != nil {
			t.Fatal(e)
		}
		req.Host = host
		req.Header.Set("Authorization", "Bearer "+accessToken)
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("host %s %s: %d %s", host, path, res.StatusCode, raw)
		}
		return string(raw)
	}
	hostRequest(domains[0].Name, baseB+"/mailboxes", 404)
	raw := hostRequest(domains[0].Name, "/api/v1/workspaces", 200)
	if !strings.Contains(raw, teams[0].ID) || strings.Contains(raw, teams[1].ID) {
		t.Fatalf("host workspace leakage %s", raw)
	}
	hostRequest("unconfigured-"+suffix+".example", "/api/v1/workspaces", 421)
	// Provider calls use their own sealed keys, exactly once, and fixed API hosts.
	for i := range teams {
		draft, e := mailbox.CreateDraft(ctx, toQueryMailbox(boxes[i]), owner.Subject, schema.DraftInput{To: teamString("recipient@outside.example"), Subject: teamString("Team provider isolation"), Text: teamString("Fixture mail")})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = mailbox.Queue(authContext(ctx, owner.Subject), teams[i].ID, boxes[i].ID, draft.ID, schema.SendMailInput{}); e != nil {
			t.Fatal(e)
		}
		srv.Clock.Advance(11 * time.Second)
		payload, _ := json.Marshal(map[string]string{"id": draft.ID})
		if e = mailbox.Deliver(ctx, payload); e != nil {
			t.Fatal(e)
		}
		if e = mailbox.Deliver(ctx, payload); e != nil {
			t.Fatal(e)
		}
	}
	recorder.mu.Lock()
	calls := append([]string(nil), recorder.requests...)
	recorder.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("provider calls %v", calls)
	}
	if !strings.Contains(calls[0], "api.mailgun.net Basic ") || !strings.Contains(calls[1], "api.sendgrid.com Bearer PUBLIC-fixture-team-key-1") {
		t.Fatalf("wrong provider credentials %v", calls)
	}
	// Relays use per-domain secrets, not the installation or another team's key.
	_, inboundA, e := tenancy.LoadMail(ctx, teams[0].ID, domains[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	rawMail := []byte("From: sender@outside.example\r\nTo: " + boxes[0].Address + "\r\nSubject: Team inbound isolation\r\n\r\nTest message\r\n")
	stamp := strconv.FormatInt(srv.Clock.Now().Unix(), 10)
	deliveryID := "team-relay-fixture-" + suffix
	mac := hmac.New(sha256.New, []byte(inboundA.InboundSecret))
	mac.Write([]byte(stamp + "." + deliveryID + "."))
	mac.Write(rawMail)
	signature := hex.EncodeToString(mac.Sum(nil))
	received, e := mailbox.Receive(ctx, boxes[0].ID, stamp, deliveryID, signature, "", rawMail)
	if e != nil || received.MailboxID != boxes[0].ID {
		t.Fatalf("team relay failed: %v", e)
	}
	if _, e = mailbox.Receive(ctx, boxes[1].ID, stamp, deliveryID, signature, "", rawMail); e == nil {
		t.Fatal("another team's relay key was accepted")
	}
	// Disabling configuration cannot enter the global deployment sender.
	configs, e := q.New(db.From(ctx)).ListTeamMailConfigs(ctx, teams[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(ctx).Exec(ctx, "UPDATE team_mail_config SET enabled=false WHERE domain_id=$1", configs[0].DomainID); e != nil {
		t.Fatal(e)
	}
	if _, managed, e := tenancy.ResolveMail(ctx, toQueryMailbox(boxes[0])); e == nil || !managed {
		t.Fatal("disabled team config fell back")
	}
	// Workspace owners are not deployment administrators.
	memberEmail := "member-" + suffix + "@teams.example"
	member, e := auth.From(ctx).CreateUser(ctx, memberEmail, "Team owner", password)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = auth.From(ctx).DeleteUser(ctx, member.Subject) })
	memberTeam, e := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Owner-only", OwnerEmail: memberEmail})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", memberTeam.ID); err != nil {
			t.Error(err)
		}
		if _, err := db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", memberTeam.ID); err != nil {
			t.Error(err)
		}
	})
	request("POST", "/api/v1/auth/logout", nil, nil, 204)
	login(memberEmail)
	request("GET", baseA+"/team-setup", nil, nil, 403)
	request("POST", "/api/v1/workspaces", schema.NewTeamInput{Name: "Unauthorized"}, nil, 403)
}
func toQueryMailbox(m schema.Mailbox) q.Mailbox {
	return q.Mailbox{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name, Address: m.Address, ConfigPrefix: m.ConfigPrefix}
}
func authContext(ctx context.Context, subject string) context.Context {
	return auth.WithUser(ctx, &auth.User{ID: subject})
}
func teamString(s string) *string { return &s }

func TestManagedTeamSMIPIdentitiesDeliverBetweenTwoThuraServers(t *testing.T) {
	a, b := newMailNode(t, "managed-alpha.example"), newMailNode(t, "managed-beta.example")
	t.Setenv("ADMIN_USERS", a.owner.Subject+","+b.owner.Subject)
	configure := func(n *mailNode, domain string) {
		t.Helper()
		base := "/api/v1/workspaces/" + n.workspace.ID
		var d schema.TeamDomainView
		n.request(t, "POST", base+"/domains", schema.TeamDomainInput{Name: domain, Web: true, Mail: true}, &d, 201)
		key := "PUBLIC-unused-provider-key"
		n.request(t, "PUT", base+"/domains/"+d.ID+"/mail", schema.TeamMailInput{Provider: "mailgun", APIKey: &key, Region: "us", Enabled: true, Smip: true}, nil, 200)
		n.request(t, "POST", base+"/team-setup/publish", schema.TeamPublishInput{PrimaryDomain: domain}, nil, 200)
	}
	configure(a, a.config.Domain)
	configure(b, b.config.Domain)
	// A second workspace/domain shares server A but owns a separate signing key.
	third, e := workspace.Create(a.server.Context(), schema.CreateWorkspaceInput{Name: "Second team on A", OwnerEmail: a.owner.Email})
	if e != nil {
		t.Fatal(e)
	}
	alias := *a
	alias.workspace = third
	alias.config = a.config
	alias.config.Domain = "second-managed-alpha.example"
	box, e := mailbox.Provision(a.server.Context(), schema.ProvisionMailboxInput{WorkspaceID: third.ID, Name: "Second team inbox", Address: "team@" + alias.config.Domain})
	if e != nil {
		t.Fatal(e)
	}
	alias.box = box
	configure(&alias, alias.config.Domain)
	sa, sb := mailTLS(t, a, nil), mailTLS(t, b, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: sb.URL}, sb)
	mailTrust(t, b, map[string]string{a.config.Domain: sa.URL, alias.config.Domain: sa.URL}, sa)
	for _, pair := range [][2]*mailNode{{a, b}, {&alias, b}, {b, a}, {b, &alias}} {
		from, to := pair[0], pair[1]
		var draft schema.MailItem
		from.request(t, "POST", from.base(), schema.DraftInput{To: teamString(to.box.Address), Subject: teamString("Managed identity delivery"), Text: teamString("One durable team-scoped copy")}, &draft, 201)
		from.request(t, "POST", from.base()+"/"+draft.ID+"/send", schema.SendMailInput{}, nil, 200)
		from.server.Clock.Advance(11 * time.Second)
		raw, _ := json.Marshal(map[string]string{"id": draft.ID})
		if e = mailbox.Deliver(from.server.Context(), raw); e != nil {
			t.Fatal(e)
		}
		if e = mailbox.Deliver(from.server.Context(), raw); e != nil {
			t.Fatal(e)
		}
		deliveries, e := smipmail.Deliveries(from.server.Context(), draft.ID)
		if e != nil || len(deliveries) != 1 || deliveries[0].State != "accepted" || deliveries[0].Route != "smip" {
			t.Fatalf("managed SMIP failed %v %+v", e, deliveries)
		}
		var packet smip.Packet
		if e = json.Unmarshal([]byte(deliveries[0].Packet), &packet); e != nil {
			t.Fatal(e)
		}
		if packet.Envelope.From != from.config.Domain {
			t.Fatal("wrong team signing domain")
		}
	}
	var keys [2]string
	for i, n := range []*mailNode{a, &alias} {
		var view schema.TeamSetupView
		n.request(t, "GET", "/api/v1/workspaces/"+n.workspace.ID+"/team-setup", nil, &view, 200)
		keys[i] = view.Mail[0].KeyID
	}
	if keys[0] == keys[1] || keys[0] == "" {
		t.Fatal("different teams shared a SMIP identity")
	}
	// Rotation preserves historical verification material without changing domain.
	var setup schema.TeamSetupView
	a.request(t, "GET", "/api/v1/workspaces/"+a.workspace.ID+"/team-setup", nil, &setup, 200)
	var rotated schema.TeamMailView
	a.request(t, "POST", "/api/v1/workspaces/"+a.workspace.ID+"/domains/"+setup.Domains[0].ID+"/smip/rotate", nil, &rotated, 200)
	if rotated.KeyID == keys[0] {
		t.Fatal("identity did not rotate")
	}
	_, secret, e := tenancy.LoadMail(a.server.Context(), a.workspace.ID, setup.Domains[0].ID)
	if e != nil || secret.Keys[keys[0]].Public == nil {
		t.Fatal("rotation discarded historical verification key")
	}
}
