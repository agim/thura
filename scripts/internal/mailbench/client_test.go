package mailbench

import (
	"encoding/json"
	"testing"
)

func TestEvidenceExcludesGoldLabelsAndSourceIdentity(t *testing.T) {
	row := input{ID: "withheld-id", Label: "phishing", Source: "withheld-source", Text: "Please bring notes to the meeting."}
	data, err := evidence(row)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["text"] != row.Text {
		t.Fatalf("unexpected model evidence: %s", data)
	}
}
