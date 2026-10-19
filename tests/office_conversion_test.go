package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/office"
	"thura/internal/platform/documentprocess"
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
	var documents *httptest.Server
	documents = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/converter" {
			w.Header().Set("Content-Type", "application/json")
			if e := json.NewEncoder(w).Encode(map[string]any{"endConvert": true, "percent": 100, "fileUrl": documents.URL + "/result.docx"}); e != nil {
				t.Error(e)
			}
			return
		}
		if _, e := w.Write(word); e != nil {
			t.Error(e)
		}
	}))
	defer documents.Close()
	t.Setenv("OFFICE_SERVER_URL", documents.URL)
	t.Setenv("OFFICE_APP_URL", "http://app.example")
	t.Setenv("OFFICE_JWT_SECRET", "office-conversion-test-secret-maple-1872936")
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
	if r := srv.JSON(t, "GET", "/api/v1/office/editor/web-apps/apps/api/documents/api.js", nil, nil); r.StatusCode != 401 {
		t.Fatalf("anonymous document proxy: %d", r.StatusCode)
	}
	if r := srv.JSON(t, "POST", apiPath+"/convert", schema.OfficeConversionInput{Target: "docx"}, nil); r.StatusCode != 401 {
		t.Fatalf("anonymous conversion: %d", r.StatusCode)
	}
	converted, err := office.Conversion(ctx, space.ID, f.ID, "docx")
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
	if _, err = office.Conversion(ctx, space.ID, f.ID, "xlsx"); err == nil {
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
	view, viewErr := office.OpenMode(ctx, space.ID, f.ID, "dark", true)
	if viewErr != nil {
		t.Fatal(viewErr)
	}
	if !strings.Contains(string(view.Config), `"mode":"view"`) || !strings.Contains(string(view.Config), `"edit":false`) || !strings.Contains(string(view.Config), `"type":"embedded"`) {
		t.Fatalf("mobile viewer must be signed read-only: %s", view.Config)
	}
	_, err = queries.UpdateDriveFlags(ctx, q.UpdateDriveFlagsParams{WorkspaceID: space.ID, ID: f.ID, Name: "source.doc", Trashed: false})
	if err != nil {
		t.Fatal(err)
	}
	config, err := office.Open(ctx, space.ID, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config.Config), `"mode":"view"`) || !strings.Contains(string(config.Config), `"edit":false`) {
		t.Fatal("legacy document was not read-only")
	}
	before := calls.Load()
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", space.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	if _, err = office.Conversion(ctx, space.ID, f.ID, "docx"); err == nil {
		t.Fatal("revoked member converted a file")
	}
	if calls.Load() != before {
		t.Fatal("denied request reached document service")
	}
}
