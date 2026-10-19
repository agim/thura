package tests

import (
	"bytes"
	"context"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	lidzatest "github.com/agim/lidza/pkg/lidzatest"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/documentprocess"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

type wopiFixture struct {
	srv                        *lidzatest.Server
	ctx                        context.Context
	queries                    *q.Queries
	f                          q.DriveFile
	original                   []byte
	userID, email, workspaceID string
}

func newWopiFixture(t *testing.T) wopiFixture {
	t.Helper()
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if _, e := io.WriteString(w, `<wopi-discovery><net-zone><app><action ext="docx" name="edit" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/><action ext="docx" name="view" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/><action ext="pdf" name="view_comment" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/><action ext="doc" name="view" urlsrc="https://office.example/api/v1/collabora/browser/test/cool.html?"/></app></net-zone></wopi-discovery>`); e != nil {
			t.Error(e)
		}
	}))
	t.Cleanup(service.Close)
	t.Setenv("COLLABORA_SERVER_URL", service.URL)
	t.Setenv("COLLABORA_APP_URL", "http://app.example")
	t.Setenv("COLLABORA_TOKEN_SECRET", "test-collabora-file-token-secret-maple-730192")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	user, e := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("wopi-%d@example.com", time.Now().UnixNano()), "Writer", "test waterfall forest lantern 7823")
	if e != nil {
		t.Fatal(e)
	}
	space, e := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "WOPI", OwnerEmail: user.Email})
	if e != nil {
		t.Fatal(e)
	}
	ctx = auth.WithUser(ctx, &auth.User{ID: user.Subject})
	queries := q.New(db.From(ctx))
	original, e := documentprocess.Word("Original text")
	if e != nil {
		t.Fatal(e)
	}
	f, e := queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: space.ID, Name: "Plan.docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Size: int32(len(original))})
	if e != nil {
		t.Fatal(e)
	}
	key := "wopi-test/" + f.ID
	if _, e = storage.From(ctx).Put(ctx, key, bytes.NewReader(original), storage.PutOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: 1, ObjectKey: key, Checksum: drive.Hash(original), Size: int32(len(original)), CreatedBy: user.Subject}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		versions, e := queries.ListFileVersions(cleanup, f.ID)
		if e != nil {
			t.Error(e)
		}
		for _, v := range versions {
			if e = storage.From(cleanup).Delete(cleanup, v.ObjectKey); e != nil {
				t.Error(e)
			}
		}
		for _, statement := range []string{"DELETE FROM wopi_lock WHERE file_id=$1", "DELETE FROM office_session WHERE file_id=$1", "DELETE FROM file_version WHERE file_id=$1", "DELETE FROM drive_file WHERE id=$1"} {
			if _, e = db.From(cleanup).Exec(cleanup, statement, f.ID); e != nil {
				t.Error(e)
			}
		}
		for _, statement := range []string{"DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e = db.From(cleanup).Exec(cleanup, statement, space.ID); e != nil {
				t.Error(e)
			}
		}
		if e = auth.From(cleanup).DeleteUser(cleanup, user.Subject); e != nil {
			t.Error(e)
		}
	})
	return wopiFixture{srv, ctx, queries, f, original, user.Subject, user.Email, space.ID}
}
