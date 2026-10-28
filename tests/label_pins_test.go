package tests

import (
	"testing"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/workspace"
	"thura/schema"
)

// A pinned label filters Contacts, Chat and Calendar on the server (?labelId=).
func TestLabelFilterOnContactChatAndCalendarLists(t *testing.T) {
	n := newMailNode(t, "label-filter.example")
	ctx := n.server.Context()
	pool := db.From(ctx)
	w := n.workspace.ID
	base := "/api/v1/workspaces/" + w
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	contacts := []string{insert("INSERT INTO contact(workspace_id,name,email) VALUES($1,'Ann','ann@example.com') RETURNING id", w), insert("INSERT INTO contact(workspace_id,name,email) VALUES($1,'Ben','ben@example.com') RETURNING id", w)}
	rooms := []string{}
	for _, name := range []string{"Alpha", "Beta"} {
		rooms = append(rooms, insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,created_by) VALUES($1,$2,$3,$4,$5) RETURNING id", w, "!"+uuid.NewString()+":example.com", uuid.NewString(), name, n.owner.Subject))
	}
	cal := insert("INSERT INTO calendar(workspace_id,name,color) VALUES($1,'Team','#0066ff') RETURNING id", w)
	events := []string{}
	for _, title := range []string{"Kickoff", "Review"} {
		events = append(events, insert("INSERT INTO calendar_event(calendar_id,uid,organizer,title,all_day,start_date,end_date,time_zone) VALUES($1,$2,'owner@example.com',$3,true,'2026-10-20','2026-10-21','UTC') RETURNING id", cal, uuid.NewString(), title))
	}
	var label schema.MailLabel
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Launch"}, &label, 201)
	for source, id := range map[string]string{"contact": contacts[0], "chat": rooms[1], "calendar": events[0]} {
		n.request(t, "PATCH", base+"/resources/"+source+"/"+id+"/labels", schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 204)
	}
	var people schema.ContactList
	n.request(t, "GET", base+"/contacts?labelId="+label.ID, nil, &people, 200)
	if len(people.Items) != 1 || people.Items[0].ID != contacts[0] {
		t.Fatalf("contact filter: %d items", len(people.Items))
	}
	n.request(t, "GET", base+"/contacts", nil, &people, 200)
	if len(people.Items) != 2 {
		t.Fatal("unfiltered contacts changed")
	}
	var chats schema.ChatRoomList
	n.request(t, "GET", base+"/chat?labelId="+label.ID, nil, &chats, 200)
	if len(chats.Items) != 1 || chats.Items[0].ID != rooms[1] {
		t.Fatalf("chat filter: %d items", len(chats.Items))
	}
	var instances schema.CalendarInstances
	n.request(t, "GET", base+"/calendars/"+cal+"/instances?from=2026-10-01T00:00:00Z&to=2026-11-01T00:00:00Z&labelId="+label.ID, nil, &instances, 200)
	if len(instances.Items) != 1 || instances.Items[0].EventID != events[0] {
		t.Fatalf("calendar filter: %+v", instances.Items)
	}
	var detailed schema.CalendarEvents
	n.request(t, "GET", base+"/calendars/"+cal+"/events?labelId="+label.ID, nil, &detailed, 200)
	if len(detailed.Items) != 1 || detailed.Items[0].Event.ID != events[0] {
		t.Fatal("calendar events filter")
	}
	n.request(t, "GET", base+"/contacts?labelId=nope", nil, nil, 422)
	n.request(t, "GET", base+"/chat?labelId="+uuid.NewString(), nil, &chats, 200)
	if len(chats.Items) != 0 {
		t.Fatal("an unknown label matched rooms")
	}
}

