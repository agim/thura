package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/agim/lidza/packs/llm"
	"thura/schema"
)

// switchableRspamd answers every scan with the symbols currently set, so one
// test can deliver authenticated and forged copies of the same sender.
func switchableRspamd(t *testing.T) (*httptest.Server, func(string), func() int) {
	t.Helper()
	var mu sync.Mutex
	symbols, calls := "{}", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		current := symbols
		mu.Unlock()
		fmt.Fprint(w, `{"score":0.4,"action":"no action","symbols":`+current+`}`)
	}))
	t.Cleanup(server.Close)
	return server, func(s string) { mu.Lock(); symbols = s; mu.Unlock() }, func() int { mu.Lock(); defer mu.Unlock(); return calls }
}

const (
	trustPassSymbols = `{"R_SPF_ALLOW":{"options":["+ip4:203.0.113.0/24"]},"R_DKIM_ALLOW":{"options":["vatreja.example:s=sel1"]},"DMARC_POLICY_ALLOW":{"options":["vatreja.example","reject"]}}`
	trustFailSymbols = `{"R_SPF_FAIL":{},"R_DKIM_NA":{},"DMARC_POLICY_REJECT":{"options":["vatreja.example","reject"]}}`
	trustMessage     = "From: Vatreja <no-reply@vatreja.example>\r\nTo: %s\r\nSubject: Your statement %s\r\nList-Unsubscribe: <mailto:unsubscribe@vatreja.example>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nYour monthly statement is ready.\r\n"
)

func hasFinding(s *schema.MailScreeningView, code string) bool {
	for _, f := range s.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// "Sender approved" trusts later mail from the address only when that mail's
// own SPF/DKIM/DMARC verifies; a no-reply, automated sender is never exempt
// from Rspamd, the model or the approval gate.
func TestTrustedSenderCountsOnlyWhenItsMailIsAuthenticated(t *testing.T) {
	n := abuseNode(t, "sender-trust.example")
	p := abuseSettings(t, n)
	p.ApproveUnknown = true
	saveAbuseSettings(t, n, p)
	server, setSymbols, rspamdCalls := switchableRspamd(t)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	fake := llm.From(n.server.Context()).Fake()
	fake.Reset()
	genuine := encodedEnvelope("203.0.113.7", "mx.vatreja.example", "bounce@vatreja.example")
	forged := encodedEnvelope("198.51.100.9", "spoof.invalid", "no-reply@vatreja.example")
	deliver := func(delivery, envelope string) schema.MailItem {
		raw := fmt.Sprintf(trustMessage, n.box.Address, delivery)
		return relayWithEnvelope(t, n, delivery, raw, envelope, envelope, 200)
	}

	// First message: an unknown sender is held for approval, then approved.
	setSymbols(trustPassSymbols)
	first := deliver("first", genuine)
	screenAbuse(t, n, first, 5, "benign")
	if got := n.detail(t, first); got.Screening.State != "quarantined" || !got.Screening.ApprovalRequired {
		t.Fatalf("unknown sender bypassed approval: %+v", got.Screening)
	}
	reviewAbuse(t, n, first, "approve-sender", 200)
	reviewAbuse(t, n, first, "release", 200)

	// A trusted address with verified authentication passes the gate, but only
	// once the scan has the result: at intake it stays held, never settled clean.
	second := deliver("second", genuine)
	if got := n.detail(t, second); got.Screening.State != "pending" || got.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("trusted relay mail settled before its authentication was known: %+v", got.Screening)
	}
	calls, scans := len(fake.Calls()), rspamdCalls()
	screenAbuse(t, n, second, 5, "benign")
	got := n.detail(t, second)
	if got.Screening.State != "clean" || got.Screening.ApprovalRequired || got.Item.Folder != schema.MailFolderInbox || got.Screening.SenderAuth == nil || !got.Screening.SenderAuth.Verified {
		t.Fatalf("verified trusted sender was not known: %+v", got.Screening)
	}
	// No-reply and List-Unsubscribe mark the sender automated; that answers only
	// the Real person question and skips no screening.
	if got.Screening.PersonState != "automated" || got.Screening.PersonSource != "header" {
		t.Fatalf("no-reply sender not decided by headers: %+v", got.Screening)
	}
	if len(fake.Calls()) != calls+1 || rspamdCalls() != scans+1 || got.Screening.AiState != "complete" {
		t.Fatal("automated trusted sender skipped the model or Rspamd")
	}

	// The same verified, trusted, automated sender is still quarantined on content.
	risky := deliver("risky", genuine)
	screenAbuse(t, n, risky, 92, "phishing")
	if got := n.detail(t, risky); got.Screening.State != "quarantined" || got.Screening.ApprovalRequired || !hasFinding(got.Screening, "ai_assessment") {
		t.Fatalf("trusted no-reply sender skipped content screening: %+v", got.Screening)
	}

	// The same address with failed authentication is held, with the spoofing finding.
	setSymbols(trustFailSymbols)
	forgery := deliver("forged", forged)
	screenAbuse(t, n, forgery, 5, "benign")
	got = n.detail(t, forgery)
	if got.Screening.State != "quarantined" || !got.Screening.ApprovalRequired || got.Item.Folder != schema.MailFolderSpam || got.Screening.Score < 70 {
		t.Fatalf("forged trusted sender was not held: %+v", got.Screening)
	}
	if !hasFinding(got.Screening, "trusted_sender_unverified") {
		t.Fatalf("forged trusted sender has no spoofing finding: %+v", got.Screening.Findings)
	}
	for _, f := range got.Screening.Findings {
		if f.Code == "trusted_sender_unverified" && (f.Severity != "danger" || f.Detail != "This sender is approved, but this message failed SPF/DKIM/DMARC — it may be forged") {
			t.Fatalf("spoofing finding: %+v", f)
		}
	}
	reviewAbuse(t, n, forgery, "release", 409)

	// Authentication not checked (no Rspamd): held for approval like an unknown
	// sender, without claiming a forgery.
	t.Setenv("MAIL_RSPAMD_URL", "")
	unchecked := receiveAbuse(t, n, "unchecked", "Vatreja <no-reply@vatreja.example>", "Your monthly statement is ready.", "", 200)
	screenAbuse(t, n, unchecked, 5, "benign")
	got = n.detail(t, unchecked)
	if got.Screening.State != "quarantined" || !got.Screening.ApprovalRequired || got.Screening.SenderAuth != nil || hasFinding(got.Screening, "trusted_sender_unverified") {
		t.Fatalf("unchecked trusted sender was not held for approval: %+v", got.Screening)
	}
	reviewAbuse(t, n, unchecked, "release", 409)
	// Approving the sender on that message approves the message itself.
	reviewAbuse(t, n, unchecked, "approve-sender", 200)
	reviewAbuse(t, n, unchecked, "release", 200)
}

