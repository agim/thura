package calendar

import (
	"strings"
	"testing"
	q "thura/db/queries/gen"
	"thura/schema"
	"time"
)

func TestRecurrencePreservesWallTimeAndSkipsDSTGap(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, hour := range []int{9, 2} {
		start := time.Date(2026, 3, 1, hour, 30, 0, 0, zone)
		rule, err := Rule("FREQ=WEEKLY;COUNT=3", start, false)
		if err != nil {
			t.Fatal(err)
		}
		next := rule.Iterator()
		values := []time.Time{}
		for {
			v, ok := next()
			if !ok {
				break
			}
			values = append(values, v)
		}
		if len(values) != 3 {
			t.Fatalf("COUNT yielded %d", len(values))
		}
		for _, v := range values {
			if v.Hour() != hour || v.Minute() != 30 {
				t.Fatalf("wall time shifted: %s", v)
			}
		}
		if hour == 9 {
			if values[0].UTC().Hour() != 14 || values[1].UTC().Hour() != 13 {
				t.Fatal("DST UTC offset was lost")
			}
		} else if values[1].Day() != 15 || values[2].Day() != 22 {
			t.Fatalf("nonexistent March 8 time was not skipped: %v", values)
		}
	}
	rule, err := Rule("FREQ=WEEKLY;COUNT=2", time.Date(2026, 10, 25, 1, 30, 0, 0, zone), false)
	if err != nil {
		t.Fatal(err)
	}
	next := rule.Iterator()
	first, _ := next()
	second, _ := next()
	if first.Hour() != 1 || second.Hour() != 1 || second.UTC().Hour() != 5 {
		t.Fatal("fall-back must use the first ambiguous occurrence", second)
	}
	for _, raw := range []string{"FREQ=SECONDLY;COUNT=5", "FREQ=DAILY", "FREQ=DAILY;COUNT=1001", "FREQ=YEARLY;COUNT=50", "FREQ=DAILY;UNTIL=20400101T000000Z", "FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=31;COUNT=1", "FREQ=DAILY;BYHOUR=1,2;COUNT=2"} {
		if _, err = Rule(raw, time.Date(2026, 1, 1, 9, 0, 0, 0, zone), false); err == nil {
			t.Fatalf("unbounded or invalid rule accepted: %s", raw)
		}
	}
}
func TestICSUnicodeExceptionsAndAllDayRoundTrip(t *testing.T) {
	zone, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, zone)
	end := start.Add(time.Hour)
	title := strings.Repeat("réunion 🌲;", 20)
	e := q.CalendarEvent{ID: "event", CalendarID: "calendar", Uid: "stable@example.com", Organizer: "owner@example.com", Title: title, Description: "Line one\nLine two, with punctuation; and \\", StartsAt: &start, EndsAt: &end, TimeZone: zone.String(), Rrule: "FREQ=WEEKLY;COUNT=3", UpdatedAt: start}
	renamed := "Moved occurrence"
	moved := time.Date(2026, 3, 16, 10, 0, 0, 0, zone)
	movedEnd := moved.Add(time.Hour)
	exceptions := []q.EventException{{InstanceKey: "2026-03-08T13:00:00Z", Cancelled: true}, {InstanceKey: "2026-03-15T13:00:00Z", Title: &renamed, StartsAt: &moved, EndsAt: &movedEnd}}
	allDay := q.CalendarEvent{ID: "all-day", Uid: "date@example.com", Organizer: "owner@example.com", Title: "Holiday", AllDay: true, StartDate: "2026-03-08", EndDate: "2026-03-09", TimeZone: zone.String(), UpdatedAt: start}
	raw, err := ExportICS([]q.CalendarEvent{e, allDay}, map[string][]q.EventAttendee{"event": {{Email: "guest@example.com", Response: q.AttendanceAccepted}}}, map[string][]q.EventException{"event": exceptions}, "PUBLISH")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("ICS line exceeds 75 octets: %d", len(line))
		}
	}
	if !strings.Contains(string(raw), "BEGIN:VTIMEZONE") || !strings.Contains(string(raw), "TZOFFSETTO:-0400") {
		t.Fatal("timezone transitions not exported")
	}
	records, method, err := ParseICS(raw, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	if method != "PUBLISH" || len(records) != 3 {
		t.Fatal("lost series, override or all-day event")
	}
	if records[0].Input.Title != title || text(records[0].Input.Description) != e.Description || records[0].Input.TimeZone != zone.String() || len(records[0].Exdates) != 1 || records[0].Responses["guest@example.com"] != q.AttendanceAccepted {
		t.Fatal("calendar text or recurrence metadata did not round-trip")
	}
	if records[1].Instance != "2026-03-15T13:00:00Z" || records[1].Input.Title != renamed || !records[1].Input.StartsAt.Equal(moved) {
		t.Fatal("override did not round-trip")
	}
	if !records[2].Input.AllDay || text(records[2].Input.StartDate) != "2026-03-08" || records[2].Input.StartsAt != nil {
		t.Fatal("all-day date became a timestamp")
	}
	instances, err := Instances(e, "#15756b", exceptions, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(instances) != 2 || instances[1].Title != renamed || instances[1].Start != "2026-03-16T14:00:00Z" {
		t.Fatal("cancel/move exceptions were not applied", instances, err)
	}
	invalid := schema.EventInput{Title: "Bad all-day", AllDay: true, StartDate: &allDay.StartDate, EndDate: &allDay.EndDate, StartsAt: &start, TimeZone: zone.String(), Attendees: []string{}}
	if _, err = Validate(invalid); err == nil {
		t.Fatal("mixed all-day/timestamp fields accepted")
	}
	alarm := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:alarm@example.com\r\nSUMMARY:Visible event\r\nDTSTART:20261008T130000Z\r\nDTEND:20261008T140000Z\r\nBEGIN:VALARM\r\nDESCRIPTION:Alarm message\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	parsed, _, err := ParseICS(alarm, "America/New_York")
	if err != nil || parsed[0].Input.Title != "Visible event" || text(parsed[0].Input.Description) != "" {
		t.Fatal("alarm properties leaked into event", err)
	}
}
