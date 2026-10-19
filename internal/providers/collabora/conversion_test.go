package collabora

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConversionUploadsOnlyExactSourceToFixedService(t *testing.T) {
	var received bool
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Prefix+"/cool/convert-to/docx" || r.Method != "POST" {
			t.Error("wrong conversion endpoint")
		}
		part, header, err := r.FormFile("data")
		if err != nil {
			t.Error(err)
			return
		}
		defer part.Close()
		body, err := io.ReadAll(part)
		if err != nil || header.Filename != "source.odt" || string(body) != "authorized-source" {
			t.Error("source bytes/name not bound")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("app credentials forwarded")
		}
		received = true
		if _, err = w.Write([]byte("PK\x03\x04converted")); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	c := Config{ServerURL: service.URL}
	out, err := c.Convert(context.Background(), "source.odt", "docx", []byte("authorized-source"))
	if err != nil || string(out) != "PK\x03\x04converted" || !received {
		t.Fatalf("converted source: %q %v", out, err)
	}
	if _, err = c.Convert(context.Background(), "source.odt", "../../foreign", nil); err == nil {
		t.Fatal("unsupported destination escaped fixed endpoint")
	}
}
func TestConversionRefusesRedirectsAndProviderErrors(t *testing.T) {
	var foreignReads int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignReads++; w.WriteHeader(200) }))
	defer foreign.Close()
	for _, status := range []int{302, 500} {
		service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", foreign.URL)
			w.WriteHeader(status)
		}))
		c := Config{ServerURL: service.URL}
		_, err := c.Convert(context.Background(), "file.pdf", "docx", []byte("source"))
		service.Close()
		if err == nil {
			t.Fatal("failed or redirected conversion accepted")
		}
	}
	if foreignReads != 0 {
		t.Fatal("conversion fetched a foreign origin")
	}
}
func TestAuthenticatedOriginProxyRetainsServiceRootAndStripsAppCredentials(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Prefix+"/browser/hash/script.js" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("proxy path or credentials escaped")
		}
		if r.Header.Get("X-Forwarded-Host") != "app.example" {
			t.Error("wrong public host")
		}
		w.Header().Set("X-Frame-Options", "DENY")
		if _, err := w.Write([]byte("editor asset")); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	t.Setenv("COLLABORA_SERVER_URL", service.URL)
	t.Setenv("COLLABORA_APP_URL", "https://app.example")
	t.Setenv("COLLABORA_TOKEN_SECRET", "synthetic-collabora-access-key-maple-73619284")
	request := httptest.NewRequest("GET", "http://app.example"+Prefix+"/browser/hash/script.js", nil)
	request.Header.Set("Cookie", "app-session=synthetic")
	request.Header.Set("Authorization", "Bearer synthetic")
	response := httptest.NewRecorder()
	Proxy(response, request)
	if response.Code != 200 || response.Body.String() != "editor asset" || response.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("proxy: %d %v", response.Code, response.Header())
	}
}

func TestDocumentSocketPathIsRedactedAfterForwarding(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "access_token=private-grant") {
			t.Error("upstream lost document grant")
		}
		w.WriteHeader(200)
	}))
	defer service.Close()
	t.Setenv("COLLABORA_SERVER_URL", service.URL)
	t.Setenv("COLLABORA_APP_URL", "https://app.example")
	t.Setenv("COLLABORA_TOKEN_SECRET", "synthetic-collabora-redaction-key-maple-73619284")
	request := httptest.NewRequest("GET", "http://app.example"+Prefix+"/cool/http%3A%2F%2Fstorage.example%2Ffile%3Faccess_token%3Dprivate-grant/ws", nil)
	response := httptest.NewRecorder()
	Proxy(response, request)
	if response.Code != 200 || strings.Contains(request.URL.String(), "private-grant") || strings.Contains(request.RequestURI, "private-grant") {
		t.Fatal("bearer grant remains visible to access logging")
	}
}

func TestBrowserProxyDoesNotExposePrivateConversion(t *testing.T) {
	request := httptest.NewRequest("POST", "http://app.example"+Prefix+"/cool/convert-to/docx", strings.NewReader("unbounded upload"))
	response := httptest.NewRecorder()
	Proxy(response, request)
	if response.Code != 404 {
		t.Fatal("private converter exposed through browser proxy")
	}
}
