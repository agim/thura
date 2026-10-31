package livekit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func signalConfig(t *testing.T, handler http.HandlerFunc) (Config, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return Config{URL: "http://127.0.0.1:7880", PublicURL: "ws://" + u.Host + "/rtc", Key: "key", Secret: strings.Repeat("s", 32)}, u.Hostname()
}

func TestCheckSignalAcceptsLiveKitBehindStrippedPrefix(t *testing.T) {
	c, host := signalConfig(t, func(w http.ResponseWriter, r *http.Request) {
		// livekit-client's validate path, as the proxy forwards it.
		if r.URL.Path != "/rtc/rtc/validate" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(w, "no permissions to access the room", http.StatusUnauthorized)
	})
	if err := c.CheckSignal(context.Background(), host, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSignalReportsAMissingProxyRoute(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"404": http.NotFound,
		"app page": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>Thura</title>"))
		},
		"html error": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("<html>Sign in</html>"))
		},
		"bad gateway": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"redirect":    func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusFound) },
	} {
		c, host := signalConfig(t, handler)
		if err := c.CheckSignal(context.Background(), host, nil); !errors.Is(err, ErrSignalNotForwarded) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestCheckSignalReportsATimeout(t *testing.T) {
	release := make(chan struct{})
	c, host := signalConfig(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	previous := SignalTimeout
	SignalTimeout = 200 * time.Millisecond
	t.Cleanup(func() { SignalTimeout = previous })
	if err := c.CheckSignal(context.Background(), host, nil); !errors.Is(err, ErrSignalTimeout) {
		t.Fatal(err)
	}
}

func TestCheckSignalRefusesPrivateAddresses(t *testing.T) {
	calls := 0
	c, _ := signalConfig(t, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusUnauthorized) })
	// The app's own domain may resolve locally; another host may not.
	if err := c.CheckSignal(context.Background(), "thura.example.com", nil); !errors.Is(err, ErrSignalRefusedHost) {
		t.Fatalf("loopback for a foreign host: %v", err)
	}
	for _, public := range []string{"wss://10.0.0.7/rtc", "wss://192.168.1.4/rtc", "wss://[fd00::1]/rtc", "wss://169.254.169.254/rtc", "wss://[fe80::1]/rtc", "wss://0.0.0.0/rtc", "wss://100.64.0.1/rtc"} {
		c.PublicURL = public
		if err := c.CheckSignal(context.Background(), "thura.example.com", nil); !errors.Is(err, ErrSignalRefusedHost) {
			t.Fatalf("%s: %v", public, err)
		}
	}
	// Metadata stays refused even when named as the app's own host.
	c.PublicURL = "wss://169.254.169.254/rtc"
	if err := c.CheckSignal(context.Background(), "169.254.169.254", nil); !errors.Is(err, ErrSignalRefusedHost) {
		t.Fatalf("metadata as own host: %v", err)
	}
	if calls != 0 {
		t.Fatal("a refused address was contacted")
	}
}
