package collabora

import (
	"github.com/agim/lidza/pkg/router"
	"net/http"
	"strings"
	"thura/internal/platform/servicehttp"
)

// allowed admits only what the browser editor loads: its static assets, the
// WOPI discovery and the document WebSocket. Collabora trusts Thura's own
// address, so every other endpoint (converters, thumbnails, link extraction,
// the admin console) stays private to server-to-server calls.
func allowed(r *http.Request) bool {
	// The escaped path keeps an encoded WOPISrc as one segment.
	rest := strings.TrimPrefix(r.URL.EscapedPath(), Prefix)
	switch {
	case strings.HasPrefix(rest, "/browser/"):
		// Plain asset paths only; cool.html itself is POSTed the file-scoped
		// token by the editor form.
		if strings.Contains(rest, "..") || strings.Contains(rest, "%") {
			return false
		}
		return r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodPost && strings.HasSuffix(rest, "/cool.html")
	case rest == "/hosting/discovery" || rest == "/hosting/capabilities":
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	case strings.HasPrefix(rest, "/cool/") && strings.HasSuffix(rest, "/ws"):
		// /cool/<WOPISrc>/ws: exactly one document segment between.
		return r.Method == http.MethodGet && !strings.Contains(strings.TrimSuffix(strings.TrimPrefix(rest, "/cool/"), "/ws"), "/")
	}
	return false
}

func Proxy(w http.ResponseWriter, r *http.Request) {
	if !allowed(r) {
		router.Error(w, http.StatusNotFound, "Document resource unavailable")
		return
	}
	// CODE's compatibility WebSocket URL embeds its bearer grant in the path.
	// Forward it intact, then redact before outer access/panic logging runs.
	if strings.HasPrefix(r.URL.Path, Prefix+"/cool/") && strings.Contains(r.URL.Path, "access_token") {
		defer func() {
			r.URL.Path = Prefix + "/cool/[document]/ws"
			r.URL.RawPath = ""
			r.URL.RawQuery = ""
			r.RequestURI = r.URL.Path
		}()
	}
	c, e := Load()
	if e != nil {
		router.Error(w, 503, "Collabora editing is not configured")
		return
	}
	servicehttp.Proxy(w, r, c.ServerURL, c.CAFile)
}
