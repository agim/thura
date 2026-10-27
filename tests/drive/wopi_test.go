package tests

import (
	"bytes"
	"encoding/base64"
	"github.com/agim/lidza/packs/db"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"thura/internal/drive"
	"thura/internal/platform/documentprocess"
	"thura/internal/wopi"
)

func TestWopiTokensLocksVersionedSavesAndRevocation(t *testing.T) {
	fixture := newWopiFixture(t)
	srv, ctx, queries, f, original := fixture.srv, fixture.ctx, fixture.queries, fixture.f, fixture.original
	editor, e := wopi.Open(ctx, fixture.workspaceID, f.ID, "dark", "https://thura.example", false)
	if e != nil {
		t.Fatal(e)
	}
	if editor.ReadOnly || !strings.HasPrefix(editor.EditorURL, "/api/v1/collabora/browser/") {
		t.Fatal("invalid editable configuration")
	}
	// Use the real mounted Go routes, not a direct handler with forged context.
	send := func(method, suffix, token, operation, lock string, data []byte) *http.Response {
		t.Helper()
		request, e := http.NewRequest(method, srv.URL+"/api/v1/wopi/files/"+f.ID+suffix+"?access_token="+url.QueryEscape(token), bytes.NewReader(data))
		if e != nil {
			t.Fatal(e)
		}
		request.Header.Set("X-WOPI-Override", operation)
		request.Header.Set("X-WOPI-Lock", lock)
		response, e := srv.Client().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if e := response.Body.Close(); e != nil {
				t.Error(e)
			}
		})
		return response
	}
	token := editor.AccessToken
	if r := send("GET", "", "bad", "", "", nil); r.StatusCode != 401 {
		t.Fatalf("invalid token: %d", r.StatusCode)
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", original); r.StatusCode != 409 || r.Header.Get("X-WOPI-Lock") != "" {
		t.Fatalf("unlocked write: %d", r.StatusCode)
	}
	if r := send("POST", "", token, "LOCK", "writer-lock", nil); r.StatusCode != 200 {
		t.Fatalf("lock: %d", r.StatusCode)
	}
	if r := send("POST", "", token, "LOCK", "other-lock", nil); r.StatusCode != 409 || r.Header.Get("X-WOPI-Lock") != "writer-lock" {
		t.Fatalf("conflicting lock: %d", r.StatusCode)
	}
	readOnly, e := wopi.Open(ctx, fixture.workspaceID, f.ID, "light", "https://thura.example", true)
	if e != nil {
		t.Fatal(e)
	}
	if r := send("POST", "/contents", readOnly.AccessToken, "PUT", "writer-lock", original); r.StatusCode != 403 {
		t.Fatalf("view-only write: %d", r.StatusCode)
	}
	changed, e := documentprocess.Word("Saved from Collabora")
	if e != nil {
		t.Fatal(e)
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", changed); r.StatusCode != 200 || r.Header.Get("X-WOPI-ItemVersion") != "2" {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("save: %d %s", r.StatusCode, b)
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", changed); r.StatusCode != 200 || r.Header.Get("X-WOPI-ItemVersion") != "2" {
		t.Fatal("duplicate save created another version")
	}
	f, e = queries.GetSharedFile(ctx, f.ID)
	if e != nil {
		t.Fatal(e)
	}
	content, e := drive.Content(ctx, f, 2)
	if e != nil {
		t.Fatal(e)
	}
	b, e := base64.StdEncoding.DecodeString(content.Data)
	if e != nil || !bytes.Equal(b, changed) {
		t.Fatal("saved bytes lost")
	}
	content, e = drive.Content(ctx, f, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e = base64.StdEncoding.DecodeString(content.Data)
	if e != nil || !bytes.Equal(b, original) {
		t.Fatal("original version overwritten")
	}
	// A concurrent version change cannot be overwritten even with the right lock.
	if _, e = db.From(ctx).Exec(ctx, "UPDATE drive_file SET current_version=3 WHERE id=$1", f.ID); e != nil {
		t.Fatal(e)
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", changed); r.StatusCode != 409 {
		t.Fatal("stale base version overwritten")
	}
	if _, e = db.From(ctx).Exec(ctx, "UPDATE drive_file SET current_version=2 WHERE id=$1", f.ID); e != nil {
		t.Fatal(e)
	}
	if r := send("POST", "", token, "REFRESH_LOCK", "writer-lock", nil); r.StatusCode != 200 {
		t.Fatal("lock refresh failed")
	}
	if r := send("POST", "", token, "GET_LOCK", "", nil); r.StatusCode != 200 || r.Header.Get("X-WOPI-Lock") != "writer-lock" {
		t.Fatal("current lock not returned")
	}
	if r := send("POST", "", token, "UNLOCK", "wrong-lock", nil); r.StatusCode != 409 {
		t.Fatal("wrong owner unlocked file")
	}
	if r := send("POST", "", token, "UNLOCK", "writer-lock", nil); r.StatusCode != 200 {
		t.Fatal("owner could not unlock")
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", changed); r.StatusCode != 409 {
		t.Fatal("unlocked save accepted")
	}
	if _, e = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", fixture.workspaceID, fixture.userID); e != nil {
		t.Fatal(e)
	}
	if r := send("GET", "/contents", token, "", "", nil); r.StatusCode != 403 {
		t.Fatalf("revoked read: %d", r.StatusCode)
	}
	if r := send("POST", "/contents", token, "PUT", "writer-lock", changed); r.StatusCode != 403 {
		t.Fatalf("revoked save: %d", r.StatusCode)
	}
}