func pinned(list schema.LabelPinList, app string) []string {
	out := []string{}
	for _, p := range list.Items {
		if p.App == app {
			out = append(out, p.Label.Name)
		}
	}
	return out
}
func same(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLabelPinsArePerMemberAndWorkspaceScoped(t *testing.T) {
	n := newMailNode(t, "pins.example")
	ctx := n.server.Context()
	const password = "pinned lantern meadow copper 5521"
	member, err := auth.From(ctx).CreateUser(ctx, "member@pins.example", "Pin member", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", member.Subject, n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	stranger, err := auth.From(ctx).CreateUser(ctx, "stranger@elsewhere.example", "Stranger", password)
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Elsewhere", OwnerEmail: stranger.Email})
	if err != nil {
		t.Fatal(err)
	}
	var foreign string
	if err = db.From(ctx).QueryRow(ctx, "INSERT INTO mail_label(workspace_id,name) VALUES($1,'Foreign') RETURNING id", other.ID).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	login := func(email, pw string) {
		t.Helper()
		n.request(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	}
	base := "/api/v1/workspaces/" + n.workspace.ID
	pins := base + "/label-pins"

	// Real person is pinned in Mail by default, without a stored row.
	var list schema.LabelPinList
	n.request(t, "GET", pins, nil, &list, 200)
	if !same(pinned(list, "mail"), "Real person") || len(list.Items) != 1 || !list.Items[0].IsDefault || list.Items[0].Label.SystemKey == nil {
		t.Fatalf("default pins: %+v", list.Items)
	}
	realPerson := list.Items[0].LabelID
	var rows int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM label_pin").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("default pin stored %d rows (%v)", rows, err)
	}

	var project, travel schema.MailLabel
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Project"}, &project, 201)
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Travel"}, &travel, 201)
	n.request(t, "POST", pins, schema.LabelPinInput{LabelID: travel.ID, App: "drive"}, &list, 200)
	if !same(pinned(list, "drive"), "Travel") || !same(pinned(list, "mail"), "Real person") {
		t.Fatalf("drive pin: %+v", list.Items)
	}
	n.request(t, "POST", pins, schema.LabelPinInput{LabelID: project.ID, App: "all"}, &list, 200)
	for _, app := range []string{"mail", "drive", "calendar", "contacts", "chat"} {
		found := false
		for _, name := range pinned(list, app) {
			found = found || name == "Project"
		}
		if !found {
			t.Fatalf("pin for all apps missing in %s", app)
		}
	}
	n.request(t, "DELETE", pins+"/chat/"+project.ID, nil, &list, 200)
	if len(pinned(list, "chat")) != 0 || !same(pinned(list, "calendar"), "Project") {
		t.Fatalf("per-app unpin of an all-apps pin: %+v", list.Items)
	}
	n.request(t, "PUT", pins+"/order", schema.LabelPinOrder{App: "mail", LabelIDs: []string{project.ID, realPerson}}, &list, 200)
	if !same(pinned(list, "mail"), "Project", "Real person") {
		t.Fatalf("reorder: %v", pinned(list, "mail"))
	}
	n.request(t, "PUT", pins+"/order", schema.LabelPinOrder{App: "chat", LabelIDs: []string{project.ID}}, nil, 422)
	n.request(t, "PUT", pins+"/order", schema.LabelPinOrder{App: "mail", LabelIDs: []string{project.ID, project.ID}}, nil, 422)
	n.request(t, "POST", pins, map[string]string{"labelId": travel.ID, "app": "photos"}, nil, 422)
	n.request(t, "DELETE", pins+"/photos/"+travel.ID, nil, nil, 404)

	// Another workspace's label, or another workspace's pins, are a 404.
	n.request(t, "POST", pins, schema.LabelPinInput{LabelID: foreign, App: "mail"}, nil, 404)
	n.request(t, "DELETE", pins+"/mail/"+foreign, nil, nil, 404)
	n.request(t, "GET", "/api/v1/workspaces/"+other.ID+"/label-pins", nil, nil, 404)
	n.request(t, "POST", "/api/v1/workspaces/"+other.ID+"/label-pins", schema.LabelPinInput{LabelID: foreign, App: "mail"}, nil, 404)

	// The system label cannot be deleted, through either label route.
	n.request(t, "DELETE", base+"/labels/"+realPerson, nil, nil, 409)
	n.request(t, "DELETE", base+"/mailboxes/"+n.box.ID+"/labels/"+realPerson, nil, nil, 409)
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "real person"}, nil, 409)

	// Another member sees only the defaults, and their unpin is theirs alone.
	login(member.Email, password)
	n.request(t, "GET", pins, nil, &list, 200)
	if !same(pinned(list, "mail"), "Real person") || len(list.Items) != 1 {
		t.Fatalf("member sees the owner's pins: %+v", list.Items)
	}
	n.request(t, "DELETE", pins+"/mail/"+realPerson, nil, &list, 200)
	if len(list.Items) != 0 {
		t.Fatalf("member unpin: %+v", list.Items)
	}
	n.request(t, "GET", pins, nil, &list, 200)
	if len(list.Items) != 0 {
		t.Fatal("member unpin did not persist")
	}
	login(n.owner.Email, "mail meadow river lantern 87344")
	n.request(t, "GET", pins, nil, &list, 200)
	if !same(pinned(list, "mail"), "Project", "Real person") || !same(pinned(list, "drive"), "Project", "Travel") && !same(pinned(list, "drive"), "Travel", "Project") {
		t.Fatalf("member's unpin changed the owner's pins: %+v", list.Items)
	}

	// Deleting a label removes its pins.
	n.request(t, "DELETE", base+"/labels/"+travel.ID, nil, nil, 204)
	n.request(t, "GET", pins, nil, &list, 200)
	for _, p := range list.Items {
		if p.LabelID == travel.ID {
			t.Fatal("deleted label still pinned")
		}
	}
}
