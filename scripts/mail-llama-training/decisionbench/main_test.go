package main

import (
	"encoding/json"
	"testing"
)

func TestDecisionCodesMatchFrozenPrompt(t *testing.T) {
	var contract struct{ Codes map[string]string }
	if err := json.Unmarshal([]byte(prompt), &contract); err != nil {
		t.Fatal(err)
	}
	for label, code := range contract.Codes {
		actual, err := parse(code)
		if err != nil || actual != label {
			t.Fatalf("codebook mismatch: %q %q %v", code, actual, err)
		}
	}
	if len(contract.Codes) != 3 {
		t.Fatal("expected exactly three reviewed classes")
	}
	for _, invalid := range []string{"", "D", "safe", "AB", "A because it is safe", `{"label":"A"}`, "C A"} {
		if _, err := parse(invalid); err == nil {
			t.Fatalf("accepted ambiguous output %q", invalid)
		}
	}
}
