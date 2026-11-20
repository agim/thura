package push

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	q "thura/db/queries/gen"
)

func TestDeliveryOutcomesInPlainWords(t *testing.T) {
	for code, want := range map[int]struct {
		delivered, remove, retry bool
		text                     string
	}{
		201: {delivered: true},
		410: {remove: true, text: "The push service refused it (410 Gone): this device unsubscribed. Turn notifications on again here."},
		404: {remove: true, text: "(404 Not Found): this device unsubscribed"},
		403: {text: "(403 Forbidden): it does not accept this server's key"},
		429: {retry: true, text: "busy (429 Too Many Requests)"},
		503: {retry: true, text: "The push service had a problem (503 Service Unavailable)"},
		400: {text: "The push service refused it (400 Bad Request)."},
	} {
		o := statusOutcome(code, "")
		if o.delivered != want.delivered || o.remove != want.remove || (o.retry != nil) != want.retry || !strings.Contains(o.message, want.text) {
			t.Errorf("%d: %+v", code, o)
		}
	}
	// The push service's own words are kept, never an address.
	if o := statusOutcome(400, "invalid TTL header"); !strings.HasSuffix(o.message, ": invalid TTL header") {
		t.Errorf("detail dropped: %s", o.message)
	}
	if o := statusOutcome(400, "see https://fcm.googleapis.com/fcm/send/secret"); strings.Contains(o.message, "fcm") {
		t.Errorf("detail with an address kept: %s", o.message)
	}
	if o := sendError(&url.Error{Op: "Post", URL: "https://fcm.googleapis.com/x", Err: errors.New("refused")}); o.kind != ErrNetwork || o.retry == nil || strings.Contains(o.message, "fcm") {
		t.Errorf("network: %+v", o)
	}
	if o := sendError(errors.New("bad key")); o.kind != ErrVAPID || o.retry != nil {
		t.Errorf("vapid: %+v", o)
	}
	for endpoint, want := range map[string]string{
		"https://fcm.googleapis.com/fcm/send/x":            "Google's push service",
		"https://updates.push.services.mozilla.com/wpush/": "Mozilla's push service",
		"https://web.push.apple.com/x":                     "Apple's push service",
		"https://wns2-par02p.notify.windows.com/w/":        "Microsoft's push service",
	} {
		if got := serviceName(endpoint); got != want {
			t.Errorf("%s: %s", endpoint, got)
		}
	}
	if validKeys("not-a-key", "AAAAAAAAAAAAAAAAAAAAAA") {
		t.Error("an invalid p256dh key encrypts")
	}
}

func TestDeclinedNamesTheReason(t *testing.T) {
	people := defaults()
	all := defaults()
	all.MailPeopleOnly = false
	none := defaults()
	none.Mail = false
	for _, c := range []struct {
		p      q.NotificationPreference
		person bool
		want   string
	}{
		{people, false, SkipPeopleOnly}, {people, true, ""},
		{all, false, ""}, {all, true, ""},
		{none, false, SkipOff}, {none, true, SkipOff},
	} {
		if got := declined(c.p, Event{Kind: KindMail, Person: c.person}); got != c.want {
			t.Errorf("mail=%v peopleOnly=%v person=%v: %q want %q", c.p.Mail, c.p.MailPeopleOnly, c.person, got, c.want)
		}
	}
}
