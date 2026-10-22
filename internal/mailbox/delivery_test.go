package mailbox

import (
	"context"
	"errors"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	q "thura/db/queries/gen"
	"thura/schema"
)

func TestInboundSubjectLineBreaksAreStoredAsSpaces(t *testing.T) {
	raw := []byte("From: sender@example.com\r\nTo: inbox@example.com\r\nSubject: =?utf-8?q?Hello=0D=0ABcc:_x@evil.com?=\r\nContent-Type: text/plain\r\n\r\nBody\r\n")
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject != "Hello Bcc: x@evil.com" {
		t.Fatalf("decoded subject kept a line break: %q", parsed.Subject)
	}
}

func TestReplyQuoteIsBoundedInBytesOnRuneBoundary(t *testing.T) {
	if got := shortText("héllo", 2); got != "h" {
		t.Fatalf("cut inside a rune: %q", got)
	}
	if got := shortText("héllo", 3); got != "hé" {
		t.Fatalf("wrong byte bound: %q", got)
	}
	source := q.MailItem{ID: "source", Subject: "Big", TextBody: strings.Repeat("é", 499980), Status: "received", FromAddress: "sender@example.com", HTMLBody: ""}
	id := source.ID
	in := sourceDraft(q.Mailbox{Address: "team@example.com"}, source, schema.DraftInput{ReplyID: &id})
	if len(Text(in.Text)) > 500000 || !strings.HasSuffix(Text(in.Text), "é") {
		t.Fatalf("quoted reply exceeds the delivery byte limit or ends inside a rune: %d bytes", len(Text(in.Text)))
	}
}

func TestProviderRefusalIsDefiniteOnlyWithAReply(t *testing.T) {
	cases := map[string]struct {
		err      error
		definite bool
	}{
		"http 4xx":        {errors.New("api.mailgun.net: 400 {\"message\":\"'to' parameter is not a valid address\"}"), true},
		"http 5xx":        {errors.New("api.mailgun.net: 503 outcome unknown"), false},
		"smtp reply":      {&textproto.Error{Code: 550, Msg: "no such user"}, true},
		"transport":       {&url.Error{Op: "Post", URL: "https://api.mailgun.net/v3", Err: context.DeadlineExceeded}, false},
		"wrapped timeout": {&url.Error{Op: "Post", URL: "https://api.mailgun.net/v3", Err: errors.New("x: 400 looks like a status")}, false},
		"plain":           {errors.New("mail: duplicate recipient"), false},
	}
	for name, c := range cases {
		if definite(c.err) != c.definite {
			t.Errorf("%s: definite=%v", name, !c.definite)
		}
	}
}