// A workspace member's address claimed by relay mail that fails authentication
// gets the spoofing finding too.
func TestForgedMemberAddressIsFlagged(t *testing.T) {
	n := abuseNode(t, "member-forgery.example")
	server, setSymbols, _ := switchableRspamd(t)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	setSymbols(`{"R_SPF_FAIL":{},"R_DKIM_NA":{},"DMARC_POLICY_REJECT":{"options":["member-forgery.example","reject"]}}`)
	member := n.owner.Email
	if member == "" {
		t.Fatal("workspace owner has no address")
	}
	envelope := encodedEnvelope("198.51.100.9", "spoof.invalid", member)
	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Urgent\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlease review.\r\n", member, n.box.Address)
	item := relayWithEnvelope(t, n, "member-forged", raw, envelope, envelope, 200)
	screenAbuse(t, n, item, 5, "benign")
	got := n.detail(t, item)
	if !hasFinding(got.Screening, "trusted_sender_unverified") || got.Screening.State != "quarantined" {
		t.Fatalf("forged member address not flagged: %+v", got.Screening)
	}
}

// SMIP proves the sender itself: an approved SMIP sender stays known without
// any SPF/DKIM/DMARC result.
func TestApprovedSMIPSenderIsStillTrusted(t *testing.T) {
	a, b := abuseNode(t, "trust-smip-alpha.example"), abuseNode(t, "trust-smip-beta.example")
	t.Setenv("MAIL_RSPAMD_URL", "")
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	p := abuseSettings(t, b)
	p.ApproveUnknown = true
	saveAbuseSettings(t, b, p)
	receive := func() schema.MailItem {
		var before schema.MailItemList
		b.request(t, "GET", b.base()+"?folder=spam", nil, &before, 200)
		a.deliver(t, a.queue(t, b.box.Address))
		var after schema.MailItemList
		b.request(t, "GET", b.base()+"?folder=spam", nil, &after, 200)
		if len(after.Items) != len(before.Items)+1 {
			t.Fatal("SMIP mail was not held for screening")
		}
		seen := map[string]bool{}
		for _, i := range before.Items {
			seen[i.ID] = true
		}
		for _, i := range after.Items {
			if !seen[i.ID] {
				return i
			}
		}
		t.Fatal("new SMIP message not found")
		return schema.MailItem{}
	}
	first := receive()
	screenAbuse(t, b, first, 5, "benign")
	if got := b.detail(t, first); !got.Screening.Authenticated || !got.Screening.ApprovalRequired {
		t.Fatalf("unknown SMIP sender bypassed approval: %+v", got.Screening)
	}
	reviewAbuse(t, b, first, "approve-sender", 200)
	second := receive()
	if got := b.detail(t, second); got.Screening.ApprovalRequired {
		t.Fatalf("approved SMIP sender required approval at intake: %+v", got.Screening)
	}
	screenAbuse(t, b, second, 5, "benign")
	if got := b.detail(t, second); got.Screening.State != "clean" || got.Screening.ApprovalRequired || got.Item.Folder != schema.MailFolderInbox {
		t.Fatalf("approved SMIP sender was not trusted: %+v", got.Screening)
	}
}
