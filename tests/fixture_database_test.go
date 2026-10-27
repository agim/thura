package tests

import (
	"context"
	"github.com/agim/lidza/pkg/env"
	"os"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
)

func TestMain(m *testing.M) {
	// This package mixes explicitly isolated setup/SMIP fixtures with ordinary
	// app fixtures. Give the latter a package clone without letting Start
	// replace the explicit per-fixture DATABASE_URL values.
	if err := os.Chdir(".."); err != nil {
		panic(err)
	}
	if err := os.Setenv("LIDZA_MODE", "test"); err != nil {
		panic(err)
	}
	var cfg db.Config
	if err := env.Load(".", &cfg); err != nil {
		panic(err)
	}
	url, err := db.CloneTestDatabase(context.Background(), cfg.URL, "tests")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("DATABASE_URL", url); err != nil {
		panic(err)
	}
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
