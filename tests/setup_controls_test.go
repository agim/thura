package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/lidzatest"
)

func setupDraft(t *testing.T, s *lidzatest.Server) map[string]string {
	t.Helper()
	_, draft, _ := setupRow(t, s)
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := credentials.Decrypt(key, draft)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	if err := json.Unmarshal(plain, &values); err != nil {
		t.Fatal(err)
	}
	return values
}

func setupPage(t *testing.T, s *lidzatest.Server, step string) string {
	t.Helper()
	response, err := s.Client().Get(s.URL + "/admin/setup?step=" + step)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("%s page: %d %v", step, response.StatusCode, err)
	}
	return string(body)
}

func TestSetupCheckboxesStoreBooleanStringsAndAcceptExplicitValues(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	// A browser form omits unchecked boxes and posts value="true" for checked ones.
	n, _, _ := setupRow(t, s)
	r := setupForm(t, s, "save", url.Values{"revision": {fmt.Sprint(n)}, "step": {"policy"}, "DRIVE_QUOTA_BYTES": {"1073741824"}, "THURA_MAX_MEMBERS": {"100"}, "MAIL_MAX_RECIPIENTS": {"50"}, "THURA_ALLOW_SHARES": {"true"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("checkbox save failed: " + r.Header.Get("Location"))
	}
	values := setupDraft(t, s)
	if values["THURA_ALLOW_SHARES"] != "true" || values["THURA_ADMIN_INVITES"] != "false" || values["THURA_ALLOW_ANONYMOUS_SHARES"] != "false" {
		t.Fatalf("checkbox values not stored as booleans: %q %q %q", values["THURA_ALLOW_SHARES"], values["THURA_ADMIN_INVITES"], values["THURA_ALLOW_ANONYMOUS_SHARES"])
	}
	page := setupPage(t, s, "policy")
	if !strings.Contains(page, `type="checkbox" id="THURA_ALLOW_SHARES" name="THURA_ALLOW_SHARES" value="true" checked`) || !strings.Contains(page, `type="checkbox" id="THURA_ADMIN_INVITES" name="THURA_ADMIN_INVITES" value="true">`) {
		t.Fatal("policy booleans are not rendered as checkboxes reflecting the draft")
	}
	// API clients still post explicit strings, including "false" for a present field.
	saveStep(t, s, "policy", map[string]string{"THURA_ADMIN_INVITES": "true", "THURA_ALLOW_SHARES": "false", "THURA_ALLOW_ANONYMOUS_SHARES": "true"})
	values = setupDraft(t, s)
	if values["THURA_ADMIN_INVITES"] != "true" || values["THURA_ALLOW_SHARES"] != "false" || values["THURA_ALLOW_ANONYMOUS_SHARES"] != "true" {
		t.Fatal("explicit true/false values were not accepted")
	}
	// Drafts accept any text, as for selects; validation still rejects a non-boolean at review.
	saveStep(t, s, "policy", map[string]string{"THURA_ADMIN_INVITES": "maybe"})
	if page := setupPage(t, s, "review"); !strings.Contains(page, "choose a valid Allow workspace admins to invite members") {
		t.Fatal("a non-boolean checkbox value passed validation")
	}
}

func TestSetupDerivesMailDomainAndPostfixHostnameWhenBlank(t *testing.T) {
	s := setupServer(t)
	t.Setenv("THURA_POSTFIX_TOKEN", postfixFixtureToken)
	claimSetup(t, s)
	saveStep(t, s, "server", map[string]string{"APP_URL": "https://Team.Example.org", "THURA_OPERATOR_EMAIL": "owner@setup.example", "THURA_BACKUP_PLAN": "Daily backups · retain 7 days"})
	page := setupPage(t, s, "mail")
	if !strings.Contains(page, `placeholder="mail.team.example.org"`) {
		t.Fatal("mail step does not show the derived Postfix hostname before saving")
	}
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_MAIL_OUTBOUND": "provider", "MAIL_PROVIDER": "mailgun", "MAIL_FROM": "Notices@Team.Example.org", "MAIL_API_KEY": "PUBLIC-synthetic-mailgun-key", "MAIL_DOMAIN": "", "THURA_POSTFIX_HOSTNAME": ""})
	values := setupDraft(t, s)
	if values["MAIL_DOMAIN"] != "team.example.org" || values["THURA_POSTFIX_HOSTNAME"] != "mail.team.example.org" {
		t.Fatalf("derived defaults were not applied: domain %q hostname %q", values["MAIL_DOMAIN"], values["THURA_POSTFIX_HOSTNAME"])
	}
	// Explicit values win over derivation, and the hidden SMTP fields keep their defaults.
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_MAIL_OUTBOUND": "provider", "MAIL_PROVIDER": "mailgun", "MAIL_FROM": "notices@team.example.org", "MAIL_DOMAIN": "mg.team.example.org", "THURA_POSTFIX_HOSTNAME": "mx.team.example.org", "MAIL_SMTP_PORT": ""})
	values = setupDraft(t, s)
	if values["MAIL_DOMAIN"] != "mg.team.example.org" || values["THURA_POSTFIX_HOSTNAME"] != "mx.team.example.org" || values["MAIL_SMTP_PORT"] != "587" {
		t.Fatalf("explicit values or hidden defaults lost: %q %q %q", values["MAIL_DOMAIN"], values["THURA_POSTFIX_HOSTNAME"], values["MAIL_SMTP_PORT"])
	}
	page = setupPage(t, s, "mail")
	for _, want := range []string{`data-show-when="[{&#34;MAIL_PROVIDER&#34;:[&#34;mailgun&#34;]}]"`, `data-derive-from="MAIL_FROM" data-derive-rule="email-domain"`, `Local Postfix submission`} {
		if !strings.Contains(page, want) {
			t.Fatalf("mail page lacks declarative control %q", want)
		}
	}
}

func TestSetupPostfixOutboundFillsLocalSubmissionAndKeepsProviderChoice(t *testing.T) {
	s := setupServer(t)
	t.Setenv("THURA_POSTFIX_TOKEN", postfixFixtureToken)
	claimSetup(t, s)
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "relay", "THURA_MAIL_OUTBOUND": "provider", "MAIL_PROVIDER": "sendgrid", "MAIL_FROM": "team@setup.example", "MAIL_API_KEY": "PUBLIC-synthetic-sendgrid-key", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters"})
	// Switching to Postfix sending pins SMTP to the loopback listener even when the
	// hidden fields still carry stale browser values.
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "relay", "THURA_MAIL_OUTBOUND": "postfix", "MAIL_PROVIDER": "sendgrid", "MAIL_FROM": "team@setup.example", "MAIL_SMTP_HOST": "smtp.stale.example", "MAIL_SMTP_PORT": "465", "MAIL_SMTP_SECURITY": "tls", "MAIL_SMTP_USERNAME": "stale", "MAIL_SMTP_PASSWORD": "stale", "THURA_POSTFIX_HOSTNAME": "mail.setup.example"})
	values := setupDraft(t, s)
	for key, want := range map[string]string{"MAIL_PROVIDER": "smtp", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": "2525", "MAIL_SMTP_SECURITY": "none", "MAIL_SMTP_USERNAME": "", "MAIL_SMTP_PASSWORD": ""} {
		if values[key] != want {
			t.Fatalf("%s = %q, wanted %q", key, values[key], want)
		}
	}
	if values["MAIL_API_KEY"] != "PUBLIC-synthetic-sendgrid-key" {
		t.Fatal("hidden provider key was dropped by the Postfix switch")
	}
}

