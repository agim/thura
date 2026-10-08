package calendar

import (
	"strings"
	"testing"
)

func TestReplyICSIdentityWithoutDates(t *testing.T) {
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nMETHOD:REPLY\r\nBEGIN:VEVENT\r\nUID:test-event\r\nSEQUENCE:3\r\nATTENDEE;CN=\"Guest; One\";PARTSTAT=ACCEPTED:mailto:guest@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	r, err := ParseReplyICS(raw)
	if err != nil || r.UID != "test-event" || r.Sequence != 3 || r.Email != "guest@example.com" || r.Response != "accepted" {
		t.Fatalf("reply: %+v %v", r, err)
	}
	for _, bad := range []string{strings.Replace(raw, "METHOD:REPLY", "METHOD:REQUEST", 1), strings.Replace(raw, "SEQUENCE:3\r\n", "", 1), strings.Replace(raw, "SEQUENCE:3", "SEQUENCE:3\r\nSEQUENCE:4", 1), strings.Replace(raw, "END:VEVENT", "BEGIN:VALARM\r\nEND:VALARM\r\nEND:VEVENT", 1), strings.Replace(raw, "END:VCALENDAR", "", 1), strings.Replace(raw, "PARTSTAT=ACCEPTED", "PARTSTAT=UNKNOWN", 1), strings.Replace(raw, "END:VEVENT", "ATTENDEE;PARTSTAT=ACCEPTED:mailto:other@example.com\r\nEND:VEVENT", 1)} {
		if _, err = ParseReplyICS(bad); err == nil {
			t.Fatal("accepted malformed reply")
		}
	}
}
