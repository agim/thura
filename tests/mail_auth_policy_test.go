package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agim/lidza/packs/llm"
	"thura/internal/mailabuse"
	"thura/schema"
)

func TestDMARCQuarantineCannotBeOverriddenByLowSpamScoreOrAI(t *testing.T) {
	n := abuseNode(t, "dmarc-hold.example")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"score":0,"action":"add header","symbols":{"DMARC_POLICY_QUARANTINE":{}}}`)
	}))
	defer server.Close()
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	item := receiveAbuse(t, n, "dmarc-quarantine", "spoof@outside.example", "Ordinary project meeting notes without suspicious links.", "", 200)
	payload, _ := json.Marshal(schema.MailScreenJob{ID: item.ID})
	if err := mailabuse.Screen(n.server.Context(), payload); err != nil {
		t.Fatal(err)
	}
	got := n.detail(t, item)
	if got.Screening.State != "quarantined" || got.Screening.AiState != "skipped_local" || len(llm.From(n.server.Context()).Fake().Calls()) != 0 {
		t.Fatal("DMARC quarantine was bypassed or sent to AI", got.Screening)
	}
}
