package rspamd

import (
	"encoding/json"
	"testing"
)

func scanned(t *testing.T, symbols string) Result {
	t.Helper()
	var r Result
	if err := json.Unmarshal([]byte(`{"score":0,"action":"no action","symbols":`+symbols+`}`), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

var smtp = Envelope{IP: "203.0.113.7", Helo: "mx.forwarder.example", MailFrom: "bounce@mail.forwarder.example"}

func TestAuthenticationPassesWithAlignedSPFDKIMAndDMARC(t *testing.T) {
	r := scanned(t, `{"R_SPF_ALLOW":{"name":"R_SPF_ALLOW","score":-0.2,"options":["+ip4:203.0.113.0/24"]},
		"R_DKIM_ALLOW":{"name":"R_DKIM_ALLOW","score":-0.2,"options":["esp.example:s=bulk","forwarder.example:s=sel1"]},
		"DMARC_POLICY_ALLOW":{"name":"DMARC_POLICY_ALLOW","score":-0.5,"options":["forwarder.example","reject"]}}`)
	a := r.Authentication("Forwarder.example", smtp)
	if a.SPF != Pass || !a.SPFAligned || a.SPFDomain != "mail.forwarder.example" {
		t.Fatalf("spf: %+v", a)
	}
	// The aligned signature is reported even when another signer comes first.
	if a.DKIM != Pass || a.DKIMDomain != "forwarder.example" || !a.DKIMAligned {
		t.Fatalf("dkim: %+v", a)
	}
	if a.DMARC != Pass || !a.Verified() || a.FromDomain != "forwarder.example" {
		t.Fatalf("dmarc: %+v", a)
	}
}

func TestAuthenticationFailuresAndMissingResults(t *testing.T) {
	cases := []struct {
		symbols       string
		envelope      Envelope
		spf, dkim, dm string
		dkimDomain    string
		dkimAligned   bool
		verified      bool
	}{
		{`{"R_SPF_FAIL":{},"R_DKIM_REJECT":{"options":["forwarder.example:s=sel1"]},"DMARC_POLICY_REJECT":{"options":["forwarder.example","reject"]}}`, smtp, Fail, Fail, Fail, "forwarder.example", false, false},
		{`{"R_SPF_SOFTFAIL":{},"R_DKIM_NA":{},"DMARC_POLICY_SOFTFAIL":{}}`, smtp, SoftFail, None, Fail, "", false, false},
		{`{"R_SPF_NEUTRAL":{},"R_DKIM_TEMPFAIL":{"options":["forwarder.example:s=x"]},"DMARC_DNSFAIL":{}}`, smtp, Neutral, TempError, TempError, "forwarder.example", false, false},
		{`{"R_SPF_DNSFAIL":{},"R_DKIM_PERMFAIL":{},"DMARC_BAD_POLICY":{}}`, smtp, TempError, PermError, PermError, "", false, false},
		{`{"R_SPF_PERMFAIL":{}}`, smtp, PermError, None, NotChecked, "", false, false},
		{`{"R_SPF_NA":{},"R_DKIM_NA":{},"DMARC_NA":{}}`, smtp, None, None, None, "", false, false},
		// Without the SMTP client IP, an SPF symbol cannot describe the sender.
		{`{"R_SPF_ALLOW":{},"R_DKIM_NA":{},"DMARC_NA":{}}`, Envelope{}, NotChecked, None, None, "", false, false},
		// Aligned DKIM alone authenticates the sender, even without SPF or DMARC.
		{`{"R_DKIM_ALLOW":{"options":["news.forwarder.example:s=a"]}}`, Envelope{}, NotChecked, Pass, NotChecked, "news.forwarder.example", true, true},
		// A third-party signature verifies but does not authenticate the From domain.
		{`{"R_SPF_ALLOW":{},"R_DKIM_ALLOW":{"options":["esp.example:s=a"]},"DMARC_NA":{}}`, Envelope{IP: "203.0.113.7", MailFrom: "bounce@esp.example"}, Pass, Pass, None, "esp.example", false, false},
	}
	for i, c := range cases {
		a := scanned(t, c.symbols).Authentication("forwarder.example", c.envelope)
		if a.SPF != c.spf || a.DKIM != c.dkim || a.DMARC != c.dm || a.DKIMDomain != c.dkimDomain || a.DKIMAligned != c.dkimAligned || a.Verified() != c.verified {
			t.Errorf("case %d: %+v", i, a)
		}
	}
}

func TestAlignmentIsRelaxedAndSPFUsesHeloForBounces(t *testing.T) {
	if !Aligned("mail.example.co.uk", "EXAMPLE.co.uk.") || Aligned("example.co.uk", "other.co.uk") || Aligned("", "") {
		t.Fatal("relaxed organizational-domain alignment")
	}
	bounce := Envelope{IP: "203.0.113.7", Helo: "mx.forwarder.example"}
	a := scanned(t, `{"R_SPF_ALLOW":{}}`).Authentication("forwarder.example", bounce)
	if a.SPFDomain != "mx.forwarder.example" || !a.SPFAligned || !a.Verified() {
		t.Fatalf("null sender: %+v", a)
	}
	if SPFDomain(smtp, NotChecked) != "" {
		t.Fatal("unchecked SPF has no identity")
	}
	if (Authentication{DMARC: Pass}).Verified() {
		t.Fatal("verified without a From domain")
	}
}
