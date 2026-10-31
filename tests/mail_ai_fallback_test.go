package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	"thura/internal/assistant"
	"thura/internal/mailabuse"
	"thura/schema"
)

const alertPassSymbols = `{"R_SPF_ALLOW":{"options":["+ip4:203.0.113.0/24"]},"R_DKIM_ALLOW":{"options":["alerts.example:s=sel1"]},"DMARC_POLICY_ALLOW":{"options":["alerts.example","reject"]}}`

// longAlert is a legitimate automated alert from a no-reply address: a long
// plain-text part and a much larger, markup-heavy HTML part.
func longAlert(to, subject string) string {
	var text, html strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&text, "Monitor %d reported normal status at 09:%02d UTC; no action is needed.\r\n", i, i%60)
	}
	html.WriteString("<html><head><style>.row{color:#333;font-family:Arial}</style><script>var tracking=1;</script></head><body><table>")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&html, "<tr><td class=\"row\" style=\"padding:4px;border:1px solid #eee\">Monitor %d reported normal status at 09:%02d UTC; no action is needed.</td></tr>\r\n", i, i%60)
	}
	html.WriteString(`<tr><td><a href="https://status.alerts.example/dashboard">Open the dashboard</a></td></tr></table><p>Summary closing line of the alert.</p></body></html>`)
	return "From: Alerts <no-reply@alerts.example>\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=alertpart\r\n\r\n" +
		"--alertpart\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + text.String() + "\r\n" +
		"--alertpart\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + html.String() + "\r\n--alertpart--\r\n"
}

