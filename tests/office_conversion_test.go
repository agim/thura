package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/office"
	"thura/internal/platform/documentprocess"
	"thura/internal/providers/collabora"
	"thura/internal/wopi"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestOfficeConversionAccessReviewedTextAndOriginalPreservation(t *testing.T) {
	word, err := documentprocess.Word("Converted text: budget $3,500")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/v1/collabora/hosting/discovery" {
			w.Header().Set("Content-Type", "application/xml")
			if _, e := io.WriteString(w, `<wopi-discovery><net-zone><app><action ext="pdf" name="view_comment" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/><action ext="doc" name="view" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/></app></net-zone></wopi-discovery>`); e != nil {
				t.Error(e)
			}
			return
		}
		if r.URL.Path != "/api/v1/collabora/cool/convert-to/docx" {
			t.Error("unexpected conversion endpoint")
			w.WriteHeader(404)
			return
		}
		source, _, e := r.FormFile("data")
		if e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		defer source.Close()
		b, e := io.ReadAll(source)
		if e != nil || string(b) != "%PDF-1.7" {
			t.Error("authorized source bytes lost")
		}
		if _, e = w.Write(word); e != nil {
			t.Error(e)
		}
	}))
	defer documents.Close()
	t.Setenv("COLLABORA_SERVER_URL", documents.URL)
	t.Setenv("COLLABORA_APP_URL", "http://app.example")
	t.Setenv("COLLABORA_TOKEN_SECRET", "collabora-conversion-test-secret-maple-1872936")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("convert-%d@example.com", time.Now().UnixNano())
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", "test waterfall forest lantern 7823")
	if err != nil {
		t.Fatal(err)
	}
	space, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Conversions", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	ctx = auth.WithUser(ctx, &auth.User{ID: owner.Subject})
	queries := q.New(db.From(ctx))
	f, err := queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: space.ID, Name: "source.pdf", ContentType: "application/pdf", Size: 8})
	if err != nil {
		t.Fatal(err)
	}
	key := "conversion-test/" + f.ID
	source := []byte("%PDF-1.7")
	if _, err = storage.From(ctx).Put(ctx, key, bytes.NewReader(source), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: 1, ObjectKey: key, Checksum: drive.Hash(source), Size: 8, CreatedBy: owner.Subject}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		for _, sql := range []string{"DELETE FROM office_session WHERE file_id=$1", "DELETE FROM file_version WHERE file_id=$1", "DELETE FROM drive_file WHERE id=$1"} {
			if _, e := db.From(cleanup).Exec(cleanup, sql, f.ID); e != nil {
				t.Error(e)
			}
		}
		for _, sql := range []string{"DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := db.From(cleanup).Exec(cleanup, sql, space.ID); e != nil {
				t.Error(e)
			}
		}
		if e := storage.From(cleanup).Delete(cleanup, key); e != nil {
			t.Error(e)
		}
		if e := auth.From(cleanup).DeleteUser(cleanup, owner.Subject); e != nil {
			t.Error(e)
		}
	})
	apiPath := "/api/v1/workspaces/" + space.ID + "/drive/files/" + f.ID
	if r := srv.JSON(t, "GET", "/api/v1/collabora/browser/test/cool.html", nil, nil); r.StatusCode != 401 {
		t.Fatalf("anonymous document proxy: %d", r.StatusCode)
	}
	if r := srv.JSON(t, "POST", apiPath+"/convert", schema.OfficeConversionInput{Target: "docx"}, nil); r.StatusCode != 401 {
		t.Fatalf("anonymous conversion: %d", r.StatusCode)
	}
	converted, err := wopi.Convert(ctx, space.ID, f.ID, "docx")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(converted.Data)
	if err != nil {
		t.Fatal(err)
	}
	if converted.Status != "ready" || converted.Name != "source (editable).docx" || !bytes.Equal(decoded, word) {
		t.Fatal("converted bytes, name or format lost")
	}
	if _, err = wopi.Convert(ctx, space.ID, f.ID, "xlsx"); err == nil {
		t.Fatal("unsupported PDF spreadsheet conversion accepted")
	}
	reviewed, err := office.ReviewedWord(ctx, space.ID, f.ID, schema.WordCopyInput{SourceVersion: 1, Text: "Recognized: 12 switches, $1,500"})
	if err != nil || reviewed.ContentType != office.WordType || reviewed.Data == "" {
		t.Fatalf("reviewed Word: %+v %v", reviewed, err)
	}
	if _, err = office.ReviewedWord(ctx, space.ID, f.ID, schema.WordCopyInput{SourceVersion: 2, Text: "stale"}); err == nil {
		t.Fatal("stale source version accepted")
	}
	current, err := drive.Get(ctx, space.ID, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CurrentVersion != 1 || current.Name != "source.pdf" || current.ContentType != "application/pdf" {
		t.Fatal("conversion mutated original")
	}
	original, err := drive.Content(ctx, current, 1)
	if err != nil {
		t.Fatal(err)
	}
	if original.Data != base64.StdEncoding.EncodeToString(source) {
		t.Fatal("original bytes overwritten")
	}
	view, viewErr := wopi.Open(ctx, space.ID, f.ID, "dark", "https://app.example", true)
	if viewErr != nil {
		t.Fatal(viewErr)
	}
	viewerURL, e := url.Parse(view.EditorURL)
	if e != nil || viewerURL.Query().Get("ui_defaults") != "UITheme=light" {
		t.Fatal("PDF must retain readable light paper")
	}
	if !view.ReadOnly || view.AccessToken == "" || view.EditorURL == "" {
		t.Fatal("mobile PDF viewer must use signed read-only configuration")
	}
	cfg, err := collabora.Load()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := cfg.Verify(view.AccessToken)
	if err != nil || claims["ro"] != true {
		t.Fatal("read-only grant not enforced")
	}

	_, err = queries.UpdateDriveFlags(ctx, q.UpdateDriveFlagsParams{WorkspaceID: space.ID, ID: f.ID, Name: "source.doc", Trashed: false})
	if err != nil {
		t.Fatal(err)
	}
	config, err := wopi.Open(ctx, space.ID, f.ID, "light", "https://app.example", false)
	if err != nil {
		t.Fatal(err)
	}
	if !config.ReadOnly {
		t.Fatal("view-only discovery action became writable")
	}

	before := calls.Load()
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", space.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	if _, err = wopi.Convert(ctx, space.ID, f.ID, "docx"); err == nil {
		t.Fatal("revoked member converted a file")
	}
	if calls.Load() != before {
		t.Fatal("denied request reached document service")
	}
}
