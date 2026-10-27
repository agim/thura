package tests

import (
	"encoding/json"
	"github.com/agim/lidza/packs/auth"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"thura/internal/providers/collabora"
	"thura/schema"
	"time"
)

func TestOfficeWopiEndpointAuthenticationFileBindingAndExpiry(t *testing.T) {
	f := newWopiFixture(t)
	base := "/api/v1/workspaces/" + f.workspaceID + "/drive/files/" + f.f.ID
	if r := f.srv.JSON(t, "POST", base+"/office", nil, nil); r.StatusCode != 401 {
		t.Fatal("anonymous editor open accepted")
	}
	if r := f.srv.JSON(t, "GET", "/api/v1/collabora/browser/test/cool.html", nil, nil); r.StatusCode != 401 {
		t.Fatal("anonymous proxy access accepted")
	}
	if r := f.srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: f.email, Password: "test waterfall forest lantern 7823"}, nil); r.StatusCode != 200 {
		t.Fatal("fixture sign-in failed")
	}
	var config schema.OfficeConfig
	if r := f.srv.JSON(t, "POST", base+"/office", nil, &config); r.StatusCode != 200 {
		t.Fatalf("open: %d", r.StatusCode)
	}
	cfg, err := collabora.Load()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := cfg.Verify(config.AccessToken)
	if err != nil || config.Provider != "collabora" || claims["fid"] != f.f.ID || claims["ro"] != false || !strings.HasPrefix(config.EditorURL, "/api/v1/collabora/browser/") {
		t.Fatal("editor grant not signed and bound to the file")
	}
	read := func(id, token string) int {
		t.Helper()
		request, err := http.NewRequest("GET", f.srv.URL+"/api/v1/wopi/files/"+id+"/contents?access_token="+url.QueryEscape(token), nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := f.srv.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if read(f.f.ID, config.AccessToken) != 200 {
		t.Fatal("valid file source unavailable")
	}
	if read("c0000000-0000-4000-8000-000000000999", config.AccessToken) != 401 {
		t.Fatal("file-scoped token escaped to another file")
	}
	if read(f.f.ID, config.AccessToken+"tampered") != 401 {
		t.Fatal("tampered token accepted")
	}
	forged := jwt.MapClaims{}
	for key, value := range claims {
		forged[key] = value
	}
	forged["fid"] = "c0000000-0000-4000-8000-000000000999"
	attacker := collabora.Config{Secret: "unrelated-service-secret-maple-8162734"}
	bad, err := attacker.Sign(forged)
	if err != nil {
		t.Fatal(err)
	}
	if read(f.f.ID, bad) != 401 {
		t.Fatal("unverified source fetched data")
	}
	expired := jwt.MapClaims{}
	for key, value := range claims {
		expired[key] = value
	}
	expired["exp"] = time.Now().Add(-time.Hour).Unix()
	expiredToken, err := cfg.Sign(expired)
	if err != nil {
		t.Fatal(err)
	}
	if read(f.f.ID, expiredToken) != 401 {
		t.Fatal("expired token accepted")
	}
	var capabilities schema.OfficeCapabilities
	if r := f.srv.JSON(t, "GET", "/api/v1/workspaces/"+f.workspaceID+"/drive/office-capabilities", nil, &capabilities); r.StatusCode != 200 || len(capabilities.Extensions) == 0 {
		t.Fatal("advertised formats missing")
	}
	f.srv.Clock.Advance(9 * time.Hour)
	if read(f.f.ID, config.AccessToken) != 401 {
		t.Fatal("expired source session accepted")
	}
	request, err := http.NewRequest("POST", f.srv.URL+"/api/v1/wopi/files/"+f.f.ID+"/contents?access_token="+url.QueryEscape(config.AccessToken), strings.NewReader("late save"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-WOPI-Override", "PUT")
	response, err := f.srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("expired session save accepted")
	}
	// The bearer grant contains no foreign source URL; changing its claims also invalidates its signature.
	b, err := json.Marshal(claims)
	if err != nil || strings.Contains(string(b), "http://untrusted") {
		t.Fatal("untrusted source address in grant")
	}
}
