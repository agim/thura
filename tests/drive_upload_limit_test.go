package tests

import (
	"fmt"
	"testing"
	"thura/app"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"thura/internal/drive"
	"thura/internal/workspace"
	"thura/schema"
)

// One member cannot hold the whole workspace quota with abandoned upload sessions.
func TestDriveMemberPendingUploadLimit(t *testing.T) {
	t.Setenv("STORAGE_DIR", t.TempDir())
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("drive-limit-%d@example.com", time.Now().UnixNano())
	pw := "maple meadow waterfall lantern 7593"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Drive upload limit", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM upload_chunk WHERE session_id IN (SELECT id FROM upload_session WHERE workspace_id=$1)", "DELETE FROM upload_session WHERE workspace_id=$1", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	if r := srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil); r.StatusCode != 200 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	base := "/api/v1/workspaces/" + w.ID + "/drive"
	var state schema.UploadState
	for i := 0; i < drive.MaxMemberUploads; i++ {
		if r := srv.JSON(t, "POST", base+"/uploads", schema.UploadInput{Name: fmt.Sprintf("pending-%d.txt", i), ContentType: "text/plain", Size: 16}, &state); r.StatusCode != 200 {
			t.Fatalf("upload %d: got %d", i, r.StatusCode)
		}
	}
	if r := srv.JSON(t, "POST", base+"/uploads", schema.UploadInput{Name: "one-too-many.txt", ContentType: "text/plain", Size: 16}, &state); r.StatusCode != 429 {
		t.Fatalf("upload beyond the member limit: got %d want 429", r.StatusCode)
	}
	if _, e := db.From(ctx).Exec(ctx, "UPDATE upload_session SET expires_at=now()-interval '1 minute' WHERE workspace_id=$1", w.ID); e != nil {
		t.Fatal(e)
	}
	if r := srv.JSON(t, "POST", base+"/uploads", schema.UploadInput{Name: "after-expiry.txt", ContentType: "text/plain", Size: 16}, &state); r.StatusCode != 200 {
		t.Fatalf("upload after expiry: got %d want 200", r.StatusCode)
	}
}
