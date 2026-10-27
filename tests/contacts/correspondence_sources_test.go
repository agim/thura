package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"net/url"
	"testing"
	"thura/app"
	"thura/internal/workspace"
	"thura/schema"
	"thura/tests/testsupport"
	"time"
)

func TestCorrespondenceSourcesAndPermissions(t *testing.T) {
	srv := testsupport.StartConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("sources-%d@example.com", time.Now().UnixNano())
	password := "maple lantern violet lake 7842"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Sources", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	t.Cleanup(func() {
		exec("DELETE FROM contact WHERE workspace_id=$1", w.ID)
		exec("DELETE FROM share_grant WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id IN ($1,$2))", w.ID, other.ID)
		exec("DELETE FROM drive_file WHERE workspace_id IN ($1,$2)", w.ID, other.ID)
		exec("DELETE FROM chat_participant WHERE room_id IN (SELECT id FROM chat_room WHERE workspace_id=$1)", w.ID)
		exec("DELETE FROM chat_room WHERE workspace_id=$1", w.ID)
		exec("DELETE FROM meeting_participant WHERE meeting_id IN (SELECT id FROM meeting WHERE workspace_id=$1)", w.ID)
		exec("DELETE FROM meeting WHERE workspace_id=$1", w.ID)
		exec("DELETE FROM event_attendee WHERE event_id IN (SELECT e.id FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id WHERE c.workspace_id=$1)", w.ID)
		exec("DELETE FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1)", w.ID)
		exec("DELETE FROM calendar WHERE workspace_id=$1", w.ID)
		exec("DELETE FROM auth_member WHERE scope IN ($1,$2)", w.ID, other.ID)
		exec("DELETE FROM workspace WHERE id IN ($1,$2)", w.ID, other.ID)
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
	base := "/api/v1/workspaces/" + w.ID
	var person schema.Contact
	status("POST", base+"/contacts", schema.ContactInput{Name: "Person", Email: email}, &person, 201)
	path := base + "/contacts/" + person.ID + "/loop"
	file := func(space, name string) string {
		return insert("INSERT INTO drive_file(workspace_id,name,content_type,size,created_at) VALUES($1,$2,'text/plain',1,'2026-01-01')", space, name)
	}
	doc := file(w.ID, "Shared document")
	manual := file(w.ID, "Manual document")
	foreign := file(other.ID, "Foreign document")
	grant := insert("INSERT INTO share_grant(file_id,token_hash,target_email,expires_at) VALUES($1,$2,$3,now()+interval '1 day')", doc, doc, email)
	for _, mode := range []string{"expired", "revoked", "public"} {
		f := file(w.ID, mode)
		target := email
		if mode == "public" {
			target = ""
		}
		g := insert("INSERT INTO share_grant(file_id,token_hash,target_email,expires_at) VALUES($1,$2,$3,now()+interval '1 day')", f, f, target)
		if mode == "expired" {
			exec("UPDATE share_grant SET expires_at=now()-interval '1 day' WHERE id=$1", g)
		}
		if mode == "revoked" {
			exec("UPDATE share_grant SET revoked_at=now() WHERE id=$1", g)
		}
	}
	room := insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,direct,created_by,created_at) VALUES($1,$2,gen_random_uuid(),'Private conversation',true,$2,'2026-01-01')", w.ID, owner.Subject)
	exec("INSERT INTO chat_participant(room_id,subject) VALUES($1,$2)", room, owner.Subject)
	hidden := insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,direct,created_by) VALUES($1,$2,gen_random_uuid(),'Hidden conversation',true,$2)", w.ID, owner.Subject+"-hidden")
	meeting := insert("INSERT INTO meeting(workspace_id,request_id,name,created_by,created_at) VALUES($1,gen_random_uuid(),'Meeting record',$2,'2026-01-01')", w.ID, owner.Subject)
	exec("INSERT INTO meeting_participant(meeting_id,subject,token_expires_at) VALUES($1,$2,now())", meeting, owner.Subject)
	cal := insert("INSERT INTO calendar(workspace_id,name,color) VALUES($1,'Shared calendar','#15756b')", w.ID)
	private := insert("INSERT INTO calendar(workspace_id,name,color,owner_subject) VALUES($1,'Hidden calendar','#15756b','another-user')", w.ID)
	event := insert("INSERT INTO calendar_event(calendar_id,uid,organizer,title,all_day,starts_at,ends_at,time_zone) VALUES($1,'event',$2,'Planning discussion',false,'2026-01-01','2026-01-01 01:00','UTC')", cal, email)
	hiddenEvent := insert("INSERT INTO calendar_event(calendar_id,uid,organizer,title,all_day,starts_at,ends_at,time_zone) VALUES($1,'event',$2,'Private planning',false,now(),now()+interval '1 hour','UTC')", private, email)
	var timeline schema.CorrespondenceTimeline
	status("GET", path, nil, &timeline, 200)
	if len(timeline.Items) != 4 || timeline.Contact.Company != "" {
		t.Fatalf("automatic sources: %+v", timeline)
	}
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 5; i++ {
		status("GET", path+"?limit=1&cursor="+url.QueryEscape(cursor), nil, &timeline, 200)
		for _, v := range timeline.Items {
			if seen[v.ID] {
				t.Fatal("duplicate page item")
			}
			seen[v.ID] = true
		}
		cursor = timeline.NextCursor
		if cursor == "" {
			break
		}
	}
	for _, id := range []string{doc, room, meeting, event} {
		if !seen[id] {
			t.Fatalf("missing %s in mixed source paging", id)
		}
	}
	org := "Example Organization"
	status("PATCH", base+"/contacts/"+person.ID, schema.ContactInput{Name: person.Name, Email: person.Email, Company: &org}, nil, 200)
	var colleague schema.Contact
	status("POST", base+"/contacts", schema.ContactInput{Name: "Colleague", Email: "colleague@example.com", Company: &org}, &colleague, 201)
	exec("INSERT INTO event_attendee(event_id,email) VALUES($1,$2)", event, colleague.Email)
	colleaguePath := base + "/contacts/" + colleague.ID + "/loop"
	status("GET", colleaguePath, nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != event {
		t.Fatalf("attendee association: %+v", timeline)
	}
	status("PUT", colleaguePath+"/links", schema.CorrespondenceLinkInput{Source: "meeting", SourceID: meeting}, nil, 200)
	status("GET", path+"?scope=organization", nil, &timeline, 200)
	if len(timeline.Items) != 4 {
		t.Fatalf("organization must deduplicate shared sources: %+v", timeline)
	}
	status("GET", path+"?source=calendar&search=discussion", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != event {
		t.Fatalf("filtered search: %+v", timeline)
	}
	status("GET", path+"?source=unknown", nil, nil, 422)
	status("GET", path+"/sources", nil, nil, 422)
	status("GET", path+"/sources?source=chat", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != room {
		t.Fatalf("chat catalog leaked: %+v", timeline)
	}
	for _, v := range []struct{ source, id string }{{"document", foreign}, {"chat", hidden}, {"calendar", hiddenEvent}} {
		status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: v.source, SourceID: v.id}, nil, 404)
	}
	for i := 0; i < 2; i++ {
		status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: "document", SourceID: manual}, nil, 200)
	}
	status("GET", path+"?source=document", nil, &timeline, 200)
	if len(timeline.Items) != 2 {
		t.Fatalf("manual link: %+v", timeline)
	}
	status("DELETE", path+"/links/document/"+manual, nil, nil, 204)
	status("GET", path+"?source=document", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != doc {
		t.Fatalf("unlink: %+v", timeline)
	}
	// Linked sources retain their own access checks after permissions change.
	status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: "chat", SourceID: room}, nil, 200)
	exec("DELETE FROM chat_participant WHERE room_id=$1", room)
	exec("UPDATE share_grant SET revoked_at=now() WHERE id=$1", grant)
	exec("UPDATE calendar SET owner_subject='another-user' WHERE id=$1", cal)
	status("GET", path, nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].ID != meeting {
		t.Fatalf("revoked source still visible: %+v", timeline)
	}
	status("GET", base+"/chat/"+room, nil, nil, 404)
	status("GET", base+"/calendars/"+cal, nil, nil, 404)
	status("GET", base+"/drive/files/"+foreign, nil, nil, 404)
	status("GET", base+"/meet/"+meeting, nil, nil, 200)
	status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: "document", SourceID: manual}, nil, 200)
	exec("UPDATE drive_file SET trashed=true WHERE id=$1", manual)
	status("GET", path+"?source=document", nil, &timeline, 200)
	if len(timeline.Items) != 0 {
		t.Fatal("trashed source visible")
	}
	exec("DELETE FROM auth_member WHERE scope=$1", w.ID)
	status("GET", path+"/sources?source=meeting", nil, nil, 404)
	status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: "meeting", SourceID: meeting}, nil, 404)
}
