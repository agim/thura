package setup

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	q "thura/db/queries/gen"
)

const apiMailProviders = "mailgun sendgrid postmark resend"

// Visibility is data: the page applies the same rules in the browser and
// Save applies them to the submitted form, so a hidden field keeps its saved
// value and validation never demands it.
func fieldVisibility() map[string][]Condition {
	provider := Condition{"THURA_MAIL_OUTBOUND": {"provider"}}
	return map[string][]Condition{
		"MAIL_PROVIDER":          {provider},
		"MAIL_API_KEY":           {{"MAIL_PROVIDER": strings.Fields(apiMailProviders)}},
		"MAIL_DOMAIN":            {{"MAIL_PROVIDER": {"mailgun"}}},
		"MAIL_REGION":            {{"MAIL_PROVIDER": {"mailgun", "sendgrid"}}},
		"MAIL_SMTP_HOST":         {{"MAIL_PROVIDER": {"smtp"}}},
		"MAIL_SMTP_PORT":         {{"MAIL_PROVIDER": {"smtp"}}},
		"MAIL_SMTP_SECURITY":     {{"MAIL_PROVIDER": {"smtp"}}},
		"MAIL_SMTP_USERNAME":     {{"MAIL_PROVIDER": {"smtp"}}},
		"MAIL_SMTP_PASSWORD":     {{"MAIL_PROVIDER": {"smtp"}}},
		"THURA_POSTFIX_HOSTNAME": {{"THURA_MAIL_INBOUND": {"postfix"}}, {"THURA_MAIL_OUTBOUND": {"postfix"}}},
		"MAIL_INBOUND_SECRET":    {{"THURA_MAIL_INBOUND": {"relay"}}},
		"STORAGE_DIR":            {{"STORAGE_PROVIDER": {"local"}}},
		"THURA_STORAGE_PROFILE":  {{"STORAGE_PROVIDER": {"s3"}}},
		"THURA_STORAGE_LOCATION": {{"STORAGE_PROVIDER": {"s3"}, "THURA_STORAGE_PROFILE": {"aws", "digitalocean"}}},
		"STORAGE_ENDPOINT":       {{"STORAGE_PROVIDER": {"s3"}}},
		"STORAGE_REGION":         {{"STORAGE_PROVIDER": {"s3"}}},
		"STORAGE_BUCKET":         {{"STORAGE_PROVIDER": {"s3"}}},
		"STORAGE_ACCESS_KEY":     {{"STORAGE_PROVIDER": {"s3"}}},
		"STORAGE_SECRET_KEY":     {{"STORAGE_PROVIDER": {"s3"}}},
		"LLM_API_KEY":            {{"LLM_PROVIDER": {"openai", "anthropic", "google", "compatible"}}},
	}
}

// Short curated lists; free text is always accepted. Ollama's installed
// models are added live from the setup page's models.json endpoint.
func modelSuggestions() map[string][]string {
	return map[string][]string{
		"ollama":    {minimumModel, "llama3.2:1b", llm.DefaultModels["ollama"], "qwen2.5:3b", "mistral"},
		"openai":    {llm.DefaultModels["openai"]},
		"anthropic": {llm.DefaultModels["anthropic"], "claude-opus-5-5", "claude-haiku-5-5"},
		"google":    {llm.DefaultModels["google"]},
	}
}

func decorate(f *Field) {
	f.ShowWhen = fieldVisibility()[f.Name]
	switch f.Name {
	case "LLM_MODEL":
		for _, provider := range []string{"ollama", "openai", "anthropic", "google"} {
			for _, model := range modelSuggestions()[provider] {
				f.Suggestions = append(f.Suggestions, Suggestion{Value: model, Label: model, Group: provider})
			}
		}
	case "DRIVE_QUOTA_BYTES":
		f.Inputmode = "numeric"
		for _, gib := range []int64{1, 5, 10, 50, 100} {
			f.Suggestions = append(f.Suggestions, Suggestion{Value: strconv.FormatInt(gib<<30, 10), Label: strconv.FormatInt(gib, 10) + " GiB"})
		}
	case "MAIL_SMTP_PORT":
		f.Inputmode = "numeric"
		for _, port := range []string{"587", "465", "25", "2525"} {
			f.Suggestions = append(f.Suggestions, Suggestion{Value: port, Label: port})
		}
	case "MAIL_DOMAIN":
		f.DeriveFrom, f.DeriveRule = "MAIL_FROM", "email-domain"
	case "LLM_BASE_URL":
		f.DeriveFrom, f.DeriveRule = "LLM_PROVIDER", "map"
		f.DeriveMap = map[string]string{"ollama": managedModelURL, "compatible": "https://… (required)", "openai": "Provider default", "anthropic": "Provider default", "google": "Provider default"}
	}
}