func TestWopiGetFileNeverRendersOnTheAppOrigin(t *testing.T) {
	fixture := newWopiFixture(t)
	srv, ctx, f := fixture.srv, fixture.ctx, fixture.f
	// The stored type is whatever the uploader claimed; it must not decide how a browser treats the bytes.
	if _, e := db.From(ctx).Exec(ctx, "UPDATE drive_file SET content_type='text/html' WHERE id=$1", f.ID); e != nil {
		t.Fatal(e)
	}
	editor, e := wopi.Open(ctx, fixture.workspaceID, f.ID, "dark", "https://thura.example", false)
	if e != nil {
		t.Fatal(e)
	}
	get := func(headers map[string]string) *http.Response {
		t.Helper()
		request, e := http.NewRequest("GET", srv.URL+"/api/v1/wopi/files/"+f.ID+"/contents?access_token="+url.QueryEscape(editor.AccessToken), nil)
		if e != nil {
			t.Fatal(e)
		}
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response, e := srv.Client().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if e := response.Body.Close(); e != nil {
				t.Error(e)
			}
		})
		return response
	}
	// Collabora's server-side fetch carries no Sec-Fetch metadata and gets the bytes.
	r := get(nil)
	if r.StatusCode != 200 {
		t.Fatalf("plain fetch: %d", r.StatusCode)
	}
	body, e := io.ReadAll(r.Body)
	if e != nil || !bytes.Equal(body, fixture.original) {
		t.Fatal("document bytes changed")
	}
	for name, want := range map[string]string{"Content-Type": "application/octet-stream", "Content-Disposition": "attachment; filename=Plan.docx", "Content-Security-Policy": "sandbox; default-src 'none'", "X-Content-Type-Options": "nosniff", "X-WOPI-ItemVersion": "1", "Cache-Control": "no-store"} {
		if got := r.Header.Get(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	// A browser address-bar or link navigation is refused even with a valid token.
	if r := get(map[string]string{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Site": "same-origin"}); r.StatusCode != 404 {
		t.Fatalf("navigation: %d", r.StatusCode)
	}
	if r := get(map[string]string{"Sec-Fetch-Mode": "navigate"}); r.StatusCode != 404 {
		t.Fatalf("navigate mode: %d", r.StatusCode)
	}
	if r := get(map[string]string{"Sec-Fetch-Dest": "document"}); r.StatusCode != 404 {
		t.Fatalf("document destination: %d", r.StatusCode)
	}
	// A same-origin script fetch still gets only an inert download.
	if r := get(map[string]string{"Sec-Fetch-Dest": "empty", "Sec-Fetch-Mode": "cors", "Sec-Fetch-Site": "same-origin"}); r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("script fetch: %d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
}
