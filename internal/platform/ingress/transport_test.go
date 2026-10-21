package ingress

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agim/lidza/pkg/gateway"
)

func TestOriginalTLSWinsOverBackendTLSAndForwardedHeaders(t *testing.T) {
	for _, original := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		var accepted bool
		h := gateway.Edge(func(_ context.Context, authority string) error {
			if authority != "thura.example" {
				return errors.New("unapproved authority")
			}
			return nil
		})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// Model a backend hop after original evidence has been verified. This
			// test-only connection state must not replace the original TLS version.
			backend := r.Clone(r.Context())
			backend.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
			accepted = TLS13(backend)
		}))
		r := httptest.NewRequest("GET", "https://thura.example/api/v1/setup/transport", nil)
		r.TLS = &tls.ConnectionState{Version: original, HandshakeComplete: true, ServerName: "thura.example"}
		h.ServeHTTP(httptest.NewRecorder(), r)
		if accepted != (original == tls.VersionTLS13) {
			t.Fatalf("backend TLS replaced original TLS %x", original)
		}
	}
	r := httptest.NewRequest("GET", "http://thura.example/api/v1/setup/transport", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set(gateway.Header, "forged")
	if TLS13(r) {
		t.Fatal("forwarded headers created TLS evidence")
	}
}
