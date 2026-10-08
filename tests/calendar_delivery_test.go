package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"strings"
	"testing"
	"thura/app"
	"thura/internal/calendar"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestCalendarInvitationsRepliesAndReminderDeduplication(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("cal-delivery-%d@example.com", suffix)
	guest := fmt.Sprintf("cal-guest-%d@example.com", suffix)
	pw := "forest maple waterfall lantern 7143"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Calendar delivery", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		events := "SELECT e.id FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id WHERE c.workspace_id=$1"
		for _, sql := range []string{"DELETE FROM calendar_reply_grant WHERE attendee_id IN (SELECT id FROM event_attendee WHERE event_id IN (" + events + "))", "DELETE FROM calendar_dispatch WHERE event_id IN (" + events + ")", "DELETE FROM calendar_reminder WHERE event_id IN (" + events + ")", "DELETE FROM reminder_notice WHERE event_id IN (" + events + ")", "DELETE FROM event_exception WHERE event_id IN (" + events + ")", "DELETE FROM event_attendee WHERE event_id IN (" + events + ")", "DELETE FROM calendar_event WHERE id IN (" + events + ")", "DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s: got %d want %d", path, res.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID + "/calendars"
	var cal schema.Calendar
	status("POST", base, schema.CalendarInput{Name: "Team", Color: "#15756b"}, &cal, 200)
	start := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	end := start.Add(time.Hour)
	input := schema.EventInput{Title: "Planning", StartsAt: &start, EndsAt: &end, TimeZone: "America/New_York", Attendees: []string{guest}}
	var event schema.EventDetail
	events := base + "/" + cal.ID + "/events"
	status("POST", events, input, &event, 200)
	path := events + "/" + event.Event.ID
	var dispatch schema.CalendarDispatchResult
	status("POST", path+"/invitations", schema.CalendarInviteInput{Sequence: 0}, &dispatch, 200)
	if dispatch.Queued != 1 || !dispatch.Captured {
		t.Fatal("expected one locally captured invitation")
	}
	status("POST", path+"/invitations", schema.CalendarInviteInput{Sequence: 0}, &dispatch, 200)
	if dispatch.Queued != 0 {
		t.Fatal("duplicate invitation queued")
	}
	var body string
	if err = pool.QueryRow(ctx, "SELECT text FROM mail_message WHERE recipient=$1 ORDER BY created_at DESC LIMIT 1", guest).Scan(&body); err != nil {
		t.Fatal(err)
	}
	fragment := strings.SplitN(body, "/rsvp#", 2)
	if len(fragment) != 2 {
		t.Fatal("no reply link")
	}
	token := strings.Fields(fragment[1])[0]
	var view schema.CalendarReplyView
	status("POST", "/api/v1/calendar-replies/open", schema.OpenShareInput{Token: token}, &view, 200)
	if view.Email != guest || view.Event.ID != event.Event.ID {
		t.Fatal("grant opened another event")
	}
	status("POST", "/api/v1/calendar-replies/respond", schema.CalendarReplyInput{Token: token, Response: "accepted"}, &view, 200)
	if view.Response != "accepted" {
		t.Fatal("response not persisted")
	}
	reply := fmt.Sprintf("BEGIN:VCALENDAR\r\nMETHOD:REPLY\r\nBEGIN:VEVENT\r\nUID:%s\r\nSEQUENCE:0\r\nATTENDEE;PARTSTAT=TENTATIVE:mailto:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", event.Event.Uid, guest)
	status("POST", "/api/v1/calendar-replies/ics", schema.CalendarReplyICS{Token: token, Content: strings.Replace(reply, guest, "wrong@example.com", 1)}, nil, 409)
	status("POST", "/api/v1/calendar-replies/ics", schema.CalendarReplyICS{Token: token, Content: reply}, &view, 200)
	if view.Response != "tentative" {
		t.Fatal("ICS reply was lost")
	}
	minutes := 30
	status("PUT", path+"/reminder", schema.ReminderInput{MinutesBefore: &minutes}, nil, 200)
	now := start.Add(-30 * time.Minute).Add(time.Second)
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_message WHERE recipient=$1 AND subject LIKE 'Reminder:%'", email).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count() != 1 {
		t.Fatal("reminder was lost or duplicated")
	}
	input.Title = "Changed planning"
	status("PUT", path, input, &event, 200)
	status("POST", "/api/v1/calendar-replies/respond", schema.CalendarReplyInput{Token: token, Response: "declined"}, nil, 409)
	if _, err = pool.Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", owner.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("revoked member received a new reminder")
	}
	if _, err = pool.Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", owner.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("PUT", path+"/reminder", schema.ReminderInput{}, nil, 200)
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("disabled reminder sent mail")
	}
	status("PUT", path+"/reminder", schema.ReminderInput{MinutesBefore: &minutes}, nil, 200)

	status("POST", path+"/invitations", schema.CalendarInviteInput{Sequence: 1}, &dispatch, 200)
	if dispatch.Queued != 1 {
		t.Fatal("new sequence not sent")
	}
	if err = pool.QueryRow(ctx, "SELECT text FROM mail_message WHERE recipient=$1 ORDER BY created_at DESC LIMIT 1", guest).Scan(&body); err != nil {
		t.Fatal(err)
	}
	token = strings.Fields(strings.SplitN(body, "/rsvp#", 2)[1])[0]
	digest := sha256.Sum256([]byte(token))
	if _, err = pool.Exec(ctx, "UPDATE calendar_reply_grant SET expires_at=now()-interval '1 minute' WHERE token_hash=$1", hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	status("POST", "/api/v1/calendar-replies/open", schema.OpenShareInput{Token: token}, nil, 410)
	status("DELETE", path+"?sequence=1", nil, &event, 200)
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("cancelled event sent reminder")
	}
	status("POST", path+"/invitations", schema.CalendarInviteInput{Sequence: 2}, &dispatch, 200)
	if dispatch.Queued != 1 {
		t.Fatal("cancellation was not queued")
	}
	if err = pool.QueryRow(ctx, "SELECT text FROM mail_message WHERE recipient=$1 AND subject='Cancelled: Changed planning'", guest).Scan(&body); err != nil || strings.Contains(body, "/rsvp#") {
		t.Fatal("cancellation contained active reply grant")
	}
}
