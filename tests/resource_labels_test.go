package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"strings"
	"testing"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestWorkspaceLabelsCreateApplyAndResourcePermissions(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("labels-%d@example.com", time.Now().UnixNano())
	password := "river forest lantern copper 4938"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Label owner", password)
	if err != nil {
		t.Fatal(err)
	}
	var workspaces []string
	t.Cleanup(func() {
		for _, w := range workspaces {
			for _, sql := range []string{
				"DELETE FROM resource_label WHERE label_id IN (SELECT id FROM mail_label WHERE workspace_id=$1)",
				"DELETE FROM mail_tag WHERE label_id IN (SELECT id FROM mail_label WHERE workspace_id=$1)",
				"DELETE FROM mail_label WHERE workspace_id=$1",
				"DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)",
				"DELETE FROM mailbox WHERE workspace_id=$1",
				"DELETE FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1)",
				"DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM chat_room WHERE workspace_id=$1",
				"DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM meeting WHERE workspace_id=$1",
				"DELETE FROM contact WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1",
			} {
				if _, e := pool.Exec(ctx, sql, w); e != nil {
					t.Error(e)
				}
			}
		}
		if e := auth.From(ctx).DeleteUser(ctx, owner.Subject); e != nil {
			t.Error(e)
		}
	})
	for i := 0; i < 2; i++ {
		w, e := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Labels", OwnerEmail: email})
		if e != nil {
			t.Fatal(e)
		}
		workspaces = append(workspaces, w.ID)
	}
	w := workspaces[0]
	base := "/api/v1/workspaces/" + w
	request := func(method, path string, in, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, in, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, r.StatusCode, want)
		}
	}
	request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w, Name: "Mail", Address: "labels@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var mail schema.MailItem
	request("POST", base+"/mailboxes/"+box.ID+"/messages", schema.DraftInput{}, &mail, 201)
	resources := map[string]string{"mail": mail.ID}
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if e := pool.QueryRow(ctx, sql, args...).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	resources["contact"] = insert("INSERT INTO contact(workspace_id,name,email) VALUES($1,'Person','person@example.com') RETURNING id", w)
	resources["document"] = insert("INSERT INTO drive_file(workspace_id,name,content_type,size) VALUES($1,'Notes.txt','text/plain',4) RETURNING id", w)
	resources["chat"] = insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,created_by) VALUES($1,$2,$3,'Shared chat',$4) RETURNING id", w, "!"+uuid.NewString()+":example.com", uuid.NewString(), owner.Subject)
	resources["meeting"] = insert("INSERT INTO meeting(workspace_id,request_id,name,created_by) VALUES($1,$2,'Meeting',$3) RETURNING id", w, uuid.NewString(), owner.Subject)
	cal := insert("INSERT INTO calendar(workspace_id,name,color) VALUES($1,'Team','#0066ff') RETURNING id", w)
	eventSQL := "INSERT INTO calendar_event(calendar_id,uid,organizer,title,all_day,start_date,end_date,time_zone) VALUES($1,$2,'owner@example.com','Event',true,'2026-10-20','2026-10-21','America/New_York') RETURNING id"
	resources["calendar"] = insert(eventSQL, cal, uuid.NewString())
	resource := func(source, id string) string { return base + "/resources/" + source + "/" + id + "/labels" }
	var label schema.MailLabel
	request("POST", resource("mail", mail.ID), schema.MailLabelInput{Name: "  Project Atlas  "}, &label, 201)
	if label.Name != "Project Atlas" || label.WorkspaceID == nil || *label.WorkspaceID != w {
		t.Fatal("new label must belong to the workspace and trim its name")
	}
	var retry schema.MailLabel
	request("POST", resource("mail", mail.ID), schema.MailLabelInput{Name: "project atlas"}, &retry, 201)
	if retry.ID != label.ID {
		t.Fatal("retry created a duplicate catalog entry")
	}
	for source, id := range resources {
		path := resource(source, id)
		request("PATCH", path, schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 204)
		var assigned schema.MailLabelList
		request("GET", path, nil, &assigned, 200)
		if len(assigned.Items) != 1 || assigned.Items[0].ID != label.ID {
			t.Fatalf("%s did not reuse the shared label", source)
		}
	}

	var driveList schema.DriveListing
	request("GET", base+"/drive", nil, &driveList, 200)
	if len(driveList.Labels) != 1 || driveList.Labels[0].FileID != resources["document"] || driveList.Labels[0].LabelID != label.ID || driveList.Labels[0].Name != label.Name {
		t.Fatal("Drive listing omitted its file label badges")
	}
	var otherDrive schema.DriveListing
	request("GET", "/api/v1/workspaces/"+workspaces[1]+"/drive", nil, &otherDrive, 200)
	if len(otherDrive.Labels) != 0 {
		t.Fatal("Drive label assignments crossed workspaces")
	}
	var catalog schema.MailLabelList
	request("GET", base+"/labels", nil, &catalog, 200)
	if len(catalog.Items) != 1 {
		t.Fatal("catalog has duplicate labels")
	}
	request("POST", base+"/labels", schema.MailLabelInput{Name: "PROJECT ATLAS"}, nil, 409)
	request("POST", resource("contact", uuid.NewString()), schema.MailLabelInput{Name: "Must not exist"}, nil, 404)
	request("POST", resource("mail", mail.ID), schema.MailLabelInput{Name: "   "}, nil, 422)
	request("POST", resource("mail", mail.ID), schema.MailLabelInput{Name: strings.Repeat("a", 41)}, nil, 422)
	foreign := "/api/v1/workspaces/" + workspaces[1]
	request("PATCH", foreign+"/resources/contact/"+resources["contact"]+"/labels", schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 404)
	foreignContact := insert("INSERT INTO contact(workspace_id,name,email) VALUES($1,'Other','other@example.com') RETURNING id", workspaces[1])
	request("PATCH", foreign+"/resources/contact/"+foreignContact+"/labels", schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 404)
	request("DELETE", foreign+"/labels/"+label.ID, nil, nil, 404)
	privateCal := insert("INSERT INTO calendar(workspace_id,name,color,owner_subject) VALUES($1,'Private','#0066ff','another-user') RETURNING id", w)
	privateEvent := insert(eventSQL, privateCal, uuid.NewString())
	privateChat := insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,direct,created_by) VALUES($1,$2,$3,'Private',true,'another-user') RETURNING id", w, "!"+uuid.NewString()+":example.com", uuid.NewString())
	for source, id := range map[string]string{"calendar": privateEvent, "chat": privateChat} {
		path := resource(source, id)
		request("GET", path, nil, nil, 404)
		request("PATCH", path, schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 404)
		request("POST", path, schema.MailLabelInput{Name: "Hidden label"}, nil, 404)
	}
	request("GET", base+"/labels", nil, &catalog, 200)
	if len(catalog.Items) != 1 {
		t.Fatal("failed requests left orphan labels")
	}
	request("DELETE", base+"/labels/"+label.ID, nil, nil, 204)
	request("GET", base+"/drive", nil, &driveList, 200)
	if len(driveList.Labels) != 0 {
		t.Fatal("deleted label remained in Drive list badges")
	}

	for source, id := range resources {
		var assigned schema.MailLabelList
		request("GET", resource(source, id), nil, &assigned, 200)
		if len(assigned.Items) != 0 {
			t.Fatalf("deleted label remained on %s", source)
		}
	}
	// Deleting a contact drops its label assignments, which have no foreign key.
	var contactLabel schema.MailLabel
	request("POST", resource("contact", resources["contact"]), schema.MailLabelInput{Name: "Removed with contact"}, &contactLabel, 201)
	request("DELETE", base+"/contacts/"+resources["contact"], nil, nil, 204)
	var stale int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM resource_label WHERE source='contact' AND source_id=$1", resources["contact"]).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("deleted contact kept %d label assignments (%v)", stale, err)
	}
	_, err = pool.Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w)
	if err != nil {
		t.Fatal(err)
	}
	request("GET", base+"/labels", nil, nil, 404)
	request("GET", resource("mail", mail.ID), nil, nil, 404)
	request("POST", resource("contact", resources["contact"]), schema.MailLabelInput{Name: "Revoked"}, nil, 404)
}
