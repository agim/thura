package tests

import (
	"encoding/base64"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"strings"
	"testing"
	"thura/app"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestCalendarMeetingLinkPreservesLocationAndScopesAccess(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	t.Setenv("LIVEKIT_SERVER_URL", "http://127.0.0.1:7880")
	t.Setenv("LIVEKIT_PUBLIC_URL", "ws://localhost:7880")
	t.Setenv("LIVEKIT_API_KEY", "calendar-test")
	t.Setenv("LIVEKIT_API_SECRET", "synthetic-calendar-test-secret-791934")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("calendar-meet-%d@example.com", time.Now().UnixNano())
	password := "river forest lantern copper 4938"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Calendar Meet", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM event_exception WHERE event_id IN (SELECT id FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1))", "DELETE FROM event_attendee WHERE event_id IN (SELECT id FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1))", "DELETE FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1)", "DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM meeting WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err := db.From(ctx).Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	request := func(method, path string, input, output any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, input, output)
		if res.StatusCode != want {
			t.Fatalf("%s %s=%d want %d", method, path, res.StatusCode, want)
		}
	}
	request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID
	var c schema.Calendar
	request("POST", base+"/calendars", schema.CalendarInput{Name: "Team", Color: "#15756b"}, &c, 200)
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(time.Hour)
	location := "Conference room"
	var event schema.EventDetail
	request("POST", base+"/calendars/"+c.ID+"/events", schema.EventInput{Title: "Planning", Location: &location, StartsAt: &start, EndsAt: &end, TimeZone: "UTC"}, &event, 200)
	var meeting schema.Meeting
	request("POST", base+"/meet", schema.MeetingInput{Name: "Planning", RequestID: uuid.NewString()}, &meeting, 200)
	path := base + "/calendars/" + c.ID + "/events/" + event.Event.ID + "/meeting"
	request("PUT", path, schema.BindMeetingInput{MeetingID: meeting.ID, Sequence: event.Event.Sequence}, &event, 200)
	if event.Event.MeetingID == nil || *event.Event.MeetingID != meeting.ID || event.Event.Location != location || event.Event.Sequence != 1 {
		t.Fatal("meeting link replaced physical location or lost event version")
	}
	request("PUT", path, schema.BindMeetingInput{MeetingID: meeting.ID, Sequence: 0}, nil, 409)
	request("PUT", path, schema.BindMeetingInput{MeetingID: meeting.ID, Sequence: 1}, &event, 200)
	if event.Event.Sequence != 1 {
		t.Fatal("idempotent link changed sequence")
	}
	request("PUT", path, schema.BindMeetingInput{MeetingID: uuid.NewString(), Sequence: 1}, nil, 404)
	var exported schema.FileContent
	request("GET", base+"/calendars/"+c.ID+"/export", nil, &exported, 200)
	raw, err := base64.StdEncoding.DecodeString(exported.Data)
	if err != nil || !strings.Contains(strings.ReplaceAll(string(raw), "\r\n ", ""), "/app?workspace="+w.ID+"&meeting="+meeting.ID) {
		t.Fatal("exported event lost member-scoped meeting URL")
	}
	foreign, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM meeting WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err := db.From(ctx).Exec(ctx, sql, foreign.ID); err != nil {
				t.Error(err)
			}
		}
	})
	var foreignMeeting schema.Meeting
	request("POST", "/api/v1/workspaces/"+foreign.ID+"/meet", schema.MeetingInput{Name: "Other", RequestID: uuid.NewString()}, &foreignMeeting, 200)
	request("PUT", path, schema.BindMeetingInput{MeetingID: foreignMeeting.ID, Sequence: 1}, nil, 404)

	if _, err := db.From(ctx).Exec(ctx, "UPDATE meeting SET ended_at=now() WHERE id=$1", meeting.ID); err != nil {
		t.Fatal(err)
	}
	request("PUT", path, schema.BindMeetingInput{MeetingID: meeting.ID, Sequence: 1}, nil, 410)
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID); err != nil {
		t.Fatal(err)
	}
	request("PUT", path, schema.BindMeetingInput{MeetingID: meeting.ID, Sequence: 1}, nil, 404)
}
