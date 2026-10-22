package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// The mail step renders its DNS instructions at once; the status follows
// from the administrator-only smip-status.json, and the no-script link asks
// for it inline.
func TestSetupMailStatusLoadsAfterThePage(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	form := url.Values{"step": {"mail"}, "smip_mail_domain": {"status.setup.example"}}
	for _, step := range setup.Catalog() {
		if step.ID == "mail" {
			for _, field := range step.Fields {
				form.Set(field.Name, field.Default)
			}
		}
	}
	form.Set("SMIP_MAIL_ENABLED", "true")
	form.Set("MAIL_FROM", "sender@status.setup.example")
	form.Set("THURA_MAIL_ADDRESS", "inbox@status.setup.example")
	revision, _, _ := setupRow(t, s)
	form.Set("revision", fmt.Sprint(revision))
	if response := setupForm(t, s, "save", form); strings.Contains(response.Header.Get("Location"), "error=") {
		t.Fatal(response.Header.Get("Location"))
	}
	get := func(client *http.Client, path string) (int, string) {
		t.Helper()
		response, err := client.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(body)
	}
	status, page := get(s.Client(), "/admin/setup?step=mail")
	if status != 200 || !strings.Contains(page, "_smip.status.setup.example") || !strings.Contains(page, "data-smip-status-pending") || !strings.Contains(page, "smip_status=now") || strings.Contains(page, "TLS 1.3 reachable") {
		t.Fatalf("mail step waited for the status or lost its instructions (%d)", status)
	}
	status, raw := get(s.Client(), "/admin/setup/smip-status.json")
	var reply struct {
		Available bool   `json:"available"`
		Domain    string `json:"domain"`
		TXT       string `json:"txt"`
		TLS       string `json:"tls"`
		CheckedAt string `json:"checkedAt"`
		Error     string `json:"error"`
	}
	if status != 200 || json.Unmarshal([]byte(raw), &reply) != nil || !reply.Available || reply.Domain != "status.setup.example" || reply.CheckedAt == "" || reply.Error != "" {
		t.Fatalf("status endpoint %d: %s", status, raw)
	}
	// Nothing publishes smip.status.setup.example: the status must not claim
	// the transport or the record.
	if reply.TLS != "no" || reply.TXT == "found" {
		t.Fatalf("unobserved status reported: %s", raw)
	}
	if strings.Contains(raw, "seed") || strings.Contains(raw, "SMIP_MAIL_CONFIG") {
		t.Fatal("status endpoint exposed the signing configuration")
	}
	status, page = get(s.Client(), "/admin/setup?step=mail&smip_status=now")
	if status != 200 || !strings.Contains(page, "TLS 1.3 reachable") {
		t.Fatalf("no-script recheck did not render the status (%d)", status)
	}
	stranger := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if status, raw = get(stranger, "/admin/setup/smip-status.json"); status == 200 && strings.Contains(raw, "status.setup.example") {
		t.Fatal("signed-out visitor read the SMIP status")
	}
}
