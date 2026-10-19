package collabora

import (
	"context"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscoverySelectsAdvertisedFormatsAndReadOnlyPDF(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Prefix+"/hosting/discovery" {
			t.Error("wrong discovery endpoint")
		}
		w.Header().Set("Content-Type", "application/xml")
		if _, err := fmt.Fprint(w, `<wopi-discovery><net-zone><app><action ext="odt" name="edit" urlsrc="https://untrusted-host.example/api/v1/collabora/browser/hash/cool.html?"/><action ext="odt" name="view" urlsrc="https://untrusted-host.example/api/v1/collabora/browser/hash/cool.html?"/><action ext="pdf" name="view_comment" urlsrc="https://untrusted-host.example/api/v1/collabora/browser/hash/cool.html?"/></app></net-zone></wopi-discovery>`); err != nil {
			t.Error(err)
		}
	}))
	defer service.Close()
	c := Config{ServerURL: service.URL, Secret: "test-collabora-signed-access-maple-816"}
	extensions, _, err := c.Formats(context.Background())
	if err != nil || len(extensions) != 2 || extensions[0] != "odt" || extensions[1] != "pdf" {
		t.Fatalf("formats: %v %v", extensions, err)
	}
	for _, ext := range []string{"odt", "pdf"} {
		path, ro, err := c.Editor(context.Background(), ext, "", false)
		if err != nil || path != Prefix+"/browser/hash/cool.html" || ro != (ext == "pdf") {
			t.Fatalf("%s %s readonly=%v %v", ext, path, ro, err)
		}
	}
	if _, _, err = c.Editor(context.Background(), "unsupported", "", false); err == nil {
		t.Fatal("unadvertised format accepted")
	}
	if _, _, err = editorPath("https://service.example/other/script", false); err == nil {
		t.Fatal("foreign editor path accepted")
	}
	token, err := c.Sign(jwt.MapClaims{"aud": "thura-collabora", "exp": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Verify(token); err != nil {
		t.Fatal(err)
	}
	expired, err := c.Sign(jwt.MapClaims{"aud": "thura-collabora", "exp": time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Verify(expired); err == nil {
		t.Fatal("expired token accepted")
	}
	wrong, err := c.Sign(jwt.MapClaims{"aud": "other-service", "exp": time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Verify(wrong); err == nil {
		t.Fatal("wrong audience accepted")
	}
}
