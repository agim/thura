package push

import (
	"encoding/json"
	"strings"
	"testing"

	q "thura/db/queries/gen"
)

func TestEndpointPolicyOutsideTestMode(t *testing.T) {
	t.Setenv("LIDZA_MODE", "")
	for endpoint, ok := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":            true,
		"https://updates.push.services.mozilla.com/wpush/v2": true,
		"https://web.push.apple.com/QGx":                     true,
		"https://wns2-par02p.notify.windows.com/w/?token=x":  true,
		"http://fcm.googleapis.com/fcm/send/abc":             false,
		"https://fcm.googleapis.com.evil.example/x":          false,
		"https://evil.example/fcm.googleapis.com":            false,
		"https://user@fcm.googleapis.com/x":                  false,
		"https://fcm.googleapis.com:8443/x":                  false,
		"http://127.0.0.1:9000/push":                         false,
		"not a url":                                          false,
	} {
		if err := CheckEndpoint(endpoint); (err == nil) != ok {
			t.Errorf("%s: accepted=%v want %v", endpoint, err == nil, ok)
		}
	}
	if publicOnly("tcp", "127.0.0.1:443", nil) == nil || publicOnly("tcp", "10.1.2.3:443", nil) == nil || publicOnly("tcp", "[::1]:443", nil) == nil {
		t.Error("the dialer reaches a non-public address")
	}
	if publicOnly("tcp", "142.250.180.10:443", nil) != nil {
		t.Error("the dialer refuses a public address")
	}
	t.Setenv("LIDZA_MODE", "test")
	if CheckEndpoint("http://127.0.0.1:9000/push") != nil {
		t.Error("the browser suite's local push service is refused in test mode")
	}
	if CheckEndpoint("https://evil.example/push") == nil {
		t.Error("test mode accepts any host")
	}
}

func TestPayloadIsBoundedAndStaysInTheApp(t *testing.T) {
	raw, err := Encode(Event{Kind: KindMail, Title: strings.Repeat("Ä", 300), Body: "line one\n\nline   two " + strings.Repeat("x", 500), URL: "https://evil.example/app/mail"})
	if err != nil {
		t.Fatal(err)
	}
	var p payload
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if len([]rune(p.Title)) > maxTitle || len([]rune(p.Body)) > maxBody || !strings.HasPrefix(p.Body, "line one line two") {
		t.Fatalf("not bounded: %d/%d %q", len([]rune(p.Title)), len([]rune(p.Body)), p.Body[:20])
	}
	if p.URL != "/app/mail" {
		t.Fatalf("link left the app: %s", p.URL)
	}
	if raw, _ = Encode(Event{Kind: KindChat, URL: "/app/chat?room=1"}); !strings.Contains(string(raw), `"title":"Thura"`) {
		t.Fatalf("empty title: %s", raw)
	}
}

func TestPreferencesDecideEachKind(t *testing.T) {
	all := defaults()
	if !Wants(all, Event{Kind: KindMail, Person: true}) || Wants(all, Event{Kind: KindMail}) {
		t.Fatal("mail defaults to real people only")
	}
	anyone := all
	anyone.MailPeopleOnly = false
	if !Wants(anyone, Event{Kind: KindMail}) {
		t.Fatal("mail from anyone")
	}
	for kind, off := range map[string]func(*q.NotificationPreference){
		KindMail:        func(p *q.NotificationPreference) { p.Mail = false },
		KindChat:        func(p *q.NotificationPreference) { p.Chat = false },
		KindChatRequest: func(p *q.NotificationPreference) { p.ChatRequests = false },
		KindCalendar:    func(p *q.NotificationPreference) { p.Calendar = false },
		KindMeet:        func(p *q.NotificationPreference) { p.Meet = false },
	} {
		p := defaults()
		if !Wants(p, Event{Kind: kind, Person: true}) {
			t.Fatalf("%s off by default", kind)
		}
		off(&p)
		if Wants(p, Event{Kind: kind, Person: true}) {
			t.Fatalf("%s ignores its checkbox", kind)
		}
	}
	if !Wants(q.NotificationPreference{}, Event{Kind: KindTest}) || Wants(all, Event{Kind: "other"}) {
		t.Fatal("test always sends; an unknown kind never does")
	}
}
