package setup

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportChallengeRejectsForgedForwardingAndOldTLS(t *testing.T) {
	id := uuid.NewString()
	for _, version := range []uint16{0, tls.VersionTLS12, tls.VersionTLS13} {
		r := httptest.NewRequest("GET", "https://thura.example/api/v1/setup/transport?nonce="+id, nil)
		r.TLS = nil
		if version != 0 {
			r.TLS = &tls.ConnectionState{Version: version}
		}
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-TLS-Version", "TLS1.3")
		out := httptest.NewRecorder()
		TransportCheck(out, r)
		if version < tls.VersionTLS13 {
			if out.Code == 200 {
				t.Fatal("unverified TLS accepted")
			}
			continue
		}
		if out.Code != 200 || !strings.Contains(out.Body.String(), id) || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("valid transport challenge failed")
		}
	}
}

func TestLiveKitProbeCreatesAndCleansRoomWithAuthentication(t *testing.T) {
	calls := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing authenticated probe")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "CreateRoom") {
			w.WriteHeader(500)
		}
		if _, err := w.Write([]byte(`{}`)); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	err := checkIntegration(context.Background(), "livekit", map[string]string{"LIVEKIT_SERVER_URL": upstream.URL, "LIVEKIT_API_KEY": "test-key", "LIVEKIT_API_SECRET": strings.Repeat("s", 32)}, uuid.NewString())
	if err == nil {
		t.Fatal("failed room creation accepted")
	}
	if len(calls) != 2 || !strings.HasSuffix(calls[0], "CreateRoom") || !strings.HasSuffix(calls[1], "DeleteRoom") {
		t.Fatal("uncertain create was not cleaned up")
	}
}
