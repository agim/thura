package mailabuse

import (
	"encoding/json"
	"strings"
	"testing"

	q "thura/db/queries/gen"
	"thura/internal/rspamd"
)

func TestSMIPDeliveriesStaySMIPAuthenticated(t *testing.T) {
	smip := q.MailScreening{Sender: "peer@remote.example", Authenticated: true}
	view := senderAuthView(smip)
	if view == nil || view.Method != "smip" || !view.Verified || view.FromDomain != "remote.example" {
		t.Fatalf("SMIP view: %+v", view)
	}
	e := evidenceFor(true, rspamd.Authentication{FromDomain: "remote.example"}, false)
	if !e.Verified || e.Method != "smip" {
		t.Fatalf("SMIP evidence: %+v", e)
	}
}

func TestStoredAuthenticationAndUncheckedEvidence(t *testing.T) {
	if senderAuthView(q.MailScreening{Sender: "a@example.com"}) != nil {
		t.Fatal("an unchecked row must not claim a result")
	}
	row := q.MailScreening{Sender: "dana@forwarder.example", ClientAddress: "203.0.113.7", ClientHelo: "mx.forwarder.example", EnvelopeSender: "bounce@forwarder.example", Spf: "pass", Dkim: "none", Dmarc: "none"}
	view := senderAuthView(row)
	if view == nil || view.Method != "spf_dkim_dmarc" || !view.Verified || !view.SpfAligned || view.Spf != "pass" {
		t.Fatalf("aligned SPF pass: %+v", view)
	}
	row.EnvelopeSender = "bounce@esp.example"
	if view = senderAuthView(row); view.Verified || view.SpfAligned {
		t.Fatalf("unaligned SPF pass verified the From domain: %+v", view)
	}
	data, err := json.Marshal(evidenceFor(false, rspamd.Authentication{FromDomain: "forwarder.example"}, false))
	if err != nil || !strings.Contains(string(data), `"Verified":false,"Method":"not_checked"`) || !strings.Contains(string(data), `"SPF":"not_checked","SPFDomain":"","SPFAligned":false,"DKIM":"not_checked"`) {
		t.Fatalf("unchecked evidence: %s", data)
	}
}

func TestEnvelopeValidation(t *testing.T) {
	for _, valid := range []Envelope{{IP: "203.0.113.7", Helo: "mx.example", MailFrom: "a@example.com"}, {IP: "2001:db8::1"}} {
		if !ValidEnvelope(valid) {
			t.Errorf("rejected %+v", valid)
		}
	}
	for _, invalid := range []Envelope{{}, {IP: "unknown"}, {IP: "fe80::1%eth0"}, {IP: "203.0.113.7", Helo: "bad helo"}, {IP: "203.0.113.7", MailFrom: "a@example.com\r\nX: y"}, {IP: "203.0.113.7", Helo: strings.Repeat("a", 256)}} {
		if ValidEnvelope(invalid) {
			t.Errorf("accepted %+v", invalid)
		}
	}
}