func TestSetupLocalStorageIgnoresHiddenCloudFields(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	before := setupDraft(t, s)
	dir := t.TempDir()
	// A browser keeps posting the hidden S3 inputs; blank or stale values must not
	// be stored, and validation must not demand them for local storage.
	saveStep(t, s, "storage", map[string]string{"STORAGE_PROVIDER": "local", "STORAGE_DIR": dir, "STORAGE_ENDPOINT": "", "STORAGE_REGION": "", "STORAGE_BUCKET": "", "THURA_STORAGE_PROFILE": "aws"})
	values := setupDraft(t, s)
	if values["STORAGE_DIR"] != dir || values["STORAGE_ENDPOINT"] != before["STORAGE_ENDPOINT"] || values["STORAGE_REGION"] != before["STORAGE_REGION"] || values["THURA_STORAGE_PROFILE"] != before["THURA_STORAGE_PROFILE"] || values["STORAGE_PREFIX"] != before["STORAGE_PREFIX"] {
		t.Fatal("hidden cloud storage fields were overwritten under local storage")
	}
	page := setupPage(t, s, "review")
	if strings.Contains(page, "s3 HTTPS origin required") || strings.Contains(page, "s3 bucket, region and credentials required") {
		t.Fatal("review demanded hidden S3 fields for local storage")
	}
	page = setupPage(t, s, "storage")
	if !strings.Contains(page, `data-setup-field="STORAGE_BUCKET" data-show-when="[{&#34;STORAGE_PROVIDER&#34;:[&#34;s3&#34;]}]"`) {
		t.Fatal("storage page lacks the declarative S3 visibility rule")
	}
}