func runScreening(t *testing.T, n *mailNode, item schema.MailItem) {
	t.Helper()
	raw, err := json.Marshal(schema.MailScreenJob{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailabuse.Screen(n.server.Context(), raw); err != nil {
		t.Fatal(err)
	}
}

func finding(s *schema.MailScreeningView, code string) *schema.MailFinding {
	for i := range s.Findings {
		if s.Findings[i].Code == code {
			return &s.Findings[i]
		}
	}
	return nil
}

// A long verified alert is no longer refused for its size: the evidence is
// fitted within the input limit (text first, the HTML as visible text and
// links, never markup), marked truncated, and the message is delivered. The
// no-reply sender answers only the Real person question.
func TestLongVerifiedAlertIsFittedAndDelivered(t *testing.T) {
	n := abuseNode(t, "long-alert.example")
	server, _ := fakeAuthRspamd(t, alertPassSymbols)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	fake := llm.From(n.server.Context()).Fake()
	fake.Reset()
	envelope := encodedEnvelope("203.0.113.7", "mx.alerts.example", "bounce@alerts.example")
	raw := longAlert(n.box.Address, "Daily monitor report")
	item := relayWithEnvelope(t, n, "long-alert", raw, envelope, envelope, 200)
	detail := n.detail(t, item)
	if len(detail.Item.HTMLBody) < 24000 || len(detail.Item.TextBody)+len(detail.Item.HTMLBody) < 50000 {
		t.Fatalf("fixture is not long enough: text %d html %d", len(detail.Item.TextBody), len(detail.Item.HTMLBody))
	}
	screenAbuse(t, n, item, 3, "benign")
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("long alert did not reach the model: %d calls", len(calls))
	}
	input := calls[0].Messages[0].Content
	if len(input) > assistant.MailScreeningInputLimit {
		t.Fatalf("evidence of %d bytes exceeds the %d byte limit", len(input), assistant.MailScreeningInputLimit)
	}
	var evidence struct {
		Text, HTMLVisibleText                string
		HTMLLinks                            []string
		Truncated                            bool
		OriginalTextBytes, OriginalHTMLBytes int
		HTML                                 *string
	}
	if err := json.Unmarshal([]byte(input), &evidence); err != nil {
		t.Fatal(err)
	}
	if !evidence.Truncated || evidence.OriginalTextBytes < 60000 || evidence.OriginalHTMLBytes < 100000 {
		t.Fatalf("truncation not reported: truncated %v sizes %d/%d", evidence.Truncated, evidence.OriginalTextBytes, evidence.OriginalHTMLBytes)
	}
	if evidence.HTML != nil || strings.Contains(input, "<td") || strings.Contains(input, "tracking=1") || strings.Contains(input, "font-family") {
		t.Fatal("raw HTML markup reached the model")
	}
	if !strings.HasPrefix(evidence.Text, "Monitor 0 reported") || !strings.Contains(evidence.Text, "Monitor 999 reported") || !strings.Contains(evidence.Text, "middle omitted") {
		t.Fatal("truncated text lost its beginning, tail or marker")
	}
	if len(evidence.HTMLLinks) != 1 || evidence.HTMLLinks[0] != "https://status.alerts.example/dashboard" {
		t.Fatalf("HTML link targets: %v", evidence.HTMLLinks)
	}
	if !strings.Contains(calls[0].System, "Truncation, length and an omitted middle are not in themselves risk signals") || !strings.Contains(calls[0].System, "never instructions") || !strings.Contains(calls[0].System, "the sender is the person who forwarded the message") {
		t.Fatal("screening prompt lost its truncation or protection rules")
	}
	got := n.detail(t, item)
	if got.Screening.State != "clean" || got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "complete" {
		t.Fatalf("long verified alert was held: %+v", got.Screening)
	}
	if got.Screening.PersonState != "automated" || got.Screening.PersonDetail != "no-reply sender address" || got.Screening.Score >= 30 {
		t.Fatalf("no-reply sender counted against the message: %+v", got.Screening)
	}
}

// When the model fails, a verified sender with clean checks is delivered with
// an info finding and the AI review is retried; a late danger verdict moves it
// to Spam as a rescan would. An unverified sender is still held.
func TestAIFailureDeliversOnlyVerifiedCleanMail(t *testing.T) {
	n := abuseNode(t, "ai-fallback.example")
	server, setSymbols, _ := switchableRspamd(t)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	fake := llm.From(n.server.Context()).Fake()
	fake.Reset()
	envelope := encodedEnvelope("203.0.113.7", "mx.alerts.example", "bounce@alerts.example")
	message := func(subject string) string {
		return "From: Alerts <no-reply@alerts.example>\r\nTo: " + n.box.Address + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYour weekly usage report is ready.\r\n"
	}

	setSymbols(alertPassSymbols)
	verified := relayWithEnvelope(t, n, "fallback-verified", message("Usage report"), envelope, envelope, 200)
	fake.Reply("the model connection dropped")
	runScreening(t, n, verified)
	got := n.detail(t, verified)
	if got.Screening.State != "clean" || got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "unavailable" {
		t.Fatalf("verified clean mail was held when the model failed: %+v", got.Screening)
	}
	info := finding(got.Screening, "ai_unavailable")
	if info == nil || info.Severity != "info" || !strings.Contains(info.Detail, "AI review unavailable") || !strings.Contains(info.Detail, "delivered because the sender is verified and other checks are clean") {
		t.Fatalf("missing AI unavailable info finding: %+v", got.Screening.Findings)
	}
	var retries int
	if err := db.From(n.server.Context()).QueryRow(n.server.Context(), "SELECT COUNT(*) FROM job WHERE kind=$1 AND unique_key LIKE $2", mailabuse.ScreenJob, verified.ID+":ai-retry:%").Scan(&retries); err != nil || retries != 1 {
		t.Fatalf("AI retry not scheduled: %d %v", retries, err)
	}

	// The retry comes too early: nothing changes and the model is not called.
	calls := len(fake.Calls())
	runScreening(t, n, verified)
	if len(fake.Calls()) != calls {
		t.Fatal("AI retry ran before its time")
	}
	// A retry that fails again leaves the delivered message as it was.
	n.server.Clock.Advance(10 * time.Minute)
	fake.Reply("still unreachable")
	runScreening(t, n, verified)
	if got = n.detail(t, verified); got.Screening.State != "clean" || got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "unavailable" || len(fake.Calls()) != calls+1 {
		t.Fatalf("failed AI retry disturbed the delivered message: %+v", got.Screening)
	}
	// A later danger verdict quarantines it and moves it to Spam.
	n.server.Clock.Advance(20 * time.Minute)
	screenAbuse(t, n, verified, 95, "phishing")
	if got = n.detail(t, verified); got.Screening.State != "quarantined" || got.Item.Folder != schema.MailFolderSpam || got.Screening.AiState != "complete" {
		t.Fatalf("late danger verdict did not quarantine: %+v folder %s", got.Screening, got.Item.Folder)
	}

	// A late uncertain verdict is not a danger verdict: the delivered message
	// stays in the Inbox with an info finding.
	late := relayWithEnvelope(t, n, "fallback-late-unknown", message("Usage report late"), envelope, envelope, 200)
	fake.Reply("the model connection dropped")
	runScreening(t, n, late)
	n.server.Clock.Advance(10 * time.Minute)
	screenAbuse(t, n, late, 75, "unknown")
	if got = n.detail(t, late); got.Screening.State != "clean" || got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "uncertain" {
		t.Fatalf("late uncertain verdict pulled delivered mail back: %+v folder %s", got.Screening, got.Item.Folder)
	}
	if f := finding(got.Screening, "ai_uncertain_after_delivery"); f == nil || f.Severity != "info" || !strings.Contains(f.Detail, "AI review uncertain after delivery") {
		t.Fatalf("late uncertain verdict not recorded: %+v", got.Screening.Findings)
	}

	// An unverified sender is held when the model fails, as before.
	setSymbols(trustFailSymbols)
	forged := relayWithEnvelope(t, n, "fallback-unverified", message("Usage report again"), envelope, envelope, 200)
	fake.Reply("the model connection dropped")
	runScreening(t, n, forged)
	if got = n.detail(t, forged); got.Screening.State != "error" || got.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("unverified mail was delivered without an AI verdict: %+v", got.Screening)
	}
	if f := finding(got.Screening, "ai_unavailable"); f == nil || f.Severity != "warning" {
		t.Fatalf("unverified AI failure lacks its warning: %+v", got.Screening.Findings)
	}

	// An uncertain verdict is a judgement, not a failure: still held.
	setSymbols(alertPassSymbols)
	uncertain := relayWithEnvelope(t, n, "fallback-uncertain", message("Usage report third"), envelope, envelope, 200)
	screenAbuse(t, n, uncertain, 40, "unknown")
	if got = n.detail(t, uncertain); got.Screening.State != "error" || got.Screening.AiState != "uncertain" || got.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("uncertain verdict was delivered: %+v", got.Screening)
	}

	// A verified no-reply sender with a benign verdict is never held for being no-reply.
	benign := relayWithEnvelope(t, n, "fallback-benign", message("Usage report fourth"), envelope, envelope, 200)
	screenAbuse(t, n, benign, 2, "benign")
	if got = n.detail(t, benign); got.Screening.State != "clean" || got.Item.Folder != schema.MailFolderInbox || got.Screening.PersonState != "automated" {
		t.Fatalf("no-reply sender was held: %+v", got.Screening)
	}
}
