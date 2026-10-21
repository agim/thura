// Package ingress adapts Thura's native TLS and verified deployment ingress.
package ingress

import (
	"crypto/tls"
	"net/http"

	"github.com/agim/lidza/pkg/gateway"
)

// TLS13 prefers verified original-client evidence over the backend connection.
// Bare TLS remains available to the standalone protocol receiver. Deployment
// gateway mode must install gateway.Verify before every protected handler; its
// private listener must never fall through to the standalone path.
func TLS13(r *http.Request) bool {
	if _, verified := gateway.From(r.Context()); verified {
		return gateway.RequireTLS13(r)
	}
	return r.TLS != nil && r.TLS.Version == tls.VersionTLS13
}
