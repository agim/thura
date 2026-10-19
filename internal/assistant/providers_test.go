package assistant

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza/packs/llm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequiredProvidersAcceptConfigurableModelIDs(t *testing.T) {
	for _, provider := range []string{"ollama", "openai", "anthropic", "google"} {
		t.Run(provider, func(t *testing.T) {
			model := "provider-new-model-id"
			client, err := llm.New(llm.Config{Provider: provider, Model: model, APIKey: "PUBLIC-test-fixture-key", EmbedProvider: "none"})
			if err != nil {
				t.Fatal(err)
			}
			if client.Model() != model || client.Provider() != provider {
				t.Fatalf("model/provider hardcoded: %s %s", client.Provider(), client.Model())
			}
		})
	}
}

func TestRequiredProvidersSendConfigurableModelsThroughRealAdapters(t *testing.T) {
	for _, provider := range []string{"ollama", "openai", "anthropic", "google"} {
		t.Run(provider, func(t *testing.T) {
			model := "a-new-model-without-a-library-release"
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if provider != "google" && body["model"] != model {
					t.Errorf("model identifier changed: %v", body["model"])
				}
				if provider == "google" && !strings.Contains(r.URL.Path, model) {
					t.Errorf("model missing from Gemini path: %s", r.URL.Path)
				}
				if provider == "openai" && r.Header.Get("Authorization") != "Bearer PUBLIC-test-fixture-key" {
					t.Error("missing OpenAI authentication")
				}
				if provider == "anthropic" && r.Header.Get("x-api-key") != "PUBLIC-test-fixture-key" {
					t.Error("missing Anthropic authentication")
				}
				if provider == "google" && r.Header.Get("x-goog-api-key") != "PUBLIC-test-fixture-key" {
					t.Error("missing Gemini authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				var response string
				switch provider {
				case "ollama":
					response = `{"model":"a-new-model-without-a-library-release","message":{"role":"assistant","content":"Fixture response"},"done":true,"prompt_eval_count":2,"eval_count":2}`
				case "openai":
					response = `{"model":"a-new-model-without-a-library-release","choices":[{"message":{"role":"assistant","content":"Fixture response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2}}`
				case "anthropic":
					response = `{"model":"a-new-model-without-a-library-release","content":[{"type":"text","text":"Fixture response"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":2}}`
				case "google":
					response = `{"candidates":[{"content":{"parts":[{"text":"Fixture response"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":2}}`
				}
				if _, err := w.Write([]byte(response)); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := llm.New(llm.Config{Provider: provider, Model: model, APIKey: "PUBLIC-test-fixture-key", BaseURL: server.URL, EmbedProvider: "none", MaxAttempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Chat(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.User, Content: "Fixture text"}}, MaxTokens: 128})
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != "Fixture response" || calls != 1 {
				t.Fatalf("wrong response or extra call: %+v, %d", result, calls)
			}
		})
	}
}