func (c Condition) holds(v map[string]string) bool {
	for name, allowed := range c {
		ok := false
		for _, value := range allowed {
			ok = ok || v[name] == value
		}
		if !ok || !visible(v, name) {
			return false
		}
	}
	return true
}

// visible follows controlling fields, so a field whose controller is hidden
// is hidden as well. Rules only reference fields of the same step.
func visible(v map[string]string, name string) bool {
	rules := fieldVisibility()[name]
	if len(rules) == 0 {
		return true
	}
	for _, rule := range rules {
		if rule.holds(v) {
			return true
		}
	}
	return false
}

func (f Field) ShowWhenJSON() string {
	if len(f.ShowWhen) == 0 {
		return ""
	}
	raw, err := json.Marshal(f.ShowWhen)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (f Field) DeriveMapJSON() string {
	if len(f.DeriveMap) == 0 {
		return ""
	}
	raw, err := json.Marshal(f.DeriveMap)
	if err != nil {
		return ""
	}
	return string(raw)
}

// derivedDefault is the value a blank field receives on save. It is shown as
// the field's placeholder first, so nothing is filled in silently.
func derivedDefault(v map[string]string, name string) string {
	switch name {
	case "MAIL_DOMAIN":
		if address, err := mail.ParseAddress(v["MAIL_FROM"]); err == nil {
			if _, domain, ok := strings.Cut(address.Address, "@"); ok {
				return strings.ToLower(domain)
			}
		}
	case "THURA_POSTFIX_HOSTNAME":
		if u, err := url.Parse(v["APP_URL"]); err == nil && u.Hostname() != "" {
			return "mail." + strings.ToLower(u.Hostname())
		}
	case "LLM_BASE_URL":
		if v["LLM_PROVIDER"] == "ollama" {
			return managedModelURL
		}
	}
	return ""
}

// applyDerivedDefaults fills blank visible fields with their derived value.
func applyDerivedDefaults(v map[string]string) bool {
	changed := false
	for _, name := range []string{"MAIL_DOMAIN", "THURA_POSTFIX_HOSTNAME", "LLM_BASE_URL"} {
		if v[name] != "" || !visible(v, name) {
			continue
		}
		if value := derivedDefault(v, name); value != "" {
			v[name] = value
			changed = true
		}
	}
	return changed
}

// Checkbox forms send "true" (or a bare "on") when checked and nothing when
// unchecked; API clients may still post an explicit "true" or "false".
func checkboxValue(present bool, value string) string {
	switch {
	case !present:
		return "false"
	case value == "on" || value == "true":
		return "true"
	default:
		return strings.TrimSpace(value)
	}
}

// Suggestions are served to the administrator from the saved draft's local
// model service; the browser never contacts Ollama and a failure yields none.
func modelsJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	var models []string
	if row, err := q.New(db.From(r.Context())).GetServerSetup(r.Context()); err == nil {
		// A hosted provider never asks a model service for its inventory; the
		// page asks for Ollama's while the administrator is selecting it.
		if v, err := unseal(row.Draft); err == nil && (v["LLM_PROVIDER"] == "ollama" || r.URL.Query().Get("provider") == "ollama") {
			models = localModels(r.Context(), v)
		}
	}
	if models == nil {
		models = []string{}
	}
	if err := json.NewEncoder(w).Encode(map[string][]string{"models": models}); err != nil {
		slog.WarnContext(r.Context(), "serve model suggestions", "error", err)
	}
}

func localModels(ctx context.Context, v map[string]string) []string {
	base := managedModelURL
	if v["LLM_PROVIDER"] == "ollama" && v["LLM_BASE_URL"] != "" {
		base = strings.TrimRight(v["LLM_BASE_URL"], "/")
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/tags", nil)
	if err != nil {
		return nil
	}
	res, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&tags) != nil {
		return nil
	}
	var names []string
	for _, model := range tags.Models {
		if model.Name != "" && len(model.Name) <= 120 && len(names) < 100 {
			names = append(names, model.Name)
		}
	}
	return names
}
