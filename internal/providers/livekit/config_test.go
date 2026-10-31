package livekit

import (
	"strings"
	"testing"

	"github.com/agim/lidza/pkg/credentials"
)

func liveKitKeys(t *testing.T) {
	t.Helper()
	t.Setenv("LIDZA_MODE", "test")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", strings.Repeat("s", 32))
	t.Setenv("LIVEKIT_SERVER_URL", "http://127.0.0.1:7880")
}

// saved stands in for a value saved in the app's settings (the admin pages
// or a published setup), which pkg/env reads from the runtime overrides.
func saved(t *testing.T, name, value string) {
	t.Helper()
	credentials.SetOverride(name, value)
	t.Cleanup(func() { credentials.SetOverride(name, "") })
}

func TestLoadDefaultsPublicURLFromAppOrigin(t *testing.T) {
	liveKitKeys(t)
	t.Setenv("LIVEKIT_PUBLIC_URL", "")
	t.Setenv("APP_URL", "https://thura.example.com/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "wss://thura.example.com/rtc" {
		t.Fatalf("public URL %q", c.PublicURL)
	}
	// The app origin saved in the setup, with an empty APP_URL in the process.
	t.Setenv("APP_URL", "")
	saved(t, "APP_URL", "https://saved.example.org")
	if c, err = Load(); err != nil || c.PublicURL != "wss://saved.example.org/rtc" {
		t.Fatalf("public URL %q: %v", c.PublicURL, err)
	}
}

func TestLoadEmptyEnvironmentKeepsSavedSetting(t *testing.T) {
	liveKitKeys(t)
	t.Setenv("APP_URL", "https://thura.example.com")
	// Compose passes LIVEKIT_PUBLIC_URL through empty when .env lacks it.
	t.Setenv("LIVEKIT_PUBLIC_URL", "")
	saved(t, "LIVEKIT_PUBLIC_URL", "wss://media.example.net/livekit")
	t.Setenv("LIVEKIT_API_SECRET", "")
	saved(t, "LIVEKIT_API_SECRET", strings.Repeat("v", 40))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "wss://media.example.net/livekit" || c.Secret != strings.Repeat("v", 40) {
		t.Fatalf("saved settings hidden by empty environment: %q", c.PublicURL)
	}
}

func TestLoadExplicitPublicURLWins(t *testing.T) {
	liveKitKeys(t)
	t.Setenv("APP_URL", "https://thura.example.com")
	saved(t, "LIVEKIT_PUBLIC_URL", "wss://saved.example.net/rtc")
	t.Setenv("LIVEKIT_PUBLIC_URL", "wss://meet.example.com")
	c, err := Load()
	if err != nil || c.PublicURL != "wss://meet.example.com" {
		t.Fatalf("public URL %q: %v", c.PublicURL, err)
	}
	t.Setenv("LIVEKIT_PUBLIC_URL", "")
	if c, err = Load(); err != nil || c.PublicURL != "wss://saved.example.net/rtc" {
		t.Fatalf("saved public URL lost to the default: %q: %v", c.PublicURL, err)
	}
}

func TestLoadDefaultsServerURLOnlyBesideBundledLiveKit(t *testing.T) {
	liveKitKeys(t)
	t.Setenv("APP_URL", "https://thura.example.com")
	t.Setenv("LIVEKIT_SERVER_URL", "")
	t.Setenv("THURA_BUNDLED_LIVEKIT", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LIVEKIT_SERVER_URL is required") {
		t.Fatalf("separate LiveKit without a server URL: %v", err)
	}
	t.Setenv("THURA_BUNDLED_LIVEKIT", "true")
	if c, err := Load(); err != nil || c.URL != BundledURL {
		t.Fatalf("bundled LiveKit: %q: %v", c.URL, err)
	}
}

func TestDefaultPublicURLNeedsAnOrigin(t *testing.T) {
	for in, want := range map[string]string{"https://a.example": "wss://a.example/rtc", "http://a.example:8080": "ws://a.example:8080/rtc", "": "", "a.example": "", "ftp://a.example": "", "https://u:p@a.example": ""} {
		if got := DefaultPublicURL(in); got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}
