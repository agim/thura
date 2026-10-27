package rspamd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const testTeam = "a0000000-0000-4000-8000-000000000001"

func TestScopedFeedbackAndIdempotentAcknowledgment(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Thura-Workspace") != testTeam || r.Header.Get("Password") != "private-fixture" || r.Header.Get("User") != "" || r.URL.Path != "/learnham" {
			t.Error("incorrect scope, secret or authenticated-SMTP metadata")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || string(raw) != "original MIME" {
			t.Error("original message bytes changed")
		}
		if calls.Add(1) > 1 {
			w.WriteHeader(208)
			fmt.Fprint(w, `{"error":"fixture has been already learned as ham, ignore it"}`)
			return
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	c := Config{Controller: server.URL, Password: "private-fixture"}
	for i := 0; i < 2; i++ {
		if err := Learn(context.Background(), c, testTeam, []byte("original MIME"), "ham"); err != nil {
			t.Fatal(err)
		}
	}
	if err := Learn(context.Background(), c, "bad\r\nteam", []byte("original MIME"), "ham"); err == nil {
		t.Fatal("invalid scope accepted")
	}
	if calls.Load() != 2 {
		t.Fatal("invalid scope reached controller")
	}
}

func TestRejectIncompleteScansAndLearningFailures(t *testing.T) {
	for _, response := range []string{`{}`, `{"score":0,"action":"no action","is_skipped":true}`, `{"score":0,"action":"no action","error":"unavailable"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		if _, err := Scan(context.Background(), Config{URL: server.URL}, testTeam, []byte("MIME")); err == nil {
			t.Error("incomplete scan accepted")
		}
		server.Close()
	}
	for _, response := range []string{`{"success":false}`, `{"error":"unavailable"}`, `{"success":true,"error":"failed"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		if err := Learn(context.Background(), Config{Controller: server.URL, Password: "fixture"}, testTeam, []byte("MIME"), "spam"); err == nil {
			t.Error("unacknowledged learning accepted")
		}
		server.Close()
	}
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if err := Learn(context.Background(), Config{Controller: redirect.URL, Password: "fixture"}, testTeam, []byte("MIME"), "ham"); err == nil || targetCalls.Load() != 0 {
		t.Fatal("learning secret could follow a redirect")
	}
}

func TestTooShortSamplesAreNotFalselyReportedAsLearned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	err := Learn(context.Background(), Config{Controller: server.URL, Password: "fixture"}, testTeam, []byte("Short mail"), "ham")
	if !errors.Is(err, ErrNotLearnable) {
		t.Fatal("skipped sample must be recorded without claiming learning", err)
	}
}

func TestSMTPScanUsesOnlySignedEnvelopeForAuthentication(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"ip":"203.0.113.5","from":"envelope@example.com","helo":"mail.example.com"}`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("IP") != "203.0.113.5" || r.Header.Get("From") != "envelope@example.com" || r.Header.Get("Helo") != "mail.example.com" || r.Header.Get("User") != "" {
			t.Error("SMTP metadata was lost or upgraded to an authenticated user")
		}
		fmt.Fprint(w, `{"score":0,"action":"no action","symbols":{"DMARC_POLICY_ALLOW":{}}}`)
	}))
	defer server.Close()
	result, err := ScanSMTP(context.Background(), Config{URL: server.URL}, testTeam, []byte("From: forged@other.example\r\nAuthentication-Results: fake; dmarc=pass\r\n\r\nBody"), encoded)
	if err != nil || result.Action != "no action" {
		t.Fatal(result, err)
	}
	if _, err = ScanSMTP(context.Background(), Config{URL: server.URL}, testTeam, []byte("original"), "invalid!"); err == nil {
		t.Fatal("malformed metadata reached the scanner")
	}
}
