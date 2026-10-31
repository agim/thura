package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agim/lidza/pkg/credentials"
	livekit "thura/internal/providers/livekit"
)

func policyOf(t *testing.T, h http.Handler) (csp, permissions string) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/app/meet", nil))
	return w.Header().Get("Content-Security-Policy"), w.Header().Get("Permissions-Policy")
}

// LiveKit settings saved in the admin pages reach the browser's policy on
// the next check, without a restart; every fixed rule stays.
func TestSecurityHeadersFollowLiveKitSettings(t *testing.T) {
	t.Setenv("LIDZA_MODE", "test")
	t.Setenv("APP_URL", "")
	t.Setenv("THURA_BUNDLED_LIVEKIT", "")
	for _, name := range []string{"LIVEKIT_SERVER_URL", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"} {
		t.Setenv(name, "")
		t.Cleanup(func() { credentials.SetOverride(name, "") })
	}
	h := liveHeaders(livekit.Load, 0)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	csp, permissions := policyOf(t, h)
	if strings.Contains(csp, "wss://meet.example.com") || strings.Contains(permissions, "camera=(self)") {
		t.Fatalf("meeting access granted before configuration: %s | %s", csp, permissions)
	}
	credentials.SetOverride("LIVEKIT_SERVER_URL", "http://127.0.0.1:7880")
	credentials.SetOverride("LIVEKIT_PUBLIC_URL", "wss://meet.example.com/rtc")
	credentials.SetOverride("LIVEKIT_API_KEY", "key")
	credentials.SetOverride("LIVEKIT_API_SECRET", strings.Repeat("s", 32))
	csp, permissions = policyOf(t, h)
	if !strings.Contains(csp, "wss://meet.example.com") || !strings.Contains(csp, "https://meet.example.com") || !strings.Contains(permissions, "camera=(self)") {
		t.Fatalf("saved LiveKit settings did not reach the policy: %s | %s", csp, permissions)
	}
	for _, rule := range append([]string{"'sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU='", "data:"}, readerStyleHashes...) {
		if !strings.Contains(csp, rule) {
			t.Fatalf("policy lost %s: %s", rule, csp)
		}
	}
	credentials.SetOverride("LIVEKIT_PUBLIC_URL", "wss://other.example.org")
	if csp, _ = policyOf(t, h); !strings.Contains(csp, "wss://other.example.org") || strings.Contains(csp, "wss://meet.example.com") {
		t.Fatalf("changed public URL not followed: %s", csp)
	}
	// Within the interval the built handler is reused; nothing is reloaded.
	loads := 0
	cached := liveHeaders(func() (livekit.Config, error) { loads++; return livekit.Load() }, 1<<62)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for range 3 {
		policyOf(t, cached)
	}
	if loads != 1 {
		t.Fatalf("settings loaded %d times within one interval", loads)
	}
}
