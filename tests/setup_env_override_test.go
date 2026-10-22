package tests

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Compose always defines LIVEKIT_PUBLIC_URL; an empty process value is not an
// override of what the wizard holds, while a different non-empty value still is.
func TestSetupPublishIgnoresEmptyEnvironmentOverride(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	// After the fixtures: the server fixture clears this variable itself.
	t.Setenv("LIVEKIT_PUBLIC_URL", "")
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("an empty environment value blocked publication", r.Header.Get("Location"))
	}
}

func TestSetupPublishRefusesConflictingEnvironmentOverride(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	t.Setenv("LIVEKIT_PUBLIC_URL", "wss://elsewhere.setup.example")
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("a conflicting environment override was published", r.Header.Get("Location"), r.StatusCode)
	}
	_, _, published := setupRow(t, s)
	if published != "" {
		t.Fatal("the refused publication changed active configuration")
	}
}
