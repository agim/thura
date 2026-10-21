package tests

import (
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSetupAutomaticChecksReuseRecordedAttempts(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	// Independent tabs have different IDs. Both must reuse the existing result.
	for _, kind := range []string{"mail", "storage", "matrix", "collabora", "livekit", "ai", "smip"} {
		var before int
		if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind=$1", kind).Scan(&before); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			response := setupForm(t, s, "auto-check", url.Values{"revision": {strconv.Itoa(revision)}, "kind": {kind}, "check_id": {uuid.NewString()}})
			response.Body.Close()
			if strings.Contains(response.Header.Get("Location"), "error=") {
				t.Fatal(response.Header.Get("Location"))
			}
		}
		var after int
		if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind=$1", kind).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s auto checks replayed: %d -> %d", kind, before, after)
		}
	}
	// A failed or interrupted current attempt must not be restarted on reload.
	for _, state := range []string{"failed", "started"} {
		if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET state=$1 WHERE kind='mail'", state); err != nil {
			t.Fatal(err)
		}
		response := setupForm(t, s, "auto-check", url.Values{"revision": {strconv.Itoa(revision)}, "kind": {"mail"}, "check_id": {uuid.NewString()}})
		response.Body.Close()
		if strings.Contains(response.Header.Get("Location"), "error=") {
			t.Fatal(response.Header.Get("Location"))
		}
		var count int
		if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind='mail'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s email was resent automatically", state)
		}
	}
	// Uncertain outcomes still cannot trigger another send after 24 hours.
	if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET created_at=$1 WHERE kind='mail'", lidza.Now(s.Context()).Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	response := setupForm(t, s, "auto-check", url.Values{"revision": {strconv.Itoa(revision)}, "kind": {"mail"}, "check_id": {uuid.NewString()}})
	response.Body.Close()
	var mailCount int
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind='mail'").Scan(&mailCount); err != nil {
		t.Fatal(err)
	}
	if mailCount != 1 {
		t.Fatal("old uncertain email was retried automatically")
	}
	// Old results must not suppress a fresh check indefinitely.
	if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET created_at=$1 WHERE kind='storage'", lidza.Now(s.Context()).Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	response = setupForm(t, s, "auto-check", url.Values{"revision": {strconv.Itoa(revision)}, "kind": {"storage"}, "check_id": {uuid.NewString()}})
	response.Body.Close()
	if strings.Contains(response.Header.Get("Location"), "error=") {
		t.Fatal(response.Header.Get("Location"))
	}
	var count int
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind='storage'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expired check was not renewed: %d %s", response.StatusCode, response.Header.Get("Location"))
	}
}

func TestSetupServerUsesSelectionsAndPreservesExistingPlans(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	saveStep(t, s, "server", map[string]string{"THURA_TIMEZONE": "US/Eastern", "THURA_BACKUP_PLAN": "Existing custom offsite backups"})
	response, err := s.Client().Get(s.URL + "/admin/setup?step=server")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if strings.Contains(response.Header.Get("Location"), "error=") {
		t.Fatal(response.Header.Get("Location"))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("server page %d: %v", response.StatusCode, err)
	}
	for _, text := range []string{"Search timezones", "America/New_York", "US/Eastern", "Daily backups · retain 30 days", "Existing custom offsite backups", "does not schedule backups"} {
		if !strings.Contains(string(body), text) {
			t.Fatalf("missing selection or preserved value %q", text)
		}
	}
	if strings.Contains(string(body), `type="text" id="THURA_TIMEZONE"`) || strings.Contains(string(body), `textarea class="form-control" id="THURA_BACKUP_PLAN"`) {
		t.Fatal("avoidable free-text setup inputs remain")
	}
}
