package servicehttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollaboraProxyPreservesPathRemovesCredentialsAndUsesCorrectSocketScheme(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			t.Setenv("APP_URL", scheme+"://thura.example")
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/collabora/browser/hash/cool.html" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Forwarded-For") != "" {
					t.Error("path or credential isolation failed")
				}
				if r.Header.Get("X-Forwarded-Proto") != scheme || r.Host != "thura.example" {
					t.Error("incorrect public origin")
				}
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("X-Frame-Options", "DENY")
				if _, err := fmt.Fprint(w, `<div data-host = "wss://thura.example"></div>`); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			request := httptest.NewRequest("GET", scheme+"://thura.example/api/v1/collabora/browser/hash/cool.html", nil)
			request.RemoteAddr = "127.0.0.1:32100"
			request.Header.Set("Cookie", "private-app-session")
			request.Header.Set("Authorization", "Bearer private")
			request.Header.Set("X-Forwarded-For", "private-ip")
			if scheme == "https" {
				request.Header.Set("X-Forwarded-Proto", "https")
			}
			response := httptest.NewRecorder()
			Proxy(response, request, upstream.URL, "")
			socket := "ws://"
			if scheme == "https" {
				socket = "wss://"
			}
			if response.Code != 200 || !strings.Contains(response.Body.String(), `data-host = "`+socket+`thura.example"`) || response.Header().Get("Content-Security-Policy") != "frame-ancestors 'self'" {
				t.Fatalf("proxy response %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHTTPSOfficeBootstrapBehindDockerPreservesSecureSocket(t *testing.T) {
	t.Setenv("APP_URL", "https://thura.example")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Error("private HTTP hop replaced public HTTPS scheme")
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div data-host = "wss://thura.example"></div>`)
	}))
	defer upstream.Close()
	r := httptest.NewRequest("GET", "http://thura.example/api/v1/collabora/browser/hash/cool.html", nil)
	r.RemoteAddr = "172.19.0.1:32100"
	r.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	Proxy(w, r, upstream.URL, "")
	if Origin(r) != "https://thura.example" || w.Code != 200 || !strings.Contains(w.Body.String(), `data-host = "wss://thura.example"`) {
		t.Fatalf("HTTPS bootstrap downgraded: %d %s", w.Code, w.Body.String())
	}
}

func TestDocumentOriginDoesNotTrustForwardedHTTPSFromNetworkPeers(t *testing.T) {
	t.Setenv("APP_URL", "http://localhost:3000")
	r := httptest.NewRequest("GET", "http://thura.example/api/v1/collabora/browser/hash/cool.html", nil)
	r.RemoteAddr = "172.19.0.1:32100"
	r.Header.Set("X-Forwarded-Proto", "https")
	if Origin(r) != "http://thura.example" {
		t.Fatal("untrusted forwarding header changed browser origin")
	}
}
