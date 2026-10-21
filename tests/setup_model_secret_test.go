package tests

import (
	"encoding/json"
	"testing"

	"github.com/agim/lidza/pkg/credentials"
)

func TestSetupModelProviderChangeDoesNotReuseOtherProviderCredential(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	saveStep(t, s, "ai", map[string]string{"THURA_AI_ENABLED": "true", "LLM_PROVIDER": "openai", "LLM_MODEL": "model", "LLM_API_KEY": "PUBLIC-synthetic-openai-credential"})
	saveStep(t, s, "ai", map[string]string{"THURA_AI_ENABLED": "true", "LLM_PROVIDER": "anthropic", "LLM_MODEL": "model"})
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	_, draft, _ := setupRow(t, s)
	plain, err := credentials.Decrypt(key, draft)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(plain, &values); err != nil {
		t.Fatal(err)
	}
	if values["LLM_API_KEY"] != "" {
		t.Fatal("changing the model vendor reused another vendor's credential")
	}
}
