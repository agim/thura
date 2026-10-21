package ingress

import (
	"net/http"

	"github.com/agim/lidza/pkg/gateway"
)

func ProtectedPath(path string) bool {
	switch path {
	case "/api/v1/smip/v0.1/messages", "/api/v1/smip/v0.2/mail", "/api/v1/smip/v0.2/capabilities", "/api/v1/setup/transport":
		return true
	}
	return false
}

// Protect applies to both HTTP and private mTLS listeners. Protected protocols
// cannot reach their standalone TLS path without verified original evidence.
// Internal HTTP services keep their existing application authentication.
func Protect(policy gateway.Policy) (func(http.Handler) http.Handler, error) {
	verify, err := gateway.Verify(policy)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		protected := verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !gateway.RequireTLS13(r) {
				http.Error(w, "Original TLS 1.3 is required for SMIP", http.StatusBadRequest)
				return
			}
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ProtectedPath(r.URL.Path) {
				protected.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}
