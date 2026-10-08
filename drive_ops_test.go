package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/lidzatest"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/objectgc"
	"thura/internal/workspace"
	"thura/schema"
)

func TestDriveQuotaReservationPurgeAndCleanup(t *testing.T) {
	t.Setenv("DRIVE_QUOTA_BYTES", "1048576")
	t.Setenv("STORAGE_DIR", t.TempDir())
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("drive-ops-%d@example.com", time.Now().UnixNano())
	pw := "maple meadow waterfall lantern 7593"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Drive operations", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM upload_chunk WHERE session_id IN (SELECT id FROM upload_session WHERE workspace_id=$1)", "DELETE FROM upload_session WHERE workspace_id=$1", "DELETE FROM share_grant WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM office_session WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM file_version WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		if r := srv.JSON(t, method, path, in, out); r.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, r.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID + "/drive"
	var states [2]schema.UploadState
	var codes [2]int
	var wg sync.WaitGroup
	for i := range states {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = srv.JSON(t, "POST", base+"/uploads", schema.UploadInput{Name: "quota.txt", ContentType: "text/plain", Size: 600 * 1024}, &states[i]).StatusCode
		}(i)
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 409) || (codes[1] == 200 && codes[0] == 409)) {
		t.Fatalf("concurrent reservations: %v", codes)
	}
	var state schema.UploadState
	for i, c := range codes {
		if c == 200 {
			state = states[i]
		}
	}
	var quota schema.DriveQuota
	status("GET", base+"/quota", nil, &quota, 200)
	if quota.Retained != 0 || quota.Reserved != 600*1024 || quota.Limit != 1<<20 {
		t.Fatalf("reservation not counted: %+v", quota)
	}
	data := bytes.Repeat([]byte("q"), 600*1024)
	chunk := func() {
		t.Helper()
		req, _ := http.NewRequest("PUT", srv.URL+base+"/uploads/"+state.Session.ID+"/chunks/0", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != 204 {
			t.Fatalf("chunk: %d", res.StatusCode)
		}
	}
	chunk()
	queries := q.New(db.From(ctx))
	before, err := queries.ListUploadChunks(ctx, state.Session.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("chunks: %v %v", before, err)
	}
	chunk()
	after, err := queries.ListUploadChunks(ctx, state.Session.ID)
	if err != nil || len(after) != 1 || before[0].ObjectKey != after[0].ObjectKey {
		t.Fatal("identical chunk retry created another object")
	}
	var file schema.DriveFile
	status("POST", base+"/uploads/"+state.Session.ID+"/finish", nil, &file, 200)
	status("GET", base+"/quota", nil, &quota, 200)
	if quota.Retained != len(data) || quota.Reserved != 0 {
		t.Fatalf("final quota: %+v", quota)
	}
	chunks, err := queries.ListUploadChunks(ctx, state.Session.ID)
	if err != nil || len(chunks) != 0 {
		t.Fatal("finished chunks retained")
	}
	var retry schema.DriveFile
	status("POST", base+"/uploads/"+state.Session.ID+"/finish", nil, &retry, 200)
	if retry.ID != file.ID || retry.CurrentVersion != 1 {
		t.Fatal("finish retry changed version")
	}
	status("POST", base+"/uploads", schema.UploadInput{Name: "too-big", ContentType: "text/plain", Size: 500 * 1024}, nil, 409)
	versions, err := queries.ListFileVersions(ctx, file.ID)
	if err != nil || len(versions) != 1 {
		t.Fatal("missing immutable version")
	}
	retained := versions[0].ObjectKey
	orphan := "drive/files/" + file.ID + "/00000000-0000-4000-8000-000000000001"
	if _, err = storage.From(ctx).Put(ctx, orphan, bytes.NewReader([]byte("orphan")), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	prefix := "drive/files/" + file.ID + "/"
	if err = objectgc.SweepPrefix(ctx, prefix, srv.Clock.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, orphan); err != nil {
		t.Fatal("cleanup deleted a recent object")
	}
	if err = objectgc.SweepPrefix(ctx, prefix, srv.Clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, orphan); err == nil {
		t.Fatal("cleanup kept an old orphan")
	}
	if _, err = storage.From(ctx).Stat(ctx, retained); err != nil {
		t.Fatal("cleanup deleted a referenced version")
	}
	payload, _ := json.Marshal(map[string]string{"key": retained})
	if err = objectgc.Delete(ctx, payload); err == nil {
		t.Fatal("delete accepted a referenced object")
	}
	if err = objectgc.SweepPrefix(ctx, "mail/", srv.Clock.Now().Add(time.Hour)); err == nil {
		t.Fatal("cleanup escaped Drive namespace")
	}
	status("DELETE", base+"/files/"+file.ID, nil, nil, 409)
	var share schema.ShareCreated
	status("POST", base+"/files/"+file.ID+"/shares", schema.ShareInput{ExpiresAt: srv.Clock.Now().Add(time.Hour)}, &share, 200)
	status("PATCH", base+"/files/"+file.ID, schema.DriveFlags{Trashed: true}, nil, 200)
	if _, err = db.From(ctx).Exec(ctx, "UPDATE auth_member SET role='member' WHERE scope=$1 AND subject=$2", w.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	status("DELETE", base+"/files/"+file.ID, nil, nil, 403)
	if _, err = db.From(ctx).Exec(ctx, "UPDATE auth_member SET role='owner' WHERE scope=$1 AND subject=$2", w.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	status("DELETE", base+"/files/"+file.ID, nil, nil, 204)
	status("GET", base+"/files/"+file.ID+"/content", nil, nil, 404)
	status("POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 404)
	status("GET", base+"/quota", nil, &quota, 200)
	if quota.Retained != 0 || quota.Reserved != 0 {
		t.Fatalf("purge did not release quota: %+v", quota)
	}
	var expired schema.UploadState
	status("POST", base+"/uploads", schema.UploadInput{Name: "expired", ContentType: "text/plain", Size: 1}, &expired, 200)
	if _, err = db.From(ctx).Exec(ctx, "UPDATE upload_session SET expires_at=$2 WHERE id=$1", expired.Session.ID, srv.Clock.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = drive.Cleanup(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.From(ctx).QueryRow(ctx, "SELECT COUNT(*) FROM upload_session WHERE id=$1", expired.Session.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired metadata remains: %d %v", count, err)
	}
	status("GET", base+"/uploads/"+expired.Session.ID, nil, nil, 404)
}
