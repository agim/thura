// Controlled prompt diagnostics; never fed final-test examples or assistant settings.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/agim/lidza/packs/llm"
)

func main() {
	client, err := llm.New(llm.Config{Provider: "compatible", BaseURL: "http://127.0.0.1:8080", Model: "thura-mail-llama1b", EmbedProvider: "none", Timeout: 120 * time.Second, MaxAttempts: 1, MaxTokens: 32})
	if err != nil {
		panic(err)
	}
	var fixed struct{ System string }
	data, err := os.ReadFile("/prompt.json")
	if err != nil || json.Unmarshal(data, &fixed) != nil {
		panic("missing fixed prompt")
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": []string{"legitimate", "spam", "phishing", "unknown"}}}, "required": []string{"label"}, "additionalProperties": false}
	emails := []string{"Hi Alex, our project meeting is Tuesday at 10 AM. No action is needed.", "We offer SEO and web design services. Hire our agency for more backlinks and leads.", "Your bank account closes today. Reply immediately with your password and one-time authentication code. Do not call your bank."}
	short := "Classify email as legitimate (normal mail), spam (unsolicited promotion), or phishing (credential theft, fraud, or impersonation). Return JSON with a label field."
	type output struct {
		Case  int    `json:"case"`
		Mode  string `json:"mode"`
		Text  string `json:"output"`
		Stop  string `json:"stop"`
		Error string `json:"error,omitempty"`
	}
	results := []output{}
	zero := 0.0
	for i, email := range emails {
		payload, err := json.Marshal(struct {
			Text string `json:"text"`
		}{email})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, mode := range []string{"fixed_schema", "short_schema", "short_plain"} {
			system, format, message := short, schema, string(payload)
			if mode == "fixed_schema" {
				system = fixed.System
			}
			if mode == "short_plain" {
				format = nil
				message = email
				system = "Classify the email. Reply with one word: legitimate, spam, or phishing."
			}
			response, callErr := client.Chat(context.Background(), llm.Request{System: system, Messages: []llm.Message{{Role: llm.User, Content: message}}, Schema: format, Temperature: &zero, MaxTokens: 32})
			item := output{Case: i, Mode: mode, Text: response.Text, Stop: response.Stop}
			if callErr != nil {
				item.Error = callErr.Error()
			}
			results = append(results, item)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
