package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"thura/internal/providers/livekit"
)

// fakeLiveKit answers the room API and, behind a /rtc prefix, the
// signalling validate endpoint the way LiveKit does without a token.
func fakeLiveKit(t *testing.T, signal http.HandlerFunc) (api, public *httptest.Server) {
	t.Helper()
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(api.Close)
	public = httptest.NewServer(signal)
	t.Cleanup(public.Close)
	return api, public
}

func liveKitDraft(api, public *httptest.Server) map[string]string {
	u, _ := url.Parse(public.URL)
	return map[string]string{"LIVEKIT_SERVER_URL": api.URL, "LIVEKIT_API_KEY": "test-key", "LIVEKIT_API_SECRET": strings.Repeat("s", 32), "LIVEKIT_PUBLIC_URL": "ws://" + u.Host + "/rtc", "APP_URL": "http://" + u.Host}
}

func checkStates(checks []MeetCheck) map[string]MeetCheck {
	out := map[string]MeetCheck{}
	for _, c := range checks {
		out[c.ID] = c
	}
	return out
}

func TestMeetingsProbePassesWhenSignallingIsForwarded(t *testing.T) {
	api, public := fakeLiveKit(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rtc/rtc/validate" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "no permissions to access the room", http.StatusUnauthorized)
	})
	v := liveKitDraft(api, public)
	if err := checkIntegration(context.Background(), "livekit", v, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	checks := checkStates(meetChecks(context.Background(), v, listRooms))
	for _, id := range []string{"settings", "api", "signal"} {
		if checks[id].State != "passed" {
			t.Fatalf("%s: %+v", id, checks[id])
		}
	}
	if checks["media"].State != "manual" || !strings.Contains(checks["media"].Fix, "UDP 7882") || !strings.Contains(checks["media"].Fix, "THURA_RTC_NODE_IP") {
		t.Fatalf("media ports reported as verified: %+v", checks["media"])
	}
}

func TestMeetingsProbeNamesAMissingProxyRoute(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"404": http.NotFound,
		"app page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><div id=root></div>"))
		},
	} {
		api, public := fakeLiveKit(t, handler)
		v := liveKitDraft(api, public)
		err := checkIntegration(context.Background(), "livekit", v, uuid.NewString())
		if err == nil {
			t.Fatalf("%s: unforwarded signalling accepted", name)
		}
		diagnostic := probeDiagnostic(err)
		if !strings.Contains(diagnostic, "Public signalling reachable through the proxy: /rtc is not forwarded to LiveKit port 7880 with the prefix stripped") || strings.Contains(diagnostic, "LiveKit API reachable") {
			t.Fatalf("%s: diagnostic %q", name, diagnostic)
		}
		signal := checkStates(meetChecks(context.Background(), v, listRooms))["signal"]
		if signal.State != "failed" || !strings.Contains(signal.Fix, "proxy rule") {
			t.Fatalf("%s: %+v", name, signal)
		}
	}
}

func TestMeetingsProbeReportsASignallingTimeout(t *testing.T) {
	release := make(chan struct{})
	api, public := fakeLiveKit(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	previous := livekit.SignalTimeout
	livekit.SignalTimeout = 200 * time.Millisecond
	t.Cleanup(func() { livekit.SignalTimeout = previous })
	err := checkIntegration(context.Background(), "livekit", liveKitDraft(api, public), uuid.NewString())
	if err == nil || !strings.Contains(probeDiagnostic(err), livekit.ErrSignalTimeout.Error()) {
		t.Fatalf("timeout not reported: %v", err)
	}
}

func TestMeetingsProbeRefusesPrivateSignallingHosts(t *testing.T) {
	signalled := false
	api, public := fakeLiveKit(t, func(w http.ResponseWriter, _ *http.Request) { signalled = true; w.WriteHeader(http.StatusUnauthorized) })
	v := liveKitDraft(api, public)
	// The loopback fixture is not this app's own domain here.
	v["APP_URL"] = "https://thura.example.com"
	for _, public := range []string{v["LIVEKIT_PUBLIC_URL"], "wss://10.1.2.3/rtc", "wss://169.254.169.254/rtc"} {
		v["LIVEKIT_PUBLIC_URL"] = public
		err := checkIntegration(context.Background(), "livekit", v, uuid.NewString())
		if err == nil || !strings.Contains(probeDiagnostic(err), livekit.ErrSignalRefusedHost.Error()) {
			t.Fatalf("%s: %v", public, err)
		}
	}
	if signalled {
		t.Fatal("a refused host was contacted")
	}
}

func TestMeetingsProbeReportsEachSettingByName(t *testing.T) {
	checks := checkStates(meetChecks(context.Background(), map[string]string{"LIVEKIT_SERVER_URL": "http://127.0.0.1:7880", "LIVEKIT_API_KEY": "k"}, listRooms))
	if checks["settings"].State != "failed" || !strings.Contains(checks["settings"].Detail, "LIVEKIT_API_SECRET") || checks["api"].State != "skipped" {
		t.Fatalf("%+v", checks)
	}
	checks = checkStates(meetChecks(context.Background(), map[string]string{"LIVEKIT_API_KEY": "k", "LIVEKIT_API_SECRET": strings.Repeat("s", 32), "APP_URL": "https://thura.example.com"}, listRooms))
	if !strings.Contains(checks["settings"].Detail, "LIVEKIT_SERVER_URL is required") || !strings.Contains(checks["settings"].Fix, "http://127.0.0.1:7880") {
		t.Fatalf("%+v", checks["settings"])
	}
}

func TestMeetingsProxySnippetsUseTheDomain(t *testing.T) {
	view := meetView(map[string]string{"APP_URL": "https://thura.example.com"}, "elsewhere.example:8443")
	if view.PublicURL != "wss://thura.example.com/rtc" || !strings.Contains(view.Nginx, "location /rtc/ {") || !strings.Contains(view.Nginx, "proxy_pass http://127.0.0.1:7880/;") || !strings.Contains(view.Nginx, "Upgrade $http_upgrade") || !strings.Contains(view.Nginx, "thura.example.com") {
		t.Fatalf("nginx %q", view.Nginx)
	}
	if !strings.HasPrefix(view.Caddy, "thura.example.com {") || !strings.Contains(view.Caddy, "handle_path /rtc/* {") {
		t.Fatalf("caddy %q", view.Caddy)
	}
	if view.FromPage {
		t.Fatal("saved address ignored")
	}
	if view := meetView(map[string]string{}, "setup.example.org:8443"); view.Domain != "setup.example.org" || !view.FromPage || !strings.Contains(view.Caddy, "setup.example.org {") {
		t.Fatalf("page address not offered: %+v", view)
	}
	if view := meetView(map[string]string{}, "bad host/"); view.Domain != "" || view.Nginx != "" {
		t.Fatal("snippets used an invalid host")
	}
}
