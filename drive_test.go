package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"net/http"
	"testing"
	"thura/internal/drive"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestDriveResumeVersionsAndShareRevocation(t *testing.T) {
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("drive-%d@example.com", time.Now().UnixNano())
	pw := "maple meadow waterfall lantern 7593"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	otherEmail := fmt.Sprintf("drive-other-%d@example.com", time.Now().UnixNano())
	outsider, err := auth.From(ctx).CreateUser(ctx, otherEmail, "Outsider", pw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auth.From(ctx).DeleteUser(ctx, outsider.Subject) })
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Drive test", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM upload_chunk WHERE session_id IN (SELECT id FROM upload_session WHERE workspace_id=$1)", "DELETE FROM upload_session WHERE workspace_id=$1", "DELETE FROM share_grant WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM file_version WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM drive_folder WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, in, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s got %d want %d", method, path, r.StatusCode, want)
		}
	}
	base := "/api/v1/workspaces/" + w.ID + "/drive"
	status("GET", base, nil, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	chunk := func(id string, n int, b []byte, want int) {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+base+"/uploads/"+id+fmt.Sprintf("/chunks/%d", n), bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("chunk got %d want %d", res.StatusCode, want)
		}
	}
	var state schema.UploadState
	data := bytes.Repeat([]byte("a"), drive.ChunkSize+7)
	status("POST", base+"/uploads", schema.UploadInput{Name: "private.txt", ContentType: "text/plain", Size: len(data)}, &state, 200)
	id := state.Session.ID
	status("POST", base+"/uploads/"+id+"/finish", nil, nil, 409)
	chunk(id, 1, data[drive.ChunkSize:], 204)
	chunk(id, 0, data[:drive.ChunkSize], 204)
	chunk(id, 0, data[:drive.ChunkSize], 204)
	status("GET", base+"/uploads/"+id, nil, &state, 200)
	if len(state.Chunks) != 2 || state.Chunks[0] != 0 || state.Chunks[1] != 1 {
		t.Fatal("resumed chunks missing")
	}
	var file schema.DriveFile
	status("POST", base+"/uploads/"+id+"/finish", nil, &file, 200)
	fileID := file.ID
	status("POST", base+"/uploads/"+id+"/finish", nil, &file, 200)
	if file.ID != fileID || file.CurrentVersion != 1 {
		t.Fatal("finish was not idempotent")
	}
	chunk(id, 1, data[drive.ChunkSize:], 409)
	var content schema.FileContent
	status("GET", base+"/files/"+file.ID+"/content", nil, &content, 200)
	decoded, _ := base64.StdEncoding.DecodeString(content.Data)
	if !bytes.Equal(decoded, data) {
		t.Fatal("corrupt downloaded bytes")
	}
	replacement := schema.UploadInput{Name: file.Name, ContentType: file.ContentType, Size: 3, FileID: &file.ID, BaseVersion: 1}
	var a, b schema.UploadState
	status("POST", base+"/uploads", replacement, &a, 200)
	status("POST", base+"/uploads", replacement, &b, 200)
	chunk(a.Session.ID, 0, []byte("new"), 204)
	chunk(b.Session.ID, 0, []byte("old"), 204)
	status("POST", base+"/uploads/"+a.Session.ID+"/finish", nil, &file, 200)
	status("POST", base+"/uploads/"+b.Session.ID+"/finish", nil, nil, 409)
	status("GET", base+"/files/"+file.ID+"/content?version=1", nil, &content, 200)
	decoded, _ = base64.StdEncoding.DecodeString(content.Data)
	if !bytes.Equal(decoded, data) {
		t.Fatal("immutable original version overwritten")
	}
	var share schema.ShareCreated
	status("POST", base+"/files/"+file.ID+"/shares", schema.ShareInput{ExpiresAt: srv.Clock.Now().Add(time.Hour)}, &share, 200)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("GET", base+"/files/"+file.ID+"/content", nil, nil, 401)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, &content, 200)
	if content.Data != base64.StdEncoding.EncodeToString([]byte("new")) {
		t.Fatal("share wrong version")
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	status("PATCH", base+"/files/"+file.ID, schema.DriveFlags{Trashed: true}, nil, 200)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 410)
	status("PATCH", base+"/files/"+file.ID, schema.DriveFlags{Trashed: false}, nil, 200)
	status("DELETE", base+"/files/"+file.ID+"/shares/"+share.Grant.ID, nil, nil, 204)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 410)
	var targeted schema.ShareCreated
	status("POST", base+"/files/"+file.ID+"/shares", schema.ShareInput{TargetEmail: &email, ExpiresAt: srv.Clock.Now().Add(time.Hour)}, &targeted, 200)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: targeted.Token}, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: targeted.Token}, nil, 200)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: otherEmail, Password: pw}, nil, 200)
	status("GET", base, nil, nil, 404)
	status("GET", base+"/files/"+file.ID+"/content", nil, nil, 404)
	status("GET", base+"/uploads/"+id, nil, nil, 404)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: targeted.Token}, nil, 403)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	srv.Clock.Advance(2 * time.Hour)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: targeted.Token}, nil, 410)
}
