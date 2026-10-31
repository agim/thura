package mailabuse

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"thura/internal/assistant"
	"thura/schema"
)

func decodeEvidence(t *testing.T, out string) aiEvidence {
	t.Helper()
	var e aiEvidence
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAIEvidenceKeepsShortMailWhole(t *testing.T) {
	out, err := buildAIEvidence("a@b.example", "Hi", "Hello <team> & friends", "<p>Hello <b>team</b> &amp; friends</p>", senderEvidence{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := decodeEvidence(t, out)
	if e.Truncated || e.Text != "Hello <team> & friends" || e.HTMLVisibleText != "" || e.HTMLLinks != nil {
		t.Fatalf("short mail changed: %+v", e)
	}
	if !strings.Contains(out, "<team>") {
		t.Fatal("markup-like characters were escaped and inflated")
	}
}

func TestAIEvidenceUsesHTMLTextWhenItAddsSomething(t *testing.T) {
	html := `<html><head><style>p{color:red}</style><script>steal()</script></head><body><p>Verify your account now</p><a href="https://login.example/reset">here</a><!-- hidden comment --></body></html>`
	out, err := buildAIEvidence("a@b.example", "Hi", "", html, senderEvidence{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := decodeEvidence(t, out)
	if !strings.Contains(e.HTMLVisibleText, "Verify your account now") || strings.Contains(out, "steal()") || strings.Contains(out, "color:red") || strings.Contains(out, "hidden comment") || strings.Contains(out, "<p>") {
		t.Fatalf("HTML not reduced to visible text: %q", out)
	}
	if len(e.HTMLLinks) != 1 || e.HTMLLinks[0] != "https://login.example/reset" {
		t.Fatalf("links: %v", e.HTMLLinks)
	}
}

func TestAIEvidenceFitsLongMailWithinTheLimit(t *testing.T) {
	text := strings.Repeat("Ünïcode line with \"quotes\" and a newline\n", 3000)
	html := "<p>" + strings.Repeat("Different words in the HTML version only. ", 4000) + "</p>"
	files := []string{}
	for i := 0; i < 150; i++ {
		files = append(files, "report.pdf (application/pdf)")
	}
	findings := []schema.MailFinding{{Code: "x", Severity: "warning", Detail: "fixture"}}
	out, err := buildAIEvidence("a@b.example", strings.Repeat("S", 5000), text, html, senderEvidence{}, files, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > assistant.MailScreeningInputLimit || !utf8.ValidString(out) {
		t.Fatalf("evidence of %d bytes over the limit or invalid UTF-8", len(out))
	}
	e := decodeEvidence(t, out)
	if !e.Truncated || e.OriginalTextBytes != len(text) || e.OriginalHTMLBytes != len(html) || len(e.Files) != 100 || len(e.Subject) != 2000 {
		t.Fatalf("truncation metadata: truncated %v sizes %d/%d files %d", e.Truncated, e.OriginalTextBytes, e.OriginalHTMLBytes, len(e.Files))
	}
	if !strings.HasPrefix(e.Text, "Ünïcode line") || !strings.HasSuffix(e.Text, "a newline\n") || !strings.Contains(e.Text, "middle omitted") {
		t.Fatal("text did not keep its beginning and tail")
	}
	if !strings.HasPrefix(e.HTMLVisibleText, "Different words") || len(e.HTMLVisibleText) < len(e.Text)/4 {
		t.Fatalf("HTML text share: text %d html %d", len(e.Text), len(e.HTMLVisibleText))
	}
}
