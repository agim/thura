package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/agim/lidza/pkg/credentials"
	"thura/internal/setup"
	"thura/internal/smipmail"
)

func TestSetupGuidedMailIdentityIsEncryptedStableAndNeverRendered(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	form := url.Values{"step": {"mail"}, "smip_mail_domain": {"setup.example"}}
	for _, step := range setup.Catalog() {
		if step.ID == "mail" {
			for _, field := range step.Fields {
				form.Set(field.Name, field.Default)
			}
		}
	}
	form.Set("SMIP_MAIL_ENABLED", "true")
	form.Set("MAIL_FROM", "sender@setup.example")
	form.Set("THURA_MAIL_ADDRESS", "inbox@setup.example")
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 0; i < 2; i++ {
		revision, _, _ := setupRow(t, s)
		form.Set("revision", fmt.Sprint(revision))
		response := setupForm(t, s, "save", form)
		if strings.Contains(response.Header.Get("Location"), "error=") {
			t.Fatal(response.Header.Get("Location"))
		}
		_, draft, _ := setupRow(t, s)
		plain, err := credentials.Decrypt(key, draft)
		if err != nil {
			t.Fatal(err)
		}
		var values map[string]string
		if err = json.Unmarshal(plain, &values); err != nil {
			t.Fatal(err)
		}
		raw := values["SMIP_MAIL_CONFIG"]
		var cfg smipmail.Config
		if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, err = smipmail.NewGateway(cfg, nil); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = raw
		} else if raw != first {
			t.Fatal("saving draft regenerated signing identity")
		}
		if strings.Contains(draft, cfg.Seed) {
			t.Fatal("draft stored plaintext seed")
		}
		page, err := s.Client().Get(s.URL + "/admin/setup?step=mail")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(page.Body)
		page.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		html := string(body)
		for _, value := range []string{cfg.Seed, `name="SMIP_MAIL_CONFIG"`, "SMIP email identity JSON"} {
			if strings.Contains(html, value) {
				t.Fatal("raw signing configuration entered wizard output")
			}
		}
		for _, value := range []string{"Identity saved for", "_smip.setup.example", "smip.setup.example", "v=SMIP1", "direct TLS 1.3"} {
			if !strings.Contains(html, value) {
				t.Fatal("missing guided identity instructions", value)
			}
		}
	}
	// A stale tab cannot replace the generated identity or lose its saved revision.
	form.Set("revision", "0")
	setupForm(t, s, "save", form)
	_, draft, _ := setupRow(t, s)
	plain, err := credentials.Decrypt(key, draft)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err = json.Unmarshal(plain, &values); err != nil {
		t.Fatal(err)
	}
	if values["SMIP_MAIL_CONFIG"] != first {
		t.Fatal("stale save replaced identity")
	}
}
