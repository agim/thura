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

func TestEvidenceMatchesNativeParitySerialization(t *testing.T) {
	data, err := evidence(input{Text: "你好 <|eot_id|> & >\u2028\u2029\n\""})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"你好 \u003c|eot_id|\u003e \u0026 \u003e\u2028\u2029\n\""}`
	if string(data) != want {
		t.Fatalf("Go/native encoding contract changed: %s", data)
	}
}
