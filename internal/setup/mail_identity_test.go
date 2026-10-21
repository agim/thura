package setup

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"thura/internal/smipmail"
)

func TestGuidedMailIdentityGenerationAndPreservation(t *testing.T) {
	now := time.Now()
	v := map[string]string{"SMIP_MAIL_ENABLED": "true", "MAIL_FROM": "sender@example.org", "THURA_MAIL_ADDRESS": "inbox@example.org"}
	if err := configureMailIdentity(v, url.Values{}, now); err != nil {
		t.Fatal(err)
	}
	var cfg smipmail.Config
	if err := json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "example.org" {
		t.Fatal("identity did not use inbox domain")
	}
	if _, err := smipmail.NewGateway(cfg, nil); err != nil {
		t.Fatal(err)
	}
	before := v["SMIP_MAIL_CONFIG"]
	if err := configureMailIdentity(v, url.Values{"smip_mail_domain": {"EXAMPLE.ORG"}}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v["SMIP_MAIL_CONFIG"] != before {
		t.Fatal("ordinary save rotated identity")
	}
	if err := configureMailIdentity(v, url.Values{"smip_mail_domain": {"other.example"}}, now); err == nil {
		t.Fatal("silently changed domain of existing identity")
	}
	if v["SMIP_MAIL_CONFIG"] != before {
		t.Fatal("failed save changed key")
	}
	view := mailIdentityView(v)
	if !view.Saved || view.Domain != "example.org" || view.KeyID == "" || view.Expires == "" {
		t.Fatal("missing public identity metadata")
	}
	raw, _ := json.Marshal(view)
	if string(raw) == "" || strings.Contains(string(raw), cfg.Seed) {
		t.Fatal("view disclosed seed")
	}
	other := map[string]string{"SMIP_MAIL_ENABLED": "true", "MAIL_FROM": "sender@example.org"}
	if err := configureMailIdentity(other, url.Values{}, now); err != nil {
		t.Fatal(err)
	}
	if other["SMIP_MAIL_CONFIG"] == before {
		t.Fatal("independent identities reused key")
	}
}

func TestGuidedMailIdentityRejectsInvalidDomainAndPreservesTrust(t *testing.T) {
	now := time.Now()
	for _, domain := range []string{"https://example.org", "other.example", "example.org/path"} {
		v := map[string]string{"SMIP_MAIL_ENABLED": "true", "MAIL_FROM": "sender@example.org"}
		if err := configureMailIdentity(v, url.Values{"smip_mail_domain": {domain}}, now); err == nil {
			t.Fatal("accepted invalid or mismatched domain", domain)
		}
		if v["SMIP_MAIL_CONFIG"] != "" {
			t.Fatal("generated identity after invalid input")
		}
	}
	v := map[string]string{"SMIP_MAIL_ENABLED": "false", "MAIL_FROM": "sender@example.org"}
	if err := configureMailIdentity(v, url.Values{}, now); err != nil || v["SMIP_MAIL_CONFIG"] != "" {
		t.Fatal("generated disabled identity")
	}
	v["SMIP_MAIL_ENABLED"] = "true"
	if err := configureMailIdentity(v, url.Values{}, now); err != nil {
		t.Fatal(err)
	}
	var cfg smipmail.Config
	if err := json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.RequiredDomains = []string{"partner.example"}
	raw, _ := json.Marshal(cfg)
	v["SMIP_MAIL_CONFIG"] = string(raw)
	if err := configureMailIdentity(v, url.Values{"smip_mail_domain": {"example.org"}}, now); err != nil {
		t.Fatal(err)
	}
	if v["SMIP_MAIL_CONFIG"] != string(raw) {
		t.Fatal("lost operator trust settings")
	}
}
