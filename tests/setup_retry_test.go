package tests

import (
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/setup"
)

func TestSetupReviewRepeatedFailureGetsNewAttemptAndRetryID(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	// A regular file is never a valid storage root; no cloud operation is used.
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	saveStep(t, s, "storage", map[string]string{"STORAGE_DIR": root})
	revision, _, _ := setupRow(t, s)
	readRetry := func() (string, string) {
		response, err := s.Client().Get(s.URL + "/admin/setup?step=review")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `data-check-state="failed"`) && (!strings.Contains(string(body), "not a directory") || !strings.Contains(string(body), "Check ID:")) {
			t.Fatal("failed check lost its diagnostic or check ID on refresh")
		}
		matches := regexp.MustCompile(`(?s)data-check-row="storage" data-check-state="([^"]+)" data-check-attempt="([^"]*)".*?name="check_id" value="([^"]+)"`).FindSubmatch(body)
		if len(matches) != 4 {
			t.Fatal("storage retry intent or recorded attempt missing")
		}
		return string(matches[2]), string(matches[3])
	}
	firstAttempt, firstID := readRetry()
	if firstAttempt != "" {
		t.Fatal("new connection had a current attempt before checking")
	}
	for index := 0; index < 2; index++ {
		_, id := readRetry()
		response := setupForm(t, s, "check-storage", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {id}, "confirm": {"check"}, "back": {"?step=review"}})
		if response.StatusCode != 303 {
			t.Fatalf("retry status %d", response.StatusCode)
		}
		attempt, nextID := readRetry()
		if attempt != id || nextID == id || nextID == firstID {
			t.Fatal("failed retry did not refresh its recorded attempt and next intent")
		}
		// Replaying a consumed form only reads the same result.
		setupForm(t, s, "check-storage", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {id}, "confirm": {"check"}})
	}
	var failures int
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE kind='storage' AND state='failed'").Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 2 {
		t.Fatalf("expected two genuine failed attempts, got %d", failures)
	}
}

func TestSetupEmailAndAIChecksKeepTheirSmallerBudget(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	for _, kind := range []string{"mail", "ai"} {
		// completeSetup already performed one synthetic check of each kind.
		for range 2 {
			if _, err := setup.Probe(probeContext(s), kind, strconv.Itoa(revision), uuid.NewString()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := setup.Probe(probeContext(s), kind, strconv.Itoa(revision), uuid.NewString()); err == nil || !strings.Contains(err.Error(), "limited to 3") {
			t.Fatalf("%s side-effect budget was not enforced: %v", kind, err)
		}
	}
}
