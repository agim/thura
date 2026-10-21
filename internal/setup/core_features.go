package setup

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agim/lidza/packs/llm"
)

// Publication requires the full product. Older published snapshots are still
// accepted by Activate so an upgrade never locks out an existing installation.
func validateCoreFeatures(v map[string]string) error {
	var missing []string
	for _, feature := range []struct{ name, key string }{
		{"Chat (Matrix)", "MATRIX_SERVER_URL"},
		{"Document editing (Collabora)", "COLLABORA_SERVER_URL"},
		{"Meetings (LiveKit)", "LIVEKIT_SERVER_URL"},
	} {
		if strings.TrimSpace(v[feature.key]) == "" {
			missing = append(missing, feature.name)
		}
	}
	if v["SMIP_ENABLED"] != "true" {
		missing = append(missing, "SMIP chat and files")
	}
	if v["SMIP_MAIL_ENABLED"] != "true" {
		missing = append(missing, "SMIP email")
	}
	if v["THURA_AI_ENABLED"] != "true" {
		missing = append(missing, "AI service")
	}
	if len(missing) > 0 {
		return fmt.Errorf("configure all core services before publishing: %s", strings.Join(missing, ", "))
	}
	if len(v["MATRIX_AS_TOKEN"]) < 32 || strings.ContainsAny(v["MATRIX_SERVER_NAME"], "/?#@ \n\r") {
		return errors.New("chat requires a valid Matrix identity and application-service token of at least 32 characters")
	}
	return validateModel(v)
}

func validateModel(v map[string]string) error {
	provider := v["LLM_PROVIDER"]
	if provider != "ollama" && provider != "openai" && provider != "anthropic" && provider != "google" && provider != "compatible" {
		return errors.New("choose an AI provider in the AI service setup step")
	}
	if strings.TrimSpace(v["LLM_MODEL"]) == "" || len(v["LLM_MODEL"]) > 120 {
		return errors.New("enter an AI model name of at most 120 characters")
	}
	base := v["LLM_BASE_URL"]
	if base != "" {
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (provider == "ollama" || provider == "compatible"))) {
			return errors.New("AI requires an HTTPS service URL, or HTTP for a local model service")
		}
	}
	if err := bounded(v, "LLM_MAX_TOKENS", 128, 4096); err != nil {
		return err
	}
	tokens, err := strconv.Atoi(v["LLM_MAX_TOKENS"])
	if err != nil {
		return errors.New("AI output token limit must be an integer")
	}
	if _, err := llm.New(llm.Config{Provider: provider, Model: v["LLM_MODEL"], BaseURL: base, APIKey: v["LLM_API_KEY"], EmbedProvider: "none", MaxTokens: tokens, Timeout: 30 * time.Second, MaxAttempts: 1}); err != nil {
		return errors.New("AI configuration is incomplete; check the model, service URL and provider credentials")
	}
	return nil
}
