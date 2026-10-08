package tests

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"io"
	"net/http"
	"strings"
	"testing"
	"thura/app"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestCalendarPrivacyConcurrentVersionsAndICSImport(t *testing.T) {
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("calendar-%d@example.com", suffix)
	otherEmail := fmt.Sprintf("calendar-other-%d@example.com", suffix)
	pw := "forest maple waterfall lantern 7143"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := auth.From(ctx).CreateUser(ctx, otherEmail, "Member", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Calendar test", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", other.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM event_exception WHERE event_id IN (SELECT id FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1))", "DELETE FROM event_attendee WHERE event_id IN (SELECT id FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1))", "DELETE FROM calendar_event WHERE calendar_id IN (SELECT id FROM calendar WHERE workspace_id=$1)", "DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
		auth.From(ctx).DeleteUser(ctx, other.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s got %d want %d", path, res.StatusCode, want)
		}
	}
	base := "/api/v1/workspaces/" + w.ID + "/calendars"
	status("GET", base, nil, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	var shared, private schema.Calendar
	status("POST", base, schema.CalendarInput{Name: "Shared", Color: "#15756b"}, &shared, 200)
	status("POST", base, schema.CalendarInput{Name: "Private", Color: "#c8492f", Personal: true}, &private, 200)
	a, b := time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 15, 0, 0, 0, time.UTC)
	rule := "FREQ=WEEKLY;COUNT=3"
	input := schema.EventInput{Title: "DST meeting", StartsAt: &a, EndsAt: &b, TimeZone: "America/New_York", Rrule: &rule, Attendees: []string{email, otherEmail}}
	var event schema.EventDetail
	events := base + "/" + shared.ID + "/events"
	status("POST", events, input, &event, 200)
	id := event.Event.ID
	status("GET", base+"/"+private.ID+"/events/"+id, nil, nil, 404)
	var instances schema.CalendarInstances
	rangePath := base + "/" + shared.ID + "/instances?from=2026-03-01T00:00:00Z&to=2026-04-01T00:00:00Z"
	status("GET", rangePath, nil, &instances, 200)
	if len(instances.Items) != 3 || instances.Items[1].Start != "2026-03-08T13:00:00Z" {
		t.Fatal("lost DST wall time")
	}
	status("GET", base+"/"+shared.ID+"/instances?from=2026-01-01T00:00:00Z&to=2027-01-01T00:00:00Z", nil, nil, 422)
	status("PATCH", events+"/"+id+"/occurrences", schema.ExceptionInput{InstanceKey: "2026-03-08T13:00:00Z", Sequence: 0, Cancelled: true}, &event, 200)
	status("PUT", events+"/"+id, input, nil, 409)
	input.Sequence = 1
	newRule := "FREQ=WEEKLY;COUNT=4"
	input.Rrule = &newRule
	status("PUT", events+"/"+id, input, nil, 409)
	input.ResetExceptions = true
	status("PUT", events+"/"+id, input, &event, 200)
	if event.Event.Sequence != 2 || len(event.Exceptions) != 0 {
		t.Fatal("exceptions were not explicitly reset")
	}
	input.Sequence = 2
	input.Title = "Concurrent update"
	results := make(chan int, 2)
	for n := 0; n < 2; n++ {
		go func() { res := srv.JSON(t, "PUT", events+"/"+id, input, nil); results <- res.StatusCode }()
	}
	counts := map[int]int{}
	counts[<-results]++
	counts[<-results]++
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent updates lost version check: %v", counts)
	}
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: otherEmail, Password: pw}, nil, 200)
	var calendars schema.CalendarList
	status("GET", base, nil, &calendars, 200)
	if len(calendars.Items) != 1 || calendars.Items[0].ID != shared.ID {
		t.Fatal("personal calendar exposed to another member")
	}
	status("GET", base+"/"+private.ID+"/events", nil, nil, 404)
	status("GET", events+"/"+id, nil, &event, 200)
	var exported schema.FileContent
	status("GET", base+"/"+shared.ID+"/export", nil, &exported, 200)
	raw, _ := base64.StdEncoding.DecodeString(exported.Data)
	if !bytes.Contains(raw, []byte("BEGIN:VTIMEZONE")) {
		t.Fatal("export lost timezone")
	}
	var importedCal schema.Calendar
	status("POST", base, schema.CalendarInput{Name: "Imported", Color: "#15756b"}, &importedCal, 200)
	upload := func(calendarID string, raw []byte, want int) schema.CalendarImportResult {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+base+"/"+calendarID+"/import", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "text/calendar")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("import got %d want %d: %s", res.StatusCode, want, body)
		}
		return schema.CalendarImportResult{}
	}
	upload(importedCal.ID, raw, 200)
	upload(importedCal.ID, raw, 200)
	var imported schema.CalendarEvents
	status("GET", base+"/"+importedCal.ID+"/events", nil, &imported, 200)
	if len(imported.Items) != 1 || imported.Items[0].Event.Uid != event.Event.Uid || imported.Items[0].Event.Sequence != 3 {
		t.Fatal("UID/sequence import was not idempotent")
	}
	cancel := fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:CANCEL\r\nBEGIN:VEVENT\r\nUID:%s\r\nSEQUENCE:4\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", event.Event.Uid)
	upload(importedCal.ID, []byte(cancel), 200)
	status("GET", base+"/"+importedCal.ID+"/events", nil, &imported, 200)
	if !imported.Items[0].Event.Cancelled || imported.Items[0].Event.Title != "Concurrent update" {
		t.Fatal("cancel import erased event or failed to cancel")
	}
	upload(importedCal.ID, raw, 200)
	status("GET", base+"/"+importedCal.ID+"/events", nil, &imported, 200)
	if !imported.Items[0].Event.Cancelled {
		t.Fatal("older delivery resurrected cancelled event")
	}
	upload(shared.ID, []byte(strings.Repeat("x", (1<<20)+1)), 413)
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", other.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", events+"/"+id, nil, nil, 404)
}
