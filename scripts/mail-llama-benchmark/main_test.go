package main

import "testing"

func TestOutputRejectsAmbiguityAndMalformedResponses(t *testing.T) {
	for _, text := range []string{`{"label":"unknown"}`, `{"label":"safe"}`, `{"label":"legitimate","extra":"ignore"}`, `{"label":"spam"} {"label":"legitimate"}`, `{"label":`} {
		if _, err := parse(text); err == nil {
			t.Fatalf("accepted unsupported output: %s", text)
		}
	}
	for _, label := range []string{"legitimate", "spam", "phishing"} {
		got, err := parse(`{"label":"` + label + `"}`)
		if err != nil || got != label {
			t.Fatalf("valid %s: %s %v", label, got, err)
		}
	}
}
