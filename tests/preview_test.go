package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/objectgc"
	"thura/internal/workspace"
	"thura/schema"
)

func TestPrivateDrivePreviewVersionsRevocationAndCleanup(t *testing.T) {
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	stamp := time.Now().UnixNano()
	email, outsiderEmail := fmt.Sprintf("preview-%d@example.com", stamp), fmt.Sprintf("preview-other-%d@example.com", stamp)
	pw := "river lantern maple meadows 7593"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := auth.From(ctx).CreateUser(ctx, outsiderEmail, "Outsider", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Private preview test", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			"DELETE FROM drive_preview WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)",
			"DELETE FROM upload_chunk WHERE session_id IN (SELECT id FROM upload_session WHERE workspace_id=$1)",
			"DELETE FROM upload_session WHERE workspace_id=$1", "DELETE FROM file_version WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)",
			"DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1",
		} {
			if _, e := db.From(ctx).Exec(ctx, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
		auth.From(ctx).DeleteUser(ctx, outsider.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		if r := srv.JSON(t, method, path, in, out); r.StatusCode != want {
			t.Fatalf("%s %s got %d want %d", method, path, r.StatusCode, want)
		}
	}
	base := "/api/v1/workspaces/" + w.ID + "/drive"
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	upload := func(data []byte, file *schema.DriveFile) schema.DriveFile {
		t.Helper()
		in := schema.UploadInput{Name: "preview.png", ContentType: "image/png", Size: len(data)}
		if file != nil {
			in.FileID, in.BaseVersion = &file.ID, file.CurrentVersion
		}
		var state schema.UploadState
		status("POST", base+"/uploads", in, &state, 200)
		req, _ := http.NewRequest("PUT", srv.URL+base+"/uploads/"+state.Session.ID+"/chunks/0", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 204 {
			t.Fatalf("upload chunk: %d", res.StatusCode)
		}
		var out schema.DriveFile
		status("POST", base+"/uploads/"+state.Session.ID+"/finish", nil, &out, 200)
		return out
	}
	var source bytes.Buffer
	if err = png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, 400, 200))); err != nil {
		t.Fatal(err)
	}
	f := upload(source.Bytes(), nil)
	path := base + "/files/" + f.ID + "/preview"
	var preview schema.FilePreview
	status("GET", path, nil, &preview, 200)
	if preview.Status != "missing" {
		t.Fatal("GET unexpectedly queued a preview")
	}
	status("POST", path, nil, &preview, 200)
	if preview.Status != "pending" {
		t.Fatal("preview was not queued")
	}
	status("POST", path, nil, &preview, 200)
	var jobCount int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM job WHERE kind=$1 AND payload->>'fileId'=$2", drive.PreviewJob, f.ID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatal("duplicate preview request queued duplicate work")
	}
	raw, _ := json.Marshal(map[string]any{"workspaceId": w.ID, "fileId": f.ID, "version": 1})
	if err = drive.GeneratePreview(ctx, raw); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, &preview, 200)
	if preview.Status != "ready" || preview.Width != 256 || preview.Height != 128 {
		t.Fatalf("wrong preview: %+v", preview)
	}
	decoded, err := base64.StdEncoding.DecodeString(preview.Data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = png.Decode(bytes.NewReader(decoded)); err != nil {
		t.Fatal(err)
	}
	stored, err := q.New(db.From(ctx)).GetDrivePreview(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	deleteRaw, _ := json.Marshal(map[string]string{"key": stored.ObjectKey})
	if err = objectgc.Delete(ctx, deleteRaw); err == nil {
		t.Fatal("cleanup deleted a referenced preview")
	}
	if _, err = storage.From(ctx).Put(ctx, stored.ObjectKey, bytes.NewReader([]byte("corrupt")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, nil, 500)
	if _, err = storage.From(ctx).Put(ctx, stored.ObjectKey, bytes.NewReader(decoded), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	status("PATCH", base+"/files/"+f.ID, schema.DriveFlags{Trashed: true}, nil, 200)
	status("GET", path, nil, nil, 410)
	status("POST", path, nil, nil, 410)
	status("PATCH", base+"/files/"+f.ID, schema.DriveFlags{}, nil, 200)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("GET", path, nil, nil, 401)
	status("POST", path, nil, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: outsiderEmail, Password: pw}, nil, 200)
	status("GET", path, nil, nil, 404)
	status("POST", path, nil, nil, 404)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	status("GET", "/api/v1/workspaces/00000000-0000-4000-8000-000000000000/drive/files/"+f.ID+"/preview", nil, nil, 404)
	// A queued job for an older immutable version cannot attach to a new one.
	f = upload(source.Bytes(), &f)
	status("GET", path, nil, &preview, 200)
	if preview.Status != "missing" || preview.Data != "" {
		t.Fatal("served obsolete-version preview")
	}
	status("POST", path, nil, &preview, 200)
	if err = drive.GeneratePreview(ctx, raw); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, &preview, 200)
	if preview.Status != "pending" || preview.Version != 2 {
		t.Fatal("stale worker changed the new preview")
	}
	var payload json.RawMessage
	if err = db.From(ctx).QueryRow(ctx, "SELECT payload FROM job WHERE id=(SELECT job_id::uuid FROM drive_preview WHERE file_id=$1)", f.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "UPDATE job SET state='failed' WHERE id=(SELECT job_id::uuid FROM drive_preview WHERE file_id=$1)", f.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, &preview, 200)
	if preview.Status != "failed" {
		t.Fatal("exhausted retry was not visible")
	}
	status("POST", path, nil, &preview, 200)
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM job WHERE id=(SELECT job_id::uuid FROM drive_preview WHERE file_id=$1)", f.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, &preview, 200)
	if preview.Status != "failed" {
		t.Fatal("missing queued job was not recoverable")
	}
	status("POST", path, nil, &preview, 200)
	if err = drive.GeneratePreview(ctx, payload); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, &preview, 200)
	if preview.Status != "ready" || preview.Version != 2 {
		t.Fatal("preview retry did not recover")
	}
	if err = objectgc.Delete(ctx, deleteRaw); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, stored.ObjectKey); err == nil {
		t.Fatal("old preview object retained after replacement cleanup")
	}
	stored, err = q.New(db.From(ctx)).GetDrivePreview(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	// An existing session loses access when its membership is removed.
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", w.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	status("GET", path, nil, nil, 404)
	status("POST", path, nil, nil, 404)
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", owner.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("PATCH", base+"/files/"+f.ID, schema.DriveFlags{Trashed: true}, nil, 200)
	status("DELETE", base+"/files/"+f.ID, nil, nil, 204)
	status("GET", path, nil, nil, 404)
	deleteRaw, _ = json.Marshal(map[string]string{"key": stored.ObjectKey})
	if err = objectgc.Delete(ctx, deleteRaw); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, stored.ObjectKey); err == nil {
		t.Fatal("purged preview object remains")
	}
}
