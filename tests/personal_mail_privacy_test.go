package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/mailabuse"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestPersonalMailboxPrivacyAcrossMailAndLoop(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	pw := "private inbox meadow lantern 9847"
	stamp := time.Now().UnixNano()
	owner, err := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("private-owner-%d@example.com", stamp), "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	member, err := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("private-member-%d@example.com", stamp), "Member", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Personal mail boundary", OwnerEmail: owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	if err = queries.GrantWorkspaceMember(ctx, q.GrantWorkspaceMemberParams{Scope: w.ID, Subject: member.Subject, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	personal, err := queries.ReservePersonalMailbox(ctx, q.ReservePersonalMailboxParams{WorkspaceID: w.ID, Name: "Personal", Address: fmt.Sprintf("personal-%d@example.com", stamp)})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Shared", Address: fmt.Sprintf("shared-%d@example.com", stamp)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, "DELETE FROM correspondence_link WHERE contact_id IN (SELECT id FROM contact WHERE workspace_id=$1)", w.ID)
		pool.Exec(ctx, "DELETE FROM contact WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)", w.ID)
		pool.Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		pool.Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, member.Subject)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	var itemID string
	if err = pool.QueryRow(ctx, "INSERT INTO mail_item(mailbox_id,author_id,folder,from_address,to_address,subject,text_body,status) VALUES($1,'','inbox','sender@example.com',$2,'Private secret','personal contents','received') RETURNING id", personal.ID, personal.Address).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s = %d want %d", method, path, res.StatusCode, want)
		}
	}
	login := func(email string) {
		status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	}
	logout := func() { status("POST", "/api/v1/auth/logout", nil, nil, 204) }
	base := "/api/v1/workspaces/" + w.ID
	messages := base + "/mailboxes/" + personal.ID + "/messages"
	login(owner.Email)
	var boxes schema.MailboxList
	status("GET", base+"/mailboxes", nil, &boxes, 200)
	if len(boxes.Items) != 1 || boxes.Items[0].ID != shared.ID {
		t.Fatalf("pending inbox leaked: %+v", boxes)
	}
	status("GET", messages, nil, nil, 404)
	if _, err = queries.AssignPersonalMailbox(ctx, q.AssignPersonalMailboxParams{ID: personal.ID, WorkspaceID: w.ID, Subject: &member.Subject}); err != nil {
		t.Fatal(err)
	}
	var contact schema.Contact
	status("POST", base+"/contacts", schema.ContactInput{Name: "Sender", Email: "sender@example.com"}, &contact, 201)
	loop := base + "/contacts/" + contact.ID + "/loop"
	var timeline schema.CorrespondenceTimeline
	status("GET", loop, nil, &timeline, 200)
	if len(timeline.Items) != 0 {
		t.Fatal("administrator could read personal mail through Loop")
	}
	status("GET", messages+"/"+itemID, nil, nil, 404)
	status("PATCH", messages+"/"+itemID, schema.MailFlags{}, nil, 404)
	status("GET", messages+"/"+itemID+"/thread", nil, nil, 404)
	status("GET", messages+"/"+itemID+"/attachments/00000000-0000-4000-8000-000000000099", nil, nil, 404)
	status("GET", base+"/resources/mail/"+itemID+"/labels", nil, nil, 404)
	status("POST", messages, schema.DraftInput{}, nil, 404)
	status("POST", messages+"/"+itemID+"/send", schema.SendMailInput{}, nil, 404)

	if _, err = mailabuse.View(ctx, itemID, owner.Subject); err == nil {
		t.Fatal("screening reader bypassed ownership")
	}
	if err = mailabuse.Review(ctx, w.ID, itemID, owner.Subject, "report-spam"); err == nil {
		t.Fatal("screening review bypassed ownership")
	}
	logout()
	login(member.Email)
	status("GET", base+"/mailboxes", nil, &boxes, 200)
	if len(boxes.Items) != 2 {
		t.Fatalf("owner missing private inbox: %+v", boxes)
	}
	status("GET", messages+"/"+itemID, nil, nil, 200)
	status("GET", loop, nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != itemID {
		t.Fatal("mail owner missing Loop history")
	}
	// Removing membership revokes even the retained owner grant.
	if err = queries.RemoveWorkspaceMember(ctx, q.RemoveWorkspaceMemberParams{Scope: w.ID, Subject: member.Subject}); err != nil {
		t.Fatal(err)
	}
	status("GET", messages+"/"+itemID, nil, nil, 404)
	status("GET", loop, nil, nil, 404)
}
