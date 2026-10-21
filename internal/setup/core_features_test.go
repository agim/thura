package setup

import (
	"strings"
	"testing"
)

func coreValues() map[string]string {
	v := validSetup()
	v["MATRIX_SERVER_URL"] = "https://matrix.team.example"
	v["MATRIX_SERVER_NAME"] = "team.example"
	v["MATRIX_AS_TOKEN"] = strings.Repeat("m", 32)
	v["COLLABORA_SERVER_URL"] = "https://office.team.example"
	v["LIVEKIT_SERVER_URL"] = "https://meet.team.example"
	v["SMIP_ENABLED"], v["SMIP_MAIL_ENABLED"], v["THURA_AI_ENABLED"] = "true", "true", "true"
	v["LLM_PROVIDER"], v["LLM_MODEL"] = "ollama", "local-model"
	return v
}

func TestPublicationRequiresEveryCoreService(t *testing.T) {
	v := coreValues()
	if err := validateCoreFeatures(v); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"MATRIX_SERVER_URL", "COLLABORA_SERVER_URL", "LIVEKIT_SERVER_URL", "SMIP_ENABLED", "SMIP_MAIL_ENABLED", "THURA_AI_ENABLED", "LLM_MODEL", "MATRIX_AS_TOKEN"} {
		t.Run(field, func(t *testing.T) {
			v := coreValues()
			v[field] = ""
			if validateCoreFeatures(v) == nil {
				t.Fatal("incomplete core service accepted")
			}
		})
	}
	// Older published snapshots still boot with their original configuration;
	// completing all core services is required when next publishing setup.
	if err := validate(validSetup()); err != nil {
		t.Fatal("legacy snapshot activation was invalidated", err)
	}
}

func TestWizardModelProviderCredentialsAndLimits(t *testing.T) {
	for _, provider := range []string{"ollama", "openai", "anthropic", "google", "compatible"} {
		v := coreValues()
		v["LLM_PROVIDER"], v["LLM_API_KEY"] = provider, "synthetic-provider-key"
		if provider == "compatible" {
			v["LLM_BASE_URL"] = "https://models.team.example/v1"
		}
		if err := validateCoreFeatures(v); err != nil {
			t.Fatal(provider, err)
		}
		if provider != "ollama" && provider != "compatible" {
			v["LLM_API_KEY"] = ""
			if validateCoreFeatures(v) == nil {
				t.Fatal("missing hosted provider credential accepted", provider)
			}
		}
	}
	for _, pair := range [][2]string{{"LLM_PROVIDER", "fake"}, {"LLM_MAX_TOKENS", "0"}, {"LLM_MAX_TOKENS", "4097"}, {"LLM_BASE_URL", "https://user:secret@models.example"}} {
		v := coreValues()
		v[pair[0]] = pair[1]
		if validateCoreFeatures(v) == nil {
			t.Fatal("invalid AI configuration accepted")
		}
	}
}
