package tests

import (
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	"thura/app"
	"thura/tests/testsupport"
)

func TestWorkspacePackageUsesItsOwnDatabase(t *testing.T) {
	srv := testsupport.StartConfigured(t, app.New(nil))
	var name string
	if err := db.From(srv.Context()).QueryRow(srv.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, "_pkg_tests_workspace_test") {
		t.Fatalf("workspace tests require their own package database, got %q", name)
	}
}
