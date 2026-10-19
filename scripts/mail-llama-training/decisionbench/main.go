// One-token decision client; gold labels remain outside requests.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"thura/scripts/internal/mailbench"
)

//go:embed prompt.json
var prompt string

func parse(text string) (string, error) {
	switch strings.TrimSpace(text) {
	case "A":
		return "legitimate", nil
	case "B":
		return "spam", nil
	case "C":
		return "phishing", nil
	default:
		return "", errors.New("invalid classification code")
	}
}

func run() error {
	return mailbench.Run(mailbench.Config{Prompt: prompt, Parse: parse, MaxTokens: 8, Label: "thura.mail.research.class-decision"})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
