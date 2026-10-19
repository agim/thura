package assistant

import (
	"github.com/agim/lidza/packs/llm"
	"testing"
)

func TestResponseKeyIncludesEffectiveProviderSettingsAndPrompt(t *testing.T) {
	config := llm.Config{Provider: "openai", Model: "new-model", APIKey: "synthetic-fixture-key", MaxTokens: 1024}
	request := llm.Request{System: "Summarize safely", Messages: []llm.Message{{Role: llm.User, Content: "File text"}}, Label: "thura.drive.summarize"}
	base, err := responseKey(config, "openai", "new-model", request, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*llm.Config){func(c *llm.Config) { c.Model = "next-model" }, func(c *llm.Config) { c.Provider = "ollama" }, func(c *llm.Config) { c.BaseURL = "https://other.example" }, func(c *llm.Config) { c.MaxTokens = 2048 }, func(c *llm.Config) { c.APIKey = "rotated-fixture-key" }} {
		next := config
		change(&next)
		key, err := responseKey(next, "openai", "new-model", request, "")
		if err != nil || key == base {
			t.Fatal("changed model configuration reused cache key")
		}
	}
	next := request
	next.System = "A revised summarization prompt"
	if key, err := responseKey(config, "openai", "new-model", next, ""); err != nil || key == base {
		t.Fatal("changed system prompt reused cache key")
	}
	if key, err := responseKey(config, "openai", "resolved-next-model", request, ""); err != nil || key == base {
		t.Fatal("changed effective model reused cache key")
	}
	if key, err := responseKey(config, "openai", "new-model", request, ""); err != nil || key != base {
		t.Fatal("same request was not deterministic")
	}
}
