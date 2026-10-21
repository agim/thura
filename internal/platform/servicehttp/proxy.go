package servicehttp

import (
	"bytes"
	"errors"
	"github.com/agim/lidza/pkg/router"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
)

func Origin(r *http.Request) string {
	scheme := "http"
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = ""
	}
	ip := net.ParseIP(host)
	// APP_URL declares the public browser scheme behind Docker/private HTTP
	// proxies. It is URL construction policy, never SMIP transport evidence.
	public, err := url.Parse(os.Getenv("APP_URL"))
	publicHTTPS := err == nil && public.Scheme == "https" && public.Host != ""
	if publicHTTPS || r.TLS != nil || ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Proxy forwards only to an operator-configured origin, retaining no app credentials.
func Proxy(w http.ResponseWriter, r *http.Request, server, ca string) {
	client, e := Client(r.Context(), ca)
	if e != nil {
		router.Error(w, 503, "Document service trust configuration is unavailable")
		return
	}
	target, e := url.Parse(server)
	if e != nil {
		router.Error(w, 503, "Document service is unavailable")
		return
	}
	origin, e := url.Parse(Origin(r))
	if e != nil {
		router.Error(w, 400, "Invalid request origin")
		return
	}
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	// CODE's TLS-termination setting emits wss even for loopback HTTP dev.
	// Production HTTPS retains wss; only the bootstrap's socket scheme changes.
	localBootstrap := origin.Scheme == "http" && strings.HasSuffix(r.URL.Path, "/cool.html")
	proxy := httputil.ReverseProxy{Transport: client.Transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.Host = p.In.Host
		p.Out.Header.Del("Cookie")
		p.Out.Header.Del("Authorization")
		p.Out.Header.Del("X-Forwarded-For")
		p.Out.Header.Set("X-Forwarded-Host", p.In.Host)
		p.Out.Header.Set("X-Forwarded-Proto", origin.Scheme)
		if localBootstrap {
			p.Out.Header.Set("Accept-Encoding", "identity")
		}
	}, ModifyResponse: func(res *http.Response) error {
		res.Header.Del("X-Frame-Options")
		res.Header.Set("Content-Security-Policy", "frame-ancestors 'self'")
		if localBootstrap && res.StatusCode == http.StatusOK && strings.Contains(res.Header.Get("Content-Type"), "text/html") {
			body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
			if closeErr := res.Body.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			if len(body) > 2<<20 {
				return errors.New("document bootstrap exceeds size limit")
			}
			body = bytes.ReplaceAll(body, []byte(`data-host = "wss://`), []byte(`data-host = "ws://`))
			res.Body = io.NopCloser(bytes.NewReader(body))
			res.ContentLength = int64(len(body))
			res.Header.Set("Content-Length", strconv.Itoa(len(body)))
			res.Header.Del("ETag")
			res.Header.Set("Cache-Control", "no-store")
		}
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
		router.Error(w, 502, "The document service could not be reached")
	}}
	proxy.ServeHTTP(w, r)
}
