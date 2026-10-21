package tests

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
)

func TestSetupStorageNamespaceSurvivesDraftReset(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	prefix := func() string {
		t.Helper()
		_, draft, _ := setupRow(t, s)
		plain, err := credentials.Decrypt(key, draft)
		if err != nil {
			t.Fatal(err)
		}
		var values map[string]string
		if err := json.Unmarshal(plain, &values); err != nil {
			t.Fatal(err)
		}
		return values["STORAGE_PREFIX"]
	}
	var installation string
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT id FROM server_setup").Scan(&installation); err != nil {
		t.Fatal(err)
	}
	if prefix() != "thura/"+installation+"/" {
		t.Fatal("fresh installation lacks its database-bound namespace")
	}
	for _, want := range []string{prefix(), "custom-installation/", ""} {
		saveStep(t, s, "storage", map[string]string{"STORAGE_PREFIX": want})
		revision, _, _ := setupRow(t, s)
		r := setupForm(t, s, "restart", url.Values{"revision": {fmt.Sprint(revision)}, "step": {"server"}, "confirm": {"restart"}})
		if r.StatusCode != 303 || strings.Contains(r.Header.Get("Location"), "error=") {
			t.Fatal("draft reset failed")
		}
		if prefix() != want {
			t.Fatal("reset changed the installation storage namespace")
		}
	}
}
