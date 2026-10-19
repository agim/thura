// Private research client; never uses assistant settings.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"thura/scripts/internal/mailbench"
)

//go:embed prompt.json
var prompt string

func parse(text string) (string, error) {
	var out struct {
		Label string `json:"label"`
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return "", errors.New("invalid model JSON")
	}
	switch out.Label {
	case "legitimate", "spam", "phishing":
		return out.Label, nil
	case "unknown":
		return "", errors.New("insufficient model evidence")
	}
	return "", errors.New("invalid model label")
}

func run() error {
	return mailbench.Run(mailbench.Config{Prompt: prompt, Parse: parse, MaxTokens: 32, Schema: map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string", "enum": []string{"legitimate", "spam", "phishing", "unknown"}}}, "required": []string{"label"}, "additionalProperties": false}, Label: "thura.mail.research.llama1b"})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
