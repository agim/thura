package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"net/url"
	"testing"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestCorrespondencePersonFirst(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("timeline-%d@example.com", time.Now().UnixNano())
	password := "maple lantern violet lake 7842"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Correspondence", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Mail", Address: "team@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id=$1", box.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE id=$1", box.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM contact WHERE workspace_id=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope IN ($1,$2)", w.ID, other.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id IN ($1,$2)", w.ID, other.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, in, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s: %d want %d", method, path, r.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID + "/contacts"
	var john, jane schema.Contact
	status("POST", base, schema.ContactInput{Name: "John", Email: "john@example.com"}, &john, 201)
	org := "Charm City Networks"
	status("POST", base, schema.ContactInput{Name: "Jane", Email: "jane@example.com", Company: &org}, &jane, 201)
	insert := func(from, subject, folder, state string) {
		t.Helper()
		_, err := db.From(ctx).Exec(ctx, "INSERT INTO mail_item(author_id,mailbox_id,from_address,to_address,subject,text_body,folder,status) VALUES('',$1,$2,'team@example.com',$3,'searchable contents',$4,$5)", box.ID, from, subject, folder, state)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("John <JOHN@example.com>", "First", "inbox", "received")
	insert("john@example.com", "Second", "sent", "sent")
	insert("jane@example.com", "Jane history", "inbox", "received")
	insert(`"john@example.com" <stranger@example.com>`, "False display name", "inbox", "received")
	insert("notjohn@example.com", "Substring", "inbox", "received")
	insert("john@example.com", "Spam", "spam", "received")
	insert("john@example.com", "Trash", "trash", "received")
	insert("john@example.com", "Draft", "drafts", "draft")
	path := base + "/" + john.ID + "/loop"
	var timeline schema.CorrespondenceTimeline
	status("GET", path, nil, &timeline, 200)
	if len(timeline.Items) != 2 || timeline.Contact.Company != "" {
		t.Fatalf("personal timeline: %+v", timeline)
	}
	status("GET", path+"?scope=organization", nil, nil, 422)
	status("GET", path+"?limit=1", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.NextCursor == "" {
		t.Fatalf("first page: %+v", timeline)
	}
	first := timeline.Items[0].ID
	status("GET", path+"?limit=1&cursor="+url.QueryEscape(timeline.NextCursor), nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID == first || timeline.NextCursor != "" {
		t.Fatalf("next page: %+v", timeline)
	}
	status("GET", path+"?search=First", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].Subject != "First" {
		t.Fatalf("search: %+v", timeline)
	}
	status("PATCH", base+"/"+john.ID, schema.ContactInput{Name: "John", Email: john.Email, Company: &org}, &john, 200)
	status("GET", path+"?scope=organization", nil, &timeline, 200)
	if len(timeline.Items) != 3 {
		t.Fatalf("organization rollup: %+v", timeline)
	}
	status("GET", "/api/v1/workspaces/"+other.ID+"/contacts/"+john.ID+"/loop", nil, nil, 404)
	status("GET", path+"?cursor=invalid", nil, nil, 422)
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, nil, 404)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("GET", path, nil, nil, 401)
}
