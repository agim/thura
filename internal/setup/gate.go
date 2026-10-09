package setup

import (
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/router"
)

// Gate keeps an unfinished installation in its bootstrap and configuration
// pages. Read the database on each request so other nodes see claims and
// publications immediately; never cache redirects in a browser or proxy.
func Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if setupResource(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/setup/status" {
			w.Header().Set("Cache-Control", "no-store")
		}
		status, err := Status(r.Context())
		if err != nil {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Server setup status is unavailable", http.StatusServiceUnavailable)
			return
		}
		if status.Claimed && status.Active {
			next.ServeHTTP(w, r)
			return
		}
		// Configured provider callbacks own their Authorization headers. Only
		// resolve app sessions when an unfinished installation needs routing.
		auth.Optional()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			status.Administrator = Allow(r.Context())
			w.Header().Set("Cache-Control", "no-store")
			path := r.URL.Path
			switch path {
			case "/api/v1/setup/status", "/api/v1/setup/claim",
				"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/session",
				"/api/v1/auth/register": // NoRegister still returns 404.
				next.ServeHTTP(w, r)
				return
			}
			target := "/setup"
			if status.Claimed && status.Administrator {
				target = "/admin/setup"
			}
			if path == target || (status.Claimed && auth.CurrentUser(r.Context()) != nil && (path == "/admin/setup" || path == "/admin/setup/save" || path == "/admin/setup/restart" || path == "/admin/setup/publish")) {
				// The official admin handler retains its authorization and CSRF checks.
				next.ServeHTTP(w, r)
				return
			}
			if path == "/readyz" || strings.HasPrefix(path, "/api/") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
				router.Error(w, http.StatusServiceUnavailable, "Server setup is required. Open "+target)
				return
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
		})).ServeHTTP(w, r)
	})
}

func setupResource(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	requestPath := r.URL.Path
	if path.Clean(requestPath) != requestPath {
		return false
	}
	path := requestPath
	if path == "/healthz" || path == "/api/v1/health" || path == "/admin/theme.css" || path == "/favicon.ico" || path == "/favicon.svg" || strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/admin/assets/") {
		return true
	}
	if os.Getenv("LIDZA_MODE") == "dev" {
		for _, prefix := range []string{"/src/", "/node_modules/", "/@vite/", "/@id/", "/@fs/"} {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
		return path == "/@react-refresh"
	}
	return false
}
