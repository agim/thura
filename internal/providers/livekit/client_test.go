package livekit

import (
	"slices"
	"strings"
	"testing"
)

func TestLoadAcceptsProxiedPublicPath(t *testing.T) {
	t.Setenv("LIDZA_MODE", "test")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", strings.Repeat("s", 32))
	t.Setenv("LIVEKIT_SERVER_URL", "http://127.0.0.1:7880")
	// scripts/init-compose.py and deploy/lidza.stack.json publish LiveKit at /rtc.
	t.Setenv("LIVEKIT_PUBLIC_URL", "wss://thura.example.com/rtc/")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "wss://thura.example.com/rtc" {
		t.Fatalf("public URL %q", c.PublicURL)
	}
	if got := c.PublicOrigins(); !slices.Equal(got, []string{"wss://thura.example.com", "https://thura.example.com"}) {
		t.Fatalf("CSP origins %v", got)
	}
}

func TestLoadNamesTheWrongSetting(t *testing.T) {
	t.Setenv("LIDZA_MODE", "test")
	t.Setenv("LIVEKIT_API_KEY", "key")
	t.Setenv("LIVEKIT_API_SECRET", strings.Repeat("s", 32))
	t.Setenv("LIVEKIT_PUBLIC_URL", "wss://thura.example.com/rtc")
	for value, want := range map[string]string{"": "LIVEKIT_SERVER_URL", "http://127.0.0.1:7880/api": "LIVEKIT_SERVER_URL", "ws://127.0.0.1:7880": "LIVEKIT_SERVER_URL"} {
		t.Setenv("LIVEKIT_SERVER_URL", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("server URL %q: %v", value, err)
		}
	}
	t.Setenv("LIVEKIT_SERVER_URL", "http://127.0.0.1:7880")
	for _, value := range []string{"", "https://thura.example.com/rtc", "wss://user@thura.example.com/rtc", "wss://thura.example.com/rtc?x=1"} {
		t.Setenv("LIVEKIT_PUBLIC_URL", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LIVEKIT_PUBLIC_URL") {
			t.Fatalf("public URL %q: %v", value, err)
		}
	}
	t.Setenv("LIVEKIT_API_SECRET", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LIVEKIT_API_SECRET") {
		t.Fatalf("short secret: %v", err)
	}
}
