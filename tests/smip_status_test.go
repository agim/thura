package tests

import (
	"encoding/pem"
	"net/url"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"thura/schema"
)

// The team domain's SMIP status loads after the page from its own route:
// only the server administrator reads it, it is cached for a minute, and a
// check that cannot confirm the transport says so instead of passing.
func TestTeamSmipStatusRouteAdministratorOnlyCachedAndHonest(t *testing.T) {
	n := newMailNode(t, "status-alpha.example")
	t.Setenv("ADMIN_USERS", n.owner.Subject)
	mailIdentity(t, n)
	base := "/api/v1/workspaces/" + n.workspace.ID
	var smipDomain, plainDomain schema.TeamDomainView
	n.request(t, "POST", base+"/domains", schema.TeamDomainInput{Name: n.config.Domain, Web: true, Mail: true}, &smipDomain, 201)
	n.request(t, "POST", base+"/domains", schema.TeamDomainInput{Name: "plain-" + n.config.Domain, Web: false, Mail: true}, &plainDomain, 201)
	key := "PUBLIC-unused-provider-key"
	n.request(t, "PUT", base+"/domains/"+smipDomain.ID+"/mail", schema.TeamMailInput{Provider: "mailgun", APIKey: &key, Region: "us", Enabled: true, Smip: true}, nil, 200)
	n.request(t, "PUT", base+"/domains/"+plainDomain.ID+"/mail", schema.TeamMailInput{Provider: "mailgun", APIKey: &key, Region: "us", Enabled: true, Smip: false}, nil, 200)
	// The team setup data no longer waits for the status.
	var view schema.TeamSetupView
	n.request(t, "GET", base+"/team-setup", nil, &view, 200)
	if len(view.Mail) != 2 {
		t.Fatalf("team setup: %+v", view)
	}
	// This server answers its own transport challenge over TLS 1.3 at an
	// origin pinned through the admin form, with its CA saved there too.
	listener := mailTLS(t, n, nil)
	if _, failure := mailAdminForm(t, n, "origin-pin", url.Values{"domain": {n.config.Domain}, "origin": {listener.URL}}, nil); failure != "" {
		t.Fatal(failure)
	}
	if _, failure := mailAdminForm(t, n, "ca-save", url.Values{"pem": {string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: listener.Certificate().Raw}))}}, nil); failure != "" {
		t.Fatal(failure)
	}
	path := base + "/domains/" + smipDomain.ID + "/smip/status"
	var first schema.SmipDnsStatus
	n.request(t, "GET", path, nil, &first, 200)
	if first.Domain != n.config.Domain || first.Tls != "yes" || len(first.Addresses) == 0 || first.CheckedAt.IsZero() {
		t.Fatalf("observed status: %+v", first)
	}
	// Cached for a minute: the listener going away is not seen yet.
	listener.Close()
	var cached schema.SmipDnsStatus
	n.request(t, "GET", path, nil, &cached, 200)
	if !cached.CheckedAt.Equal(first.CheckedAt) || cached.Tls != "yes" {
		t.Fatalf("cache not honoured: %+v", cached)
	}
	// After the minute the check runs again and reports the failure.
	n.server.Clock.Advance(61 * time.Second)
	var failed schema.SmipDnsStatus
	n.request(t, "GET", path, nil, &failed, 200)
	if failed.CheckedAt.Equal(first.CheckedAt) || failed.Tls != "no" || failed.Detail == "" {
		t.Fatalf("unreachable transport not reported as a failure: %+v", failed)
	}
	// A domain without SMIP delivery and an unknown domain have no status.
	n.request(t, "GET", base+"/domains/"+plainDomain.ID+"/smip/status", nil, nil, 404)
	n.request(t, "GET", base+"/domains/00000000-0000-4000-8000-0000000000ff/smip/status", nil, nil, 404)
	// A team owner who is not the server administrator is refused, and so is
	// a signed-in stranger, with the same answer as the other team-setup
	// routes (tenancy.Manage).
	ctx := n.server.Context()
	password := "status lantern meadow river 51873"
	owner, err := auth.From(ctx).CreateUser(ctx, "team-owner@"+n.config.Domain, "Team owner", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", owner.Subject, n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	stranger, err := auth.From(ctx).CreateUser(ctx, "stranger@"+n.config.Domain, "Stranger", password)
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{owner.Email, stranger.Email} {
		n.request(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
		var leaked schema.SmipDnsStatus
		if r := n.server.JSON(t, "GET", path, nil, &leaked); r.StatusCode != 403 && r.StatusCode != 404 || leaked.Domain != "" {
			t.Fatalf("%s read the SMIP status: %d %+v", email, r.StatusCode, leaked)
		}
		n.request(t, "GET", base+"/team-setup", nil, nil, 403)
	}
	n.request(t, "POST", "/api/v1/auth/logout", nil, nil, 204)
	n.request(t, "GET", path, nil, nil, 401)
}
