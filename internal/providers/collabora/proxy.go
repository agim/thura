package collabora

import (
	"github.com/agim/lidza/pkg/router"
	"net/http"
	"strings"
	"thura/internal/platform/servicehttp"
)

func Proxy(w http.ResponseWriter, r *http.Request) {
	// Conversion is server-to-server through Drive's bounded, authorized API.
	// Do not expose the private converter through the browser asset proxy.
	if strings.HasPrefix(r.URL.Path, Prefix+"/cool/convert-to") {
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
