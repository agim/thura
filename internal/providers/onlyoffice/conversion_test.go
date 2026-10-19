package onlyoffice

import (
	"context"
	"encoding/json"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConversionSignsSourceAndRefusesForeignDownloads(t *testing.T) {
	secret := "synthetic-onlyoffice-conversion-test-secret-81923"
	var received bool
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/converter" || r.Method != "POST" {
			t.Error("wrong conversion route")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		token, ok := in["token"].(string)
		if !ok {
			t.Error("missing token")
			return
		}
		parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			t.Error(err)
			return
		}
		claims := parsed.Claims.(jwt.MapClaims)
		if claims["url"] != "https://app.example/source?ticket=signed" || claims["filetype"] != "pdf" || claims["outputtype"] != "docx" || claims["key"] != "fixture-v1-convert" {
			t.Error("conversion signature did not bind exact input and output")
		}
		received = true
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"endConvert": false, "percent": 42}); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	cfg := Config{ServerURL: service.URL, Secret: secret}
	out, err := cfg.Convert(context.Background(), "pdf", "docx", "fixture-v1-convert", "https://app.example/source?ticket=signed")
	if err != nil || out.End || out.Percent != 42 || !received {
		t.Fatalf("conversion %+v %v", out, err)
	}
	if _, err = cfg.Download(context.Background(), "https://other.example/private"); err == nil {
		t.Fatal("foreign download origin accepted")
	}
}

func TestProxyCacheDownloadMapsToFixedService(t *testing.T) {
	var path string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.RawQuery != "signature=fixture" {
			t.Error("lost signed cache query")
		}
		if _, err := w.Write([]byte("saved document")); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	cfg := Config{ServerURL: service.URL}
	bytes, err := cfg.Download(context.Background(), "https://app.example/api/v1/office/editor/cache/files/fixture/output.docx?signature=fixture")
	if err != nil || string(bytes) != "saved document" || path != "/cache/files/fixture/output.docx" {
		t.Fatalf("fixed service download: %q %s %v", bytes, path, err)
	}
	if _, err = cfg.Download(context.Background(), "https://app.example/api/v1/office/editor/cache/../../admin"); err == nil {
		t.Fatal("traversal escaped cache mapping")
	}
}

func TestAuthenticatedOriginProxyDoesNotForwardAppCredentials(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web-apps/script.js" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("proxy forwarded app credentials or wrong path")
		}
		if r.Header.Get("X-Forwarded-Host") != "app.example/api/v1/office/editor" {
			t.Error("virtual path missing")
		}
		w.Header().Set("X-Frame-Options", "DENY")
		if _, err := w.Write([]byte("editor asset")); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	t.Setenv("OFFICE_SERVER_URL", service.URL)
	t.Setenv("OFFICE_APP_URL", "http://app.example")
	t.Setenv("OFFICE_JWT_SECRET", "synthetic-test-office-key-maple-73619284")
	request := httptest.NewRequest("GET", "http://app.example/api/v1/office/editor/web-apps/script.js", nil)
	request.Header.Set("Cookie", "app-session=synthetic")
	request.Header.Set("Authorization", "Bearer synthetic")
	response := httptest.NewRecorder()
	Proxy(response, request)
	if response.Code != 200 || response.Body.String() != "editor asset" || response.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("proxy response %d %v", response.Code, response.Header())
	}
}
