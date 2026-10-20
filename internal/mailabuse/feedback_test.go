package mailabuse

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"testing"

	q "thura/db/queries/gen"
)

func TestHistoricalFeedbackIsStableCompleteAndHeaderSafe(t *testing.T) {
	item := q.MailItem{ID: "00000000-0000-4000-8000-000000000001", FromAddress: "Colleague <peer@example.org>", Subject: "Planning\r\nBcc: injected@example.org", TextBody: "Please review our weekly planning agenda and updated installation schedule before the next team meeting.", HTMLBody: "<p>Planning <strong>agenda</strong></p>"}
	box := q.Mailbox{Address: "inbox@example.org"}
	first, err := historicalFeedback(item, box)
	if err != nil {
		t.Fatal(err)
	}
	second, err := historicalFeedback(item, box)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("retry changes the learning identity")
	}
	message, err := mail.ReadMessage(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if message.Header.Get("Bcc") != "" {
		t.Fatal("historical subject injected a header")
	}
	_, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(message.Body, params["boundary"])
	for _, expected := range []string{item.TextBody, item.HTMLBody} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part)
		if err != nil || string(body) != expected {
			t.Fatal("stored content was truncated or altered")
		}
	}
	if _, err = reader.NextPart(); err != io.EOF {
		t.Fatal("unexpected MIME part")
	}
}
