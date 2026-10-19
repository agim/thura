package onlyoffice

import (
	"github.com/agim/lidza/pkg/router"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Proxy keeps browser assets, frames and sockets on Thura's authenticated origin.
// Browser session cookies never leave Thura; editor access uses signed tickets.
func Proxy(w http.ResponseWriter, r *http.Request) {
	// This authenticated integration is intentionally framed by Thura itself.
	// Other application responses retain the framework's DENY policy.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	cfg, err := Load()
	if err != nil {
		router.Error(w, 503, "Document editing is not configured")
		return
	}
	client, err := cfg.client(r.Context())
	if err != nil {
		router.Error(w, 503, "Document service trust configuration is unavailable")
		return
	}
	target, err := url.Parse(cfg.ServerURL)
	if err != nil {
		router.Error(w, 503, "Document service is unavailable")
		return
	}
	scheme := "http"
	host, _, splitErr := net.SplitHostPort(r.RemoteAddr)
	if splitErr != nil {
		host = ""
	}
	ip := net.ParseIP(host)
	if r.TLS != nil || (ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https") {
		scheme = "https"
	}
	proxy := httputil.ReverseProxy{Transport: client.Transport, ModifyResponse: func(response *http.Response) error {
		response.Header.Del("X-Frame-Options")
		return nil
	}, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.URL.Path = strings.TrimPrefix(p.In.URL.Path, "/api/v1/office/editor")
		p.Out.URL.RawPath = ""
		p.Out.Header.Del("Cookie")
		p.Out.Header.Del("Authorization")
		p.Out.Header.Del("X-Forwarded-For")
		p.Out.Header.Set("X-Forwarded-Host", p.In.Host+"/api/v1/office/editor")
		p.Out.Header.Set("X-Forwarded-Proto", scheme)
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		router.Error(w, 502, "The document service could not be reached")
	}}
	proxy.ServeHTTP(w, r)
}
