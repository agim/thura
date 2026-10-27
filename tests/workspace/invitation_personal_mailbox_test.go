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

func TestInvitationReservesPrivateInboxAndUsesReachableDelivery(t *testing.T) {
	srv := testsupport.StartConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	pw := "invitation safe meadow lantern 5682"
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("bootstrap-admin-%d@example.com", stamp)
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Bootstrap", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	domain := fmt.Sprintf("invite-%d.example", stamp)
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
	assigned := "new-person@" + domain
	delivery := fmt.Sprintf("existing-%d@external.example", stamp)
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM invitation WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM team_domain_binding WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM team_domain WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		if profile, e := auth.From(ctx).ProfileByEmail(ctx, assigned); e == nil {
			auth.From(ctx).DeleteUser(ctx, profile.Subject)
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
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID
	in := schema.InviteInput{Email: assigned, MailboxAddress: &assigned, Role: schema.WorkspaceRoleMember}
	status("POST", base+"/invitations", in, nil, 422)
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM mailbox WHERE workspace_id=$1", w.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed invitation left an address reservation")
	}
	in.DeliveryEmail = &delivery
	var invitation schema.InviteView
	status("POST", base+"/invitations", in, &invitation, 201)
	var private bool
	var subject *string
	var boxID string
	if err = pool.QueryRow(ctx, "SELECT id,private,owner_subject FROM mailbox WHERE workspace_id=$1 AND address=$2", w.ID, assigned).Scan(&boxID, &private, &subject); err != nil || !private || subject != nil {
		t.Fatal("new inbox was not reserved privately")
	}
	var visible schema.MailboxList
	status("GET", base+"/mailboxes", nil, &visible, 200)
	if len(visible.Items) != 0 {
		t.Fatal("unaccepted personal inbox leaked to owner")
	}
	msg, err := mail.From(ctx).WaitFor(ctx, delivery, "Invitation", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token := ""
	if msg.Text != nil {
		for _, word := range strings.Fields(*msg.Text) {
			if i := strings.Index(word, "/invite?token="); i >= 0 {
				token = word[i+len("/invite?token="):]
			}
		}
	}
	if token == "" {
		t.Fatal("activation did not reach the existing address")
	}
	// Competing invitations cannot allocate the address or a standard alias.
	status("POST", base+"/invitations", in, nil, 409)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	name := "New member"
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: token, Name: &name, Password: &pw}, nil, 200)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: assigned, Password: pw}, nil, 200)
	status("GET", base+"/mailboxes", nil, &visible, 200)
	if len(visible.Items) != 1 || visible.Items[0].ID != boxID || !visible.Items[0].Private || visible.Items[0].OwnerSubject == nil {
		t.Fatal("acceptance did not atomically activate the member inbox")
	}
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	status("GET", base+"/mailboxes/"+boxID+"/messages", nil, nil, 404)
}
