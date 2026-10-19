package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"thura/app"
)

const postfixFixtureToken = "PUBLIC-private-postfix-sidecar-token-384792"

type postfixConfigResult struct {
	Enabled  bool   `json:"enabled"`
	Listen   string `json:"listen"`
	Hostname string `json:"hostname"`
	Revision int    `json:"revision"`
	Config   struct {
		BaseURL   string `json:"baseUrl"`
		Mailboxes map[string]struct {
			ID     string `json:"id"`
			Secret string `json:"secret"`
		} `json:"mailboxes"`
	} `json:"config"`
}

func postfixConfig(t *testing.T, s *lidzatest.Server, token string, code int) (postfixConfigResult, string) {
	t.Helper()
	req, err := http.NewRequest("GET", s.URL+"/api/v1/setup/postfix", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != code {
		t.Fatalf("Postfix status: %d wanted %d: %s", response.StatusCode, code, body)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("private config was cacheable")
	}
	var result postfixConfigResult
	if code == 200 {
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
	}
	return result, string(body)
}
func TestPostfixBootstrapPublicationActivationAndHybridSending(t *testing.T) {
	s := setupServer(t)
	t.Setenv("THURA_POSTFIX_TOKEN", postfixFixtureToken)
	postfixConfig(t, s, "", 403)
	postfixConfig(t, s, "wrong", 403)
	initial, _ := postfixConfig(t, s, postfixFixtureToken, 200)
	if initial.Listen != "127.0.0.1:2525" || len(initial.Config.Mailboxes) != 0 {
		t.Fatal("fresh setup exposed receiving")
	}
	claimSetup(t, s)
	completeSetup(t, s)
	// Managed receiving must not replace an independently selected SMTP sender.
	port, _ := setupSMTP(t)
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_POSTFIX_HOSTNAME": "mail.setup.example", "THURA_MAIL_ADDRESS": "inbox@setup.example", "MAIL_FROM": "sender@setup.example", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": port, "MAIL_SMTP_SECURITY": "none"})
	n, _, _ := setupRow(t, s)
	draft, _ := postfixConfig(t, s, postfixFixtureToken, 200)
	if draft.Hostname != "mail.setup.example" || draft.Listen == "25" || len(draft.Config.Mailboxes) != 0 {
		t.Fatal("draft activated receiving")
	}
	// Only the mail connection changed, so run new provider checks.
	runSetupChecks(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(n)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal(r.Header.Get("Location"))
	}
	postfixConfig(t, s, postfixFixtureToken, 503)
	active := lidzatest.Start(t, app.New(nil))
	cfg, body := postfixConfig(t, active, postfixFixtureToken, 200)
	entry, ok := cfg.Config.Mailboxes["inbox@setup.example"]
	if !cfg.Enabled || cfg.Listen != "25" || !ok || len(entry.Secret) < 32 || entry.ID == "" || cfg.Config.BaseURL != "http://127.0.0.1:3000" {
		t.Fatal("activated mailbox mapping missing")
	}
	if strings.Contains(body, "MAIL_API_KEY") || strings.Contains(body, "SMTP_PASSWORD") || strings.Contains(body, "sender@setup.example") {
		t.Fatal("sidecar leaked unrelated sender settings")
	}
	// New mailboxes become available without regenerating a hand-written map.
	additional := uuid.NewString()
	if _, err := db.From(active.Context()).Exec(active.Context(), "INSERT INTO mailbox(id,workspace_id,name,address,config_prefix) SELECT $1,workspace_id,'Added inbox','added@setup.example','' FROM mailbox WHERE id=$2", additional, entry.ID); err != nil {
		t.Fatal(err)
	}
	refreshed, _ := postfixConfig(t, active, postfixFixtureToken, 200)
	if refreshed.Config.Mailboxes["added@setup.example"].ID != additional || len(refreshed.Config.Mailboxes) != 2 {
		t.Fatal("new mailbox was not discovered")
	}
	// A draft provider change never changes active ingress or exposes its API key.
	saveStep(t, active, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_POSTFIX_HOSTNAME": "different.example", "MAIL_PROVIDER": "mailgun", "MAIL_FROM": "sender@setup.example", "MAIL_DOMAIN": "setup.example", "MAIL_API_KEY": "PUBLIC-hybrid-private-key"})
	preserved, raw := postfixConfig(t, active, postfixFixtureToken, 200)
	if preserved.Hostname != cfg.Hostname || preserved.Revision != cfg.Revision || strings.Contains(raw, "PUBLIC-hybrid-private-key") {
		t.Fatal("draft mutated active mail configuration")
	}
}

func TestPostfixWizardNormalizesLocalSenderAndHidesSavedSecrets(t *testing.T) {
	s := setupServer(t)
	t.Setenv("THURA_POSTFIX_TOKEN", postfixFixtureToken)
	claimSetup(t, s)
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_MAIL_OUTBOUND": "postfix", "THURA_POSTFIX_HOSTNAME": "mail.setup.example", "MAIL_FROM": "team@setup.example"})
	_, draft, _ := setupRow(t, s)
	// Check the encrypted snapshot using the same public sealing boundary as other setup tests.
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := credentials.Decrypt(key, draft)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	if err = json.Unmarshal(plain, &values); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"MAIL_PROVIDER": "smtp", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": "2525", "MAIL_SMTP_SECURITY": "none"} {
		if values[key] != want {
			t.Fatalf("%s = %s, wanted %s", key, values[key], want)
		}
	}
	response, err := s.Client().Get(s.URL + "/admin/setup?step=mail")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "Receive email through") || !strings.Contains(string(body), "Send email through") || strings.Contains(string(body), values["MAIL_INBOUND_SECRET"]) {
		t.Fatal("wizard missed independent choices or leaked secret")
	}
	t.Setenv("THURA_POSTFIX_TOKEN", "")
	postfixConfig(t, s, postfixFixtureToken, 403)

}
