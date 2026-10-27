package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"strings"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
	"thura/tests/testsupport"
	"time"
)

// A separate delivery address must not let an inviter receive the token of an
// arbitrary account email: only a reserved, inactive team inbox is delivered
// elsewhere, and the account it creates stays unverified for ADMIN_USERS.
func TestInvitationDeliveryAddressNeverVerifiesAnotherEmail(t *testing.T) {
	t.Setenv("AUTH_LOGIN_BURST", "100")
	t.Setenv("AUTH_SIGNIN_BURST", "100")
	srv := testsupport.StartConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	pw := "delivery granite harbor lantern 4471"
	stamp := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("delivery-owner-%d@example.com", stamp)
	owner, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Delivery", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	domain := fmt.Sprintf("delivery-%d.example", stamp)
	d, err := queries.AddTeamDomain(ctx, q.AddTeamDomainParams{WorkspaceID: w.ID, Name: domain, Challenge: "fixture", Mail: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE team_domain SET status='verified',verification='administrator' WHERE id=$1", d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = queries.BindTeamDomain(ctx, q.BindTeamDomainParams{Name: domain, WorkspaceID: w.ID, DomainID: d.ID}); err != nil {
		t.Fatal(err)
	}
	teamAddress := "new-person@" + domain
	external := fmt.Sprintf("external-%d@outside.example", stamp)
	legacy := fmt.Sprintf("legacy-%d@outside.example", stamp)
	inviterInbox := fmt.Sprintf("inviter-inbox-%d@outside.example", stamp)
	t.Setenv("ADMIN_USERS", teamAddress+","+legacy)
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM invitation WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM team_domain_binding WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM team_domain WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		for _, email := range []string{teamAddress, legacy} {
			if profile, e := auth.From(ctx).ProfileByEmail(ctx, email); e == nil {
				auth.From(ctx).DeleteUser(ctx, profile.Subject)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, in, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s=%d want %d", method, path, r.StatusCode, want)
		}
	}
	token := func(to string) string {
		t.Helper()
		msg, err := mail.From(ctx).WaitFor(ctx, to, "Invitation", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Text != nil {
			for _, word := range strings.Fields(*msg.Text) {
				if i := strings.Index(word, "/invite?token="); i >= 0 {
					return word[i+len("/invite?token="):]
				}
			}
		}
		t.Fatalf("invitation mail to %s missing link", to)
		return ""
	}
	verified := func(email string) bool {
		t.Helper()
		profile, err := auth.From(ctx).ProfileByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		return profile.Verified()
	}
	administrator := func() bool {
		t.Helper()
		var st schema.SetupStatus
		status("GET", "/api/v1/setup/status", nil, &st, 200)
		return st.Administrator
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: ownerEmail, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID
	// Without a team inbox the token goes to the invited address itself.
	status("POST", base+"/invitations", schema.InviteInput{Email: legacy, DeliveryEmail: &inviterInbox, Role: schema.WorkspaceRoleMember}, nil, 422)
	// An external account email with its own new team inbox is reachable too.
	status("POST", base+"/invitations", schema.InviteInput{Email: external, MailboxAddress: &teamAddress, DeliveryEmail: &inviterInbox, Role: schema.WorkspaceRoleMember}, nil, 422)
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM (SELECT 1 FROM invitation WHERE workspace_id=$1 UNION ALL SELECT 1 FROM mailbox WHERE workspace_id=$1) r", w.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("refused invitations left an invitation or address reservation")
	}
	if _, err = mail.From(ctx).WaitFor(ctx, inviterInbox, "Invitation", 500*time.Millisecond); err == nil {
		t.Fatal("refused invitation was delivered")
	}
	// Only the inactive team inbox itself is activated through another address.
	status("POST", base+"/invitations", schema.InviteInput{Email: teamAddress, DeliveryEmail: &inviterInbox, Role: schema.WorkspaceRoleMember}, nil, 201)
	teamToken := token(inviterInbox)
	status("POST", base+"/invitations", schema.InviteInput{Email: legacy, Role: schema.WorkspaceRoleMember}, nil, 201)
	legacyToken := token(legacy)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	name := "New member"
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: teamToken, Name: &name, Password: &pw}, nil, 200)
	if verified(teamAddress) {
		t.Fatal("account activated through a separate delivery address was marked verified")
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: teamAddress, Password: pw}, nil, 200)
	if administrator() {
		t.Fatal("unverified ADMIN_USERS email became server administrator")
	}
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: legacyToken, Name: &name, Password: &pw}, nil, 200)
	if !verified(legacy) {
		t.Fatal("account activated through its own address was not marked verified")
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: legacy, Password: pw}, nil, 200)
	if !administrator() {
		t.Fatal("verified ADMIN_USERS email was refused as server administrator")
	}
}
