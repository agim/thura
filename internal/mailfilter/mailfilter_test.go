package mailfilter

import (
	"testing"

	q "thura/db/queries/gen"
)

func TestMatch(t *testing.T) {
	item := q.MailItem{FromAddress: "Acme Billing <billing@acme.example>", ToAddress: "me@team.example", Cc: "accounts@team.example", Subject: "Invoice 42 is ready", TextBody: "Your invoice for October."}
	cases := []struct {
		name string
		f    q.MailFilter
		want bool
	}{
		{"no criterion never matches", q.MailFilter{}, false},
		{"only separators never matches", q.MailFilter{FromContains: " , OR "}, false},
		{"from address, any case", q.MailFilter{FromContains: "BILLING@acme.example"}, true},
		{"from name", q.MailFilter{FromContains: "acme billing"}, true},
		{"from domain", q.MailFilter{FromContains: "@acme.example"}, true},
		{"another sender", q.MailFilter{FromContains: "news@acme.example"}, false},
		{"alternatives by comma", q.MailFilter{FromContains: "news@other.example, billing@acme.example"}, true},
		{"alternatives by OR", q.MailFilter{SubjectContains: "receipt OR invoice"}, true},
		{"to covers cc", q.MailFilter{ToContains: "accounts@"}, true},
		{"words in the body", q.MailFilter{HasWords: "october"}, true},
		{"words in the subject", q.MailFilter{HasWords: "is ready"}, true},
		{"every filled criterion must match", q.MailFilter{FromContains: "acme", SubjectContains: "receipt"}, false},
		{"all match", q.MailFilter{FromContains: "acme", ToContains: "me@", SubjectContains: "invoice", HasWords: "october"}, true},
	}
	for _, c := range cases {
		if got := Match(c.f, item); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	html := q.MailItem{FromAddress: "a@b.example", HTMLBody: "<p>Weekly digest</p>"}
	if !Match(q.MailFilter{HasWords: "weekly digest"}, html) {
		t.Error("words are found in an HTML-only body")
	}
}
