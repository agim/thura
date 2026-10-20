package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/llm"
	q "thura/db/queries/gen"
	"thura/internal/mailabuse"
	"thura/schema"
)

func learnFeedback(t *testing.T, n *mailNode, item schema.MailItem) {
	t.Helper()
	raw, err := json.Marshal(schema.MailScreenJob{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailabuse.LearnFeedback(n.server.Context(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestJunkNotSpamRestoresAndLearnsWithRetryAndReversal(t *testing.T) {
	n := abuseNode(t, "junk-feedback.example")
	var calls atomic.Int32
	var fail atomic.Bool
	var last atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Thura-Workspace") != n.workspace.ID || r.Header.Get("Password") != "controller-fixture" {
			t.Error("feedback escaped its authorized team")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) == 0 {
			t.Error("missing original MIME")
		}
		calls.Add(1)
		last.Store(r.URL.Path)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	t.Setenv("MAIL_RSPAMD_CONTROLLER_URL", server.URL)
	t.Setenv("MAIL_RSPAMD_PASSWORD", "controller-fixture")
	item := receiveAbuse(t, n, "correction", "colleague@outside.example", "Please review the delivery document with our weekly planning agenda.", "", 200)
	reviewAbuse(t, n, item, "not-spam", 200)
	got := n.detail(t, item)
	if got.Item.Folder != schema.MailFolderInbox || !got.Screening.Released || got.Screening.FeedbackState != "pending" || calls.Load() != 0 {
		t.Fatal("Not spam must restore immediately and queue learning", got.Screening)
	}
	fail.Store(true)
	learnFeedback(t, n, item)
	if got = n.detail(t, item); got.Screening.FeedbackState != "error" || got.Item.Folder != schema.MailFolderInbox {
		t.Fatal("filter outage lost correction or restored mail")
	}
	fail.Store(false)
	n.server.Clock.Advance(2 * time.Minute)
	learnFeedback(t, n, item)
	if n.detail(t, item).Screening.FeedbackState != "learned" || last.Load() != "/learnham" {
		t.Fatal("ham feedback was not learned")
	}
	before := calls.Load()
	reviewAbuse(t, n, item, "not-spam", 200)
	learnFeedback(t, n, item)
	if calls.Load() != before {
		t.Fatal("repeated Not spam learned the message twice")
	}
	reviewAbuse(t, n, item, "report-spam", 200)
	learnFeedback(t, n, item)
	if got = n.detail(t, item); got.Item.Folder != schema.MailFolderSpam || got.Screening.FeedbackState != "learned" || last.Load() != "/learnspam" {
		t.Fatal("spam reversal failed")
	}
	reviewAbuse(t, n, item, "mark-safe", 200)
	learnFeedback(t, n, item)
	if n.detail(t, item).Item.Folder != schema.MailFolderInbox || last.Load() != "/learnham" {
		t.Fatal("Mark safe failed to reverse the classification")
	}
	queries := q.New(db.From(n.server.Context()))
	policy, err := queries.GetMailSenderPolicy(n.server.Context(), q.GetMailSenderPolicyParams{WorkspaceID: n.workspace.ID, Identity: "unverified:colleague@outside.example"})
	if err == nil && policy.Decision == "trusted" {
		t.Fatal("single-message correction silently allowlisted sender")
	}
}

func TestRspamdRunsFirstAndLocalQuarantineSkipsAI(t *testing.T) {
	n := abuseNode(t, "local-first.example")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/checkv2" || r.Header.Get("X-Thura-Workspace") != n.workspace.ID || r.Header.Get("User") != "" {
			t.Error("incorrect local scan request")
		}
		fmt.Fprint(w, `{"score":8.1,"action":"add header","symbols":{}}`)
	}))
	defer server.Close()
	t.Setenv("MAIL_RSPAMD_URL", server.URL)
	item := receiveAbuse(t, n, "local-held", "advertiser@outside.example", "Unsolicited bulk offer with multiple promotional paragraphs.", "", 200)
	raw, err := json.Marshal(schema.MailScreenJob{ID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailabuse.Screen(n.server.Context(), raw); err != nil {
		t.Fatal(err)
	}
	got := n.detail(t, item)
	if got.Screening.State != "quarantined" || got.Screening.AiState != "skipped_local" || len(llm.From(n.server.Context()).Fake().Calls()) != 0 {
		t.Fatal("local Junk was unnecessarily sent to AI")
	}
	queries := q.New(db.From(n.server.Context()))
	if _, err = db.From(n.server.Context()).Exec(n.server.Context(), "update mail_screening set av_state='infected' where item_id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	reviewAbuse(t, n, item, "not-spam", 409)
	if _, err = queries.GetMailFilterFeedback(n.server.Context(), item.ID); err == nil {
		t.Fatal("known malware trained a safe verdict")
	}
}

func TestShortMailCorrectionIsSavedWithoutEndlessLearningRetries(t *testing.T) {
	n := abuseNode(t, "short-feedback.example")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	t.Setenv("MAIL_RSPAMD_CONTROLLER_URL", server.URL)
	t.Setenv("MAIL_RSPAMD_PASSWORD", "controller-fixture")
	item := receiveAbuse(t, n, "short-correction", "peer@outside.example", "Thanks!", "", 200)
	reviewAbuse(t, n, item, "not-spam", 200)
	learnFeedback(t, n, item)
	if got := n.detail(t, item); got.Screening.FeedbackState != "recorded" || got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "manual_review" {
		t.Fatal("skipped sample lost the human correction or falsely claimed learning")
	}
	reviewAbuse(t, n, item, "not-spam", 200)
	learnFeedback(t, n, item)
	if calls.Load() != 1 {
		t.Fatal("too-short mail retried a completed correction forever")
	}
}

func TestRevokedMemberFeedbackCannotTrainTheTeamFilter(t *testing.T) {
	n := abuseNode(t, "revoked-feedback.example")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"success":true}`) }))
	defer server.Close()
	t.Setenv("MAIL_RSPAMD_CONTROLLER_URL", server.URL)
	t.Setenv("MAIL_RSPAMD_PASSWORD", "controller-fixture")
	item := receiveAbuse(t, n, "revoked-correction", "peer@outside.example", "An ordinary email with an agenda for our weekly meeting.", "", 200)
	reviewAbuse(t, n, item, "report-spam", 200)
	ctx := n.server.Context()
	// Direct fixture revocation isolates the background worker from API role rules.
	if _, err := db.From(ctx).Exec(ctx, "delete from auth_member where scope=$1 and subject=$2", n.workspace.ID, n.owner.Subject); err != nil {
		t.Fatal(err)
	}
	learnFeedback(t, n, item)
	row, err := q.New(db.From(ctx)).GetMailFilterFeedback(ctx, item.ID)
	if err != nil || row.State != "cancelled" || calls.Load() != 0 {
		t.Fatal("revoked actor trained the team filter", err)
	}
}

func TestUncertainAIReviewStaysInJunkAtAnyRiskThreshold(t *testing.T) {
	n := abuseNode(t, "uncertain-feedback.example")
	p := abuseSettings(t, n)
	p.SpamThreshold = 100
	saveAbuseSettings(t, n, p)
	item := receiveAbuse(t, n, "uncertain-review", "peer@outside.example", "An ordinary planning update with insufficient context for a confident review.", "", 200)
	screenAbuse(t, n, item, 0, "unknown")
	got := n.detail(t, item)
	if got.Item.Folder != schema.MailFolderSpam || got.Screening.State != "error" || got.Screening.AiState != "uncertain" {
		t.Fatal("uncertain model output was silently accepted as safe")
	}
	reviewAbuse(t, n, item, "not-spam", 200)
	if got = n.detail(t, item); got.Item.Folder != schema.MailFolderInbox || got.Screening.AiState != "manual_review" {
		t.Fatal("human correction could not release an uncertain verdict")
	}
}
