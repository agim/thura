package tests

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/app"
	"thura/internal/setup"
	"thura/schema"
)

func TestSetupActivationIsLocalToEachNodeAndFailsClosed(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("publication failed")
	}
	var status schema.SetupStatus
	if r := s.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || !status.Published || status.Active || !status.RestartRequired {
		t.Fatal("publication was confused with runtime activation")
	}
	if r := s.JSON(t, "GET", "/readyz", nil, nil); r.StatusCode != 503 {
		t.Fatal("uninitialized node reported ready")
	}
	if r := s.JSON(t, "GET", "/healthz", nil, nil); r.StatusCode != 200 {
		t.Fatal("uninitialized node lost liveness")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 503 {
		t.Fatal("uninitialized node served application traffic")
	}
	other := lidzatest.Start(t, app.New(nil))
	if r := other.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || !status.Active || status.RestartRequired {
		t.Fatal("restarted node did not become active")
	}
	if r := other.JSON(t, "GET", "/readyz", nil, nil); r.StatusCode != 200 {
		t.Fatal("activated node was not ready")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 503 {
		t.Fatal("another node's activation unlocked this node")
	}
	ctx := s.Context()
	_, _, published := setupRow(t, s)
	if _, err := db.From(ctx).Exec(ctx, "UPDATE server_setup SET published='invalid encrypted snapshot'"); err != nil {
		t.Fatal(err)
	}
	if err := setup.Activate(other.Context(), other.Services); err == nil {
		t.Fatal("invalid snapshot activated")
	}
	if r := other.JSON(t, "GET", "/readyz", nil, nil); r.StatusCode != 503 {
		t.Fatal("failed activation reported ready")
	}
	if _, err := db.From(ctx).Exec(ctx, "UPDATE server_setup SET published=$1", published); err != nil {
		t.Fatal(err)
	}
	if err := setup.Activate(other.Context(), other.Services); err != nil {
		t.Fatal(err)
	}
	if r := other.JSON(t, "GET", "/readyz", nil, nil); r.StatusCode != 200 {
		t.Fatal("repaired activation stayed unready")
	}
}

func TestSetupFixtureMarkerCannotInitializeADeployment(t *testing.T) {
	s := setupServer(t)
	ctx := s.Context()
	if _, err := db.From(ctx).Exec(ctx, "UPDATE server_setup SET subject='thura-fixture:externally-configured',published_revision=1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIDZA_MODE", "production")
	if err := setup.Activate(ctx, s.Services); err == nil {
		t.Fatal("test fixture initialized a production deployment without a snapshot")
	}
}
