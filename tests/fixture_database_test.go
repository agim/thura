package tests

import (
	"os"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
)

func TestMain(m *testing.M) {
	// These integration helpers create a database per fixture. A package-wide
	// clone would replace those explicit URLs with one shared database. Use the
	// framework's documented opt-out, retaining the stronger per-fixture scope.
	if err := os.Setenv(lidzatest.EnvSharedDatabase, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestSetupFixturesRetainSeparateDatabases(t *testing.T) {
	first := setupServer(t)
	claimSetup(t, first)
	second := setupServer(t)
	claimSetup(t, second)
	var a, b string
	if err := db.From(first.Context()).QueryRow(first.Context(), "SELECT current_database()").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.From(second.Context()).QueryRow(second.Context(), "SELECT current_database()").Scan(&b); err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("independent setup fixtures share a database")
	}
}