func TestSetupModelSuggestionsComeFromSavedLocalServiceAndFailClosed(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	page := setupPage(t, s, "ai")
	for _, want := range []string{`data-setup-suggest data-suggest-by="LLM_PROVIDER" data-suggest-live="ollama"`, `data-value="qwen2.5:0.5b" data-group="ollama"`, `data-group="anthropic"`, `data-derive-from="LLM_PROVIDER" data-derive-rule="map"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("AI page lacks suggestion control %q", want)
		}
	}
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"name":"llama3.2:1b"},{"name":"synthetic-installed:latest"}]}`)
	}))
	defer ollama.Close()
	lidza.Provide[http.RoundTripper](lidza.ServicesFrom(s.Context()), ollama.Client().Transport)
	saveStep(t, s, "ai", map[string]string{"THURA_AI_ENABLED": "true", "LLM_PROVIDER": "ollama", "LLM_MODEL": "llama3.2:1b", "LLM_BASE_URL": ollama.URL, "LLM_MAX_TOKENS": "1024"})
	models := func(status int) []string {
		t.Helper()
		response, err := s.Client().Get(s.URL + "/admin/setup/models.json")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("models.json: %d %s", response.StatusCode, body)
		}
		if status != 200 {
			return nil
		}
		if response.Header.Get("Content-Type") != "application/json" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("models.json served with an unsafe content type")
		}
		var out struct {
			Models []string `json:"models"`
		}
		if err := json.Unmarshal(body, &out); err != nil || out.Models == nil {
			t.Fatalf("models.json body: %s", body)
		}
		return out.Models
	}
	if got := models(200); strings.Join(got, ",") != "llama3.2:1b,synthetic-installed:latest" {
		t.Fatalf("installed models not suggested: %v", got)
	}
	// An unreachable or failing service yields no suggestions, never an error page.
	ollama.Close()
	if got := models(200); len(got) != 0 {
		t.Fatalf("closed service still produced suggestions: %v", got)
	}
	// A blank URL falls back to the derived local default; a hosted provider never asks Ollama for its models.
	saveStep(t, s, "ai", map[string]string{"THURA_AI_ENABLED": "true", "LLM_PROVIDER": "ollama", "LLM_MODEL": "llama3.2:1b", "LLM_BASE_URL": "", "LLM_MAX_TOKENS": "1024"})
	if values := setupDraft(t, s); values["LLM_BASE_URL"] != "http://127.0.0.1:11434" {
		t.Fatalf("blank Ollama URL was not derived: %q", values["LLM_BASE_URL"])
	}
	saveStep(t, s, "ai", map[string]string{"THURA_AI_ENABLED": "true", "LLM_PROVIDER": "anthropic", "LLM_MODEL": "claude-sonnet-5-5", "LLM_BASE_URL": "", "LLM_API_KEY": "PUBLIC-synthetic-key", "LLM_MAX_TOKENS": "1024"})
	if values := setupDraft(t, s); values["LLM_BASE_URL"] != "" {
		t.Fatal("hosted provider received a local service URL")
	}
	if got := models(200); len(got) != 0 {
		t.Fatalf("hosted provider produced local suggestions: %v", got)
	}
	// The endpoint stays behind the administrator gate.
	if r := s.JSON(t, "POST", "/api/v1/auth/logout", nil, nil); r.StatusCode != 204 {
		t.Fatal("logout failed")
	}
	client := *s.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(s.URL + "/admin/setup/models.json")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == 200 {
		t.Fatal("signed-out request read model suggestions")
	}
}
