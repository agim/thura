package mailabuse

import (
	"net/mail"
	"testing"
)

func TestAutomatedReasonHeaderRules(t *testing.T) {
	cases := []struct {
		name   string
		header mail.Header
		want   string
	}{
		{"person", mail.Header{"From": {"Ada <ada@example.com>"}}, ""},
		{"list unsubscribe", mail.Header{"From": {"news@example.com"}, "List-Unsubscribe": {"<mailto:u@example.com>"}}, "List-Unsubscribe header"},
		{"auto submitted", mail.Header{"From": {"ada@example.com"}, "Auto-Submitted": {"auto-replied; owner-email=x"}}, "Auto-Submitted header"},
		{"auto submitted no", mail.Header{"From": {"ada@example.com"}, "Auto-Submitted": {" No "}}, ""},
		{"precedence bulk", mail.Header{"From": {"ada@example.com"}, "Precedence": {"Bulk"}}, "Precedence header"},
		{"precedence list", mail.Header{"From": {"ada@example.com"}, "Precedence": {"list"}}, "Precedence header"},
		{"precedence junk", mail.Header{"From": {"ada@example.com"}, "Precedence": {"junk"}}, "Precedence header"},
		{"precedence first-class", mail.Header{"From": {"ada@example.com"}, "Precedence": {"first-class"}}, ""},
		{"no-reply", mail.Header{"From": {"Shop <no-reply@shop.example>"}}, "no-reply sender address"},
		{"noreply tag", mail.Header{"From": {"NoReply+orders@shop.example"}}, "no-reply sender address"},
		{"do not reply", mail.Header{"From": {"do_not_reply-billing@bank.example"}}, "no-reply sender address"},
		{"donotreply", mail.Header{"From": {"donotreply@bank.example"}}, "no-reply sender address"},
		{"mailer daemon", mail.Header{"From": {"MAILER-DAEMON@mx.example"}}, "no-reply sender address"},
		{"postmaster", mail.Header{"From": {"postmaster@mx.example"}}, "no-reply sender address"},
		{"reply in name only", mail.Header{"From": {"\"No Reply\" <ada@example.com>"}}, ""},
		{"replies is a person", mail.Header{"From": {"replies@example.com"}}, ""},
	}
	for _, c := range cases {
		if got := AutomatedReason(c.header); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
