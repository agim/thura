package tests

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	"thura/internal/federation"
	"thura/internal/setup"
	"thura/internal/smip"
	"thura/internal/smipmail"
	"thura/schema"
)

func coreSMIPFixture(t *testing.T) (string, string) {
	t.Helper()
	now := time.Now().Unix()
	seed, _, _ := smipKeys("setup.example", now)
	_, _, peerKeys := smipKeys("paired.example", now)
	chat, err := json.Marshal(federation.Config{Domain: "setup.example", Seed: seed, NotBefore: now - 86400, NotAfter: now + 86400*365, Peers: map[string]map[string]smip.Key{"paired.example": peerKeys}})
	if err != nil {
		t.Fatal(err)
	}
	mail, err := json.Marshal(smipmail.Config{Domain: "setup.example", Seed: seed, NotBefore: now - 86400, NotAfter: now + 86400*365})
	if err != nil {
		t.Fatal(err)
	}
	return string(chat), string(mail)
}

func TestWizardModelChangesInSettingsSurviveActivation(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("core setup publication failed")
	}
	if err := setup.Activate(s.Context(), s.Services); err != nil {
		t.Fatal(err)
	}
	var workspace string
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT workspace_id FROM server_setup").Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	var settings schema.AISettings
	response := s.JSON(t, "PUT", "/api/v1/workspaces/"+workspace+"/ai/settings", schema.AISettingsInput{Enabled: true, Provider: "ollama", Model: "replacement-local-model", MaxTokens: 1024}, &settings)
	if response.StatusCode != 200 || settings.Model != "replacement-local-model" {
		t.Fatal("AI Settings did not activate the changed model", response.StatusCode)
	}
	if err := setup.Activate(s.Context(), s.Services); err != nil {
		t.Fatal(err)
	}
	if llm.From(s.Context()).Model() != "replacement-local-model" {
		t.Fatal("wizard snapshot reverted the later Settings change")
	}
}

func TestSetupCannotPublishWithMissingCoreService(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	// Keep mail/storage probes valid: this failure must come from core services.
	saveStep(t, s, "integrations", map[string]string{"SMIP_ENABLED": "false"})
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("missing core services were published")
	}
	_, _, published := setupRow(t, s)
	if published != "" {
		t.Fatal("failed core validation changed active configuration")
	}
}
