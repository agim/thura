package tests

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type integrationProbeTransport struct{ delegate http.RoundTripper }

func (rt integrationProbeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := ""
	contentType := "application/json"
	switch {
	case r.URL.Host == "matrix.setup.example":
		if r.Header.Get("Authorization") != "Bearer PUBLIC-synthetic-matrix-token-at-least-32" {
			return nil, fmt.Errorf("fixture Matrix authorization mismatch")
		}
		if strings.HasSuffix(r.URL.Path, "/whoami") {
			body = `{"user_id":"@thura_bot:setup.example"}`
		} else {
			body = `{}`
		}
	case r.URL.Host == "office.setup.example":
		contentType = "text/xml"
		body = `<wopi-discovery><net-zone name="external-http"><app name="application/vnd.openxmlformats-officedocument.wordprocessingml.document"><action name="edit" ext="docx" urlsrc="https://office.setup.example/api/v1/collabora/browser/test/cool.html?"/><action name="edit" ext="xlsx" urlsrc="https://office.setup.example/api/v1/collabora/browser/test/cool.html?"/></app></net-zone></wopi-discovery>`
	case r.URL.Host == "meet.setup.example":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			return nil, fmt.Errorf("fixture LiveKit missing authorization")
		}
		body = `{}`
	case (r.URL.Host == "localhost:11434" || r.URL.Host == "127.0.0.1:11434") && r.URL.Path == "/api/chat":
		body = `{"model":"synthetic-local-model","message":{"role":"assistant","content":"OK"},"done":true}`
	case r.URL.Host == "setup.example" && r.URL.Path == "/api/v1/setup/transport":
		raw, err := json.Marshal(map[string]string{"nonce": r.URL.Query().Get("nonce"), "transport": "tls1.3"})
		if err != nil {
			return nil, err
		}
		body = string(raw)
	default:
		return rt.delegate.RoundTrip(r)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r, TLS: &tls.ConnectionState{Version: tls.VersionTLS13}}, nil
}
func installIntegrationProbeFixture(s *lidzatest.Server) {
	rt := lidza.HTTPClient(s.Context()).Transport
	if _, ok := rt.(integrationProbeTransport); ok {
		return
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	lidza.Provide[http.RoundTripper](s.Services, integrationProbeTransport{delegate: rt})
}
func runIntegrationChecks(t *testing.T, s *lidzatest.Server) {
	t.Helper()
	installIntegrationProbeFixture(s)
	revision, _, _ := setupRow(t, s)
	for _, kind := range []string{"matrix", "collabora", "livekit", "ai", "smip"} {
		r := setupForm(t, s, "check-integration", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {uuid.NewString()}, "kind": {kind}, "confirm": {"check"}})
		if strings.Contains(r.Header.Get("Location"), "error=") {
			t.Fatalf("%s check refused: %s", kind, r.Header.Get("Location"))
		}
		var state string
		if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT state FROM setup_probe WHERE kind=$1 ORDER BY created_at DESC,id DESC LIMIT 1", kind).Scan(&state); err != nil || state != "succeeded" {
			t.Fatalf("%s live integration check did not succeed: %s %v", kind, state, err)
		}
	}
}

func TestSetupPublicationRequiresEveryLiveIntegrationProbe(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	// The shared fixture verifies every integration. Simulate a missing AI check
	// so this test still proves that publication cannot bypass that requirement.
	if _, err := db.From(s.Context()).Exec(s.Context(), "DELETE FROM setup_probe WHERE kind='ai'"); err != nil {
		t.Fatal(err)
	}
	revision, _, _ := setupRow(t, s)
	form := url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}
	r := setupForm(t, s, "publish", form)
	if !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("unprobed integrations were published")
	}
	runIntegrationChecks(t, s)
	r = setupForm(t, s, "publish", form)
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("authenticated integration checks did not permit publication: " + r.Header.Get("Location"))
	}
}
