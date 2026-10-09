package tests

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/auth"
	"thura/internal/setup"
	"thura/schema"
)

func TestSetupRoutingRequiresBootstrapThenConfiguration(t *testing.T) {
	s := setupServer(t)
	client := *s.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	redirect := func(path, target string) {
		t.Helper()
		r, err := client.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != target || r.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: got %d location %q cache %q", path, r.StatusCode, r.Header.Get("Location"), r.Header.Get("Cache-Control"))
		}
	}
	for _, path := range []string{"/", "/app", "/about", "/forgot", "/invite", "/admin", "/admin/setup", "/assets/../app"} {
		redirect(path, "/setup")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 503 {
		t.Fatal("fresh installation exposed application API")
	}
	if r := s.JSON(t, "GET", "/api/v1/health", nil, nil); r.StatusCode != 200 {
		t.Fatal("bootstrap blocked health checks")
	}
	// Even a missing deployment token must land on setup, where the operator
	// gets instructions instead of an unconfigured application.
	t.Setenv("THURA_SETUP_TOKEN", "")
	redirect("/", "/setup")
	t.Setenv("THURA_SETUP_TOKEN", "PUBLIC-test-bootstrap-token-with-32-characters")
	claimSetup(t, s)
	for _, path := range []string{"/", "/app", "/setup", "/admin", "/admin/users", "/admin/settings"} {
		redirect(path, "/admin/setup")
	}
	if r, err := client.Get(s.URL + "/admin/setup"); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("wizard got %d", r.StatusCode)
		}
	}
	var status schema.SetupStatus
	if r := s.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || !status.Administrator || status.Published {
		t.Fatal("administrator setup status incorrect")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 503 {
		t.Fatal("administrator bypassed unpublished setup gate")
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/logout", nil, nil); r.StatusCode != 204 {
		t.Fatal("logout failed")
	}
	redirect("/admin/setup", "/setup")
	ordinary, err := auth.From(s.Context()).CreateUser(s.Context(), "ordinary-routing@setup.example", "Ordinary", "ordinary private oak river 88333")
	if err != nil {
		t.Fatal(err)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/login", map[string]string{"email": ordinary.Email, "password": "ordinary private oak river 88333"}, nil); r.StatusCode != 200 {
		t.Fatal("ordinary login failed")
	}
	redirect("/app", "/setup")
	if r := s.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || status.Administrator {
		t.Fatal("ordinary account reported as operator")
	}
	if r, err := client.Get(s.URL + "/admin/setup"); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
		if r.StatusCode != 403 {
			t.Fatalf("ordinary wizard got %d", r.StatusCode)
		}
	}
}

func TestSetupRoutingPublicationAndDraftRestart(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("publication failed")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 503 {
		t.Fatal("publication opened an uninitialized node")
	}
	if err := setup.Activate(s.Context(), s.Services); err != nil {
		t.Fatal(err)
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 200 {
		t.Fatal("published installation remained blocked")
	}
	// Office callbacks use the document provider's bearer token, not a Thura
	// session. The setup gate must leave that verification to the provider.
	request, err := http.NewRequest("POST", s.URL+"/api/v1/office/callback/unknown", strings.NewReader(`{"status":2}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer PUBLIC-provider-token-not-a-Thura-session")
	response, err := s.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var callback schema.OfficeResult
	err = json.NewDecoder(response.Body).Decode(&callback)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || callback.Error != 1 {
		t.Fatal("setup gate intercepted document-provider authentication")
	}
	revision, _, _ = setupRow(t, s)
	r = setupForm(t, s, "restart", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"restart"}, "step": {"server"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("draft restart failed")
	}
	if r := s.JSON(t, "GET", "/api/v1/workspaces", nil, nil); r.StatusCode != 200 {
		t.Fatal("draft restart blocked active installation")
	}
}
