package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	provider "thura/internal/providers/onlyoffice"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestOfficeSignedCallbacksVersionConflictAndRevocation(t *testing.T) {
	var downloads atomic.Int32
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		w.Write([]byte("edited document fixture"))
	}))
	defer documents.Close()
	secret := "office-test-fixture-secret-mountain-871963"
	t.Setenv("OFFICE_SERVER_URL", documents.URL)
	t.Setenv("OFFICE_APP_URL", "http://app.example")
	t.Setenv("OFFICE_JWT_SECRET", secret)
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("office-%d@example.com", time.Now().UnixNano())
	pw := "forest waterfall copper lantern 7193"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Office test", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	queries := q.New(db.From(ctx))
	f, err := queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: w.ID, Name: "fixture.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: 7})
	if err != nil {
		t.Fatal(err)
	}
	key := "office-test/" + f.ID
	if _, err = storage.From(ctx).Put(ctx, key, strings.NewReader("initial"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: 1, ObjectKey: key, Checksum: drive.Hash([]byte("initial")), Size: 7, CreatedBy: owner.Subject}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM office_session WHERE file_id=$1", "DELETE FROM file_version WHERE file_id=$1", "DELETE FROM drive_file WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, f.ID); e != nil {
				t.Error(e)
			}
		}
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, in, out)
		if r.StatusCode != want {
			t.Fatalf("%s got %d want %d", path, r.StatusCode, want)
		}
	}
	base := "/api/v1/workspaces/" + w.ID + "/drive/files/" + f.ID
	status("POST", base+"/office", nil, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	var config schema.OfficeConfig
	open := func() (string, string, string) {
		t.Helper()
		status("POST", base+"/office", nil, &config, 200)
		var c map[string]any
		if e := json.Unmarshal(config.Config, &c); e != nil {
			t.Fatal(e)
		}
		doc := c["document"].(map[string]any)
		editor := c["editorConfig"].(map[string]any)
		token := c["token"].(string)
		cfg, e := provider.Load()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = cfg.Verify(token); e != nil {
			t.Fatal("unsigned editor config", e)
		}
		return strings.TrimPrefix(editor["callbackUrl"].(string), "http://app.example"), strings.TrimPrefix(doc["url"].(string), "http://app.example"), doc["key"].(string)
	}
	a, source, docKey := open()
	b, _, _ := open()
	response := srv.JSON(t, "GET", source, nil, nil)
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(raw, []byte("initial")) {
		t.Fatal("wrong immutable document source")
	}
	status("GET", "/api/v1/office/source?ticket=invalid", nil, nil, 401)
	address := documents.URL + "/cache/saved.docx"
	cfg, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	signed := func(key string, status int, url string) string {
		t.Helper()
		token, e := cfg.Sign(jwt.MapClaims{"key": key, "status": status, "url": url})
		if e != nil {
			t.Fatal(e)
		}
		return token
	}
	token := signed(docKey, 6, address)
	in := schema.OfficeCallback{Key: docKey, Status: 6, URL: &address, Token: &token}
	callback := func(path string, in schema.OfficeCallback, want int) {
		t.Helper()
		var out schema.OfficeResult
		status("POST", path, in, &out, 200)
		if out.Error != want {
			t.Fatalf("callback error=%d want %d", out.Error, want)
		}
	}
	bad := in
	invalid := "invalid"
	bad.Token = &invalid
	callback(a, bad, 1)
	bad = in
	tampered := documents.URL + "/tampered"
	bad.URL = &tampered
	callback(a, bad, 1)
	if downloads.Load() != 0 {
		t.Fatal("unverified callback fetched data")
	}
	outside := "http://untrusted.invalid/file"
	outsideToken := signed(docKey, 6, outside)
	bad = in
	bad.URL = &outside
	bad.Token = &outsideToken
	callback(a, bad, 1)
	if downloads.Load() != 0 {
		t.Fatal("callback fetched a foreign origin")
	}
	callback(a, in, 0)
	callback(a, in, 0)
	callback(b, in, 1)
	latest, err := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w.ID, ID: f.ID})
	if err != nil || latest.CurrentVersion != 2 {
		t.Fatal("duplicate callback made duplicate version", err)
	}
	original, err := queries.GetFileVersion(ctx, q.GetFileVersionParams{FileID: f.ID, Number: 1})
	if err != nil || original.Checksum != drive.Hash([]byte("initial")) {
		t.Fatal("original overwritten")
	}
	versions, err := queries.ListFileVersions(ctx, f.ID)
	if err != nil || len(versions) != 2 || versions[0].Checksum != drive.Hash([]byte("edited document fixture")) {
		t.Fatal("edited version was not persisted")
	}
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", owner.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", source, nil, nil, 403)
	callback(a, in, 1)
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", owner.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	srv.Clock.Advance(9 * time.Hour)
	status("GET", source, nil, nil, 401)
	callback(a, in, 1)
}
