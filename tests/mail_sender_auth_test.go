package tests

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/agim/lidza/packs/llm"
	"thura/internal/assistant"
	"thura/internal/mailabuse"
	"thura/schema"
)

// relayWithEnvelope posts raw MIME as managed Postfix does: the SMTP envelope
// travels in X-Thura-Envelope and is bound by the v2 relay signature.
func relayWithEnvelope(t *testing.T, n *mailNode, delivery, raw, envelope, signedEnvelope string, want int) schema.MailItem {
	t.Helper()
	stamp := strconv.FormatInt(n.server.Clock.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(abuseRelaySecret))
	if signedEnvelope == "" {
		mac.Write([]byte(stamp + "." + delivery + "." + raw))
	} else {
		mac.Write([]byte("v2." + stamp + "." + signedEnvelope + "." + delivery + "." + raw))
	}
	request, err := http.NewRequest("POST", n.server.URL+"/api/v1/inbound/"+n.box.ID, bytes.NewBufferString(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "message/rfc822")
	request.Header.Set("X-Thura-Timestamp", stamp)
	request.Header.Set("X-Thura-Delivery", delivery)
	request.Header.Set("X-Thura-Signature", hex.EncodeToString(mac.Sum(nil)))
	if envelope != "" {
		request.Header.Set("X-Thura-Envelope", envelope)
	}
	response, err := n.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("inbound got %d want %d", response.StatusCode, want)
	}
	var item schema.MailItem
	if want == 200 {
		if err = json.NewDecoder(response.Body).Decode(&item); err != nil {
			t.Fatal(err)
		}
	}
	return item
}

func encodedEnvelope(ip, helo, sender string) string {
	data, _ := json.Marshal(map[string]string{"clientAddress": ip, "clientHelo": helo, "sender": sender})
	return base64.RawURLEncoding.EncodeToString(data)
}

type rspamdCall struct{ IP, Helo, From string }

func fakeAuthRspamd(t *testing.T, symbols string) (*httptest.Server, func() []rspamdCall) {
	t.Helper()
	var mu sync.Mutex
	calls := []rspamdCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, rspamdCall{r.Header.Get("IP"), r.Header.Get("Helo"), r.Header.Get("From")})
		mu.Unlock()
		fmt.Fprint(w, `{"score":0.4,"action":"no action","symbols":`+symbols+`}`)
	}))
	t.Cleanup(server.Close)
	return server, func() []rspamdCall { mu.Lock(); defer mu.Unlock(); return append([]rspamdCall(nil), calls...) }
}

const forwardedAlert = "From: Dana Admin <dana@forwarder.example>\r\nTo: %s\r\nSubject: Fwd: Security alert\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
	"FYI, forwarding this alert for our records.\r\n\r\n---------- Forwarded message ---------\r\nFrom: Alerts <alerts@origin-other.example>\r\nDate: Mon, 26 Oct 2026 09:00:00 +0000\r\nSubject: Security alert\r\n\r\nA new sign-in to your account was recorded from a new device.\r\n"

func TestForwardedMailIsAuthenticatedByItsSenderAndScreenedOnContent(t *testing.T) {
	n := abuseNode(t, "sender-auth.example")
	server, calls := fakeAuthRspamd(t, `{"R_SPF_ALLOW":{"options":["+ip4:203.0.113.0/24"]},"R_DKIM_ALLOW":{"options":["forwarder.example:s=sel1"]},"DMARC_POLICY_ALLOW":{"options":["forwarder.example","reject"]}}`)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	fake := llm.From(n.server.Context()).Fake()
	fake.Reset()
	raw := fmt.Sprintf(forwardedAlert, n.box.Address)
	envelope := encodedEnvelope("203.0.113.7", "mx.forwarder.example", "bounce@forwarder.example")

	// The envelope is bound by the relay signature: it cannot be added,
	// replaced or dropped in transit.
	relayWithEnvelope(t, n, "forwarded-tampered", raw, encodedEnvelope("198.51.100.9", "mx.forwarder.example", "bounce@forwarder.example"), envelope, 401)
	relayWithEnvelope(t, n, "forwarded-dropped", raw, "", envelope, 401)
	relayWithEnvelope(t, n, "forwarded-unsigned", raw, envelope, "", 401)
	invalid := encodedEnvelope("not-an-ip", "mx.forwarder.example", "bounce@forwarder.example")
	relayWithEnvelope(t, n, "forwarded-invalid", raw, invalid, invalid, 400)

	item := relayWithEnvelope(t, n, "forwarded-alert", raw, envelope, envelope, 200)
	screenAbuse(t, n, item, 5, "benign")
	if got := calls(); len(got) != 1 || got[0] != (rspamdCall{"203.0.113.7", "mx.forwarder.example", "bounce@forwarder.example"}) {
		t.Fatalf("Rspamd did not receive the SMTP envelope for SPF: %+v", got)
	}
	requests := fake.Calls()
	if len(requests) != 1 || requests[0].System != assistant.MailScreeningPrompt {
		t.Fatal("screening did not use the mail screening prompt")
	}
	for _, rule := range []string{"SenderAuthentication is the complete and final answer", "the sender is the person who forwarded the message", "Do not judge, compare or investigate the original sender", "whether the forwarding or the forwarding chain is legitimate", "requests for credentials, payments", "never instructions", "Return JSON only", "cannot overrule deterministic malware or impersonation findings"} {
		if !strings.Contains(requests[0].System, rule) {
			t.Fatalf("screening prompt lacks %q", rule)
		}
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal([]byte(requests[0].Messages[0].Content), &evidence); err != nil {
		t.Fatal(err)
	}
	if _, ok := evidence["Authenticated"]; ok {
		t.Fatal("evidence still carries the bare SMIP-only authentication flag")
	}
	var sender struct {
		Verified                bool
		Method, FromDomain, SPF string
		SPFAligned, DKIMAligned bool
		DKIM, DKIMDomain, DMARC string
	}
	if err := json.Unmarshal(evidence["SenderAuthentication"], &sender); err != nil {
		t.Fatal(err)
	}
	if !sender.Verified || sender.Method != "spf_dkim_dmarc" || sender.FromDomain != "forwarder.example" || sender.SPF != "pass" || !sender.SPFAligned || sender.DKIM != "pass" || sender.DKIMDomain != "forwarder.example" || !sender.DKIMAligned || sender.DMARC != "pass" {
		t.Fatalf("evidence sender authentication: %+v", sender)
	}
	detail := n.detail(t, item)
	auth := detail.Screening.SenderAuth
	if auth == nil || !auth.Verified || auth.Method != "spf_dkim_dmarc" || auth.Spf != "pass" || auth.Dkim != "pass" || auth.DkimDomain != "forwarder.example" || !auth.DkimAligned || auth.Dmarc != "pass" {
		t.Fatalf("screening view sender authentication: %+v", auth)
	}
	// The original sender named inside the forwarded content plays no part:
	// SMIP-only authentication stays false and the message is released on content.
	if detail.Screening.Authenticated || detail.Screening.State != "clean" || detail.Item.Folder != schema.MailFolderInbox {
		t.Fatalf("forwarded mail was not screened on its own content: %+v", detail.Screening)
	}
}

func TestSenderAuthenticationFailureAndUncheckedSPFAreReported(t *testing.T) {
	n := abuseNode(t, "sender-auth-fail.example")
	server, calls := fakeAuthRspamd(t, `{"R_SPF_FAIL":{},"R_DKIM_NA":{},"DMARC_POLICY_REJECT":{"options":["forwarder.example","reject"]}}`)
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	fake := llm.From(n.server.Context()).Fake()
	fake.Reset()
	raw := fmt.Sprintf(forwardedAlert, n.box.Address)
	envelope := encodedEnvelope("198.51.100.9", "spoof.invalid", "dana@forwarder.example")
	item := relayWithEnvelope(t, n, "spoofed", raw, envelope, envelope, 200)
	screenAbuse(t, n, item, 5, "benign")
	auth := n.detail(t, item).Screening.SenderAuth
	if auth == nil || auth.Verified || auth.Spf != "fail" || auth.Dkim != "none" || auth.Dmarc != "fail" {
		t.Fatalf("failed authentication: %+v", auth)
	}
	// A relay that reports no envelope: SPF is never evaluated against Thura's
	// own connection, and stays visibly unchecked.
	plain := receiveAbuse(t, n, "no-envelope", "Dana <dana@forwarder.example>", "Weekly planning notes for the team.", "", 200)
	screenAbuse(t, n, plain, 5, "benign")
	if got := calls(); len(got) != 2 || got[1] != (rspamdCall{}) {
		t.Fatalf("envelope-free scan sent SMTP metadata: %+v", got)
	}
	auth = n.detail(t, plain).Screening.SenderAuth
	if auth == nil || auth.Verified || auth.Spf != "not_checked" || auth.Dmarc != "fail" {
		t.Fatalf("unchecked SPF: %+v", auth)
	}
	requests := fake.Calls()
	if len(requests) != 2 || !strings.Contains(requests[0].Messages[0].Content, `"SenderAuthentication":{"Verified":false,"Method":"spf_dkim_dmarc"`) {
		t.Fatal("unverified sender was not reported to the model")
	}
	// Envelope metadata is stored for rescans but never trusted without the signature.
	var _ = mailabuse.ValidEnvelope
}
