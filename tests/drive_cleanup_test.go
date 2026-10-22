package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/workspace"
	"thura/schema"
)

// Every editor open leaves an office_session row; the hourly cleanup prunes
// the expired ones, and a purge removes the file's label assignments.
func TestDriveCleanupPrunesOfficeSessionsAndPurgeDropsLabels(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("drive-cleanup-%d@example.com", time.Now().UnixNano())
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", "cedar lantern river meadow 4471")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Drive cleanup", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	queries := q.New(pool)
	f, err := queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: w.ID, Name: "Plan.docx", ContentType: "application/octet-stream", Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		for _, sql := range []string{"DELETE FROM office_session WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM resource_label WHERE label_id IN (SELECT id FROM mail_label WHERE workspace_id=$1)", "DELETE FROM mail_label WHERE workspace_id=$1", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM audit_event WHERE scope=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := pool.Exec(cleanup, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		if e := auth.From(cleanup).DeleteUser(cleanup, owner.Subject); e != nil {
			t.Error(e)
		}
	})
	now := lidza.Now(ctx)
	var stale string
	for n, expires := range []time.Time{now.Add(-48 * time.Hour), now.Add(-time.Hour), now.Add(8 * time.Hour)} {
		s, e := queries.CreateOfficeSession(ctx, q.CreateOfficeSessionParams{FileID: f.ID, Subject: owner.Subject, SourceVersion: 1, DocumentKey: "collabora:" + f.ID, ExpiresAt: expires})
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			stale = s.ID
		}
	}
	if err = drive.Cleanup(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM office_session WHERE file_id=$1", f.ID).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("cleanup left %d office sessions (%v); want the live one and the one inside the grace period", remaining, err)
	}
	if _, err = queries.GetOfficeSession(ctx, stale); err == nil {
		t.Fatal("cleanup kept the long-expired office session")
	}
	var label string
	if err = pool.QueryRow(ctx, "INSERT INTO mail_label(workspace_id,name) VALUES($1,'Purged') RETURNING id", w.ID).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO resource_label(label_id,source,source_id) VALUES($1,'document',$2)", label, f.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE drive_file SET trashed=true WHERE id=$1", f.ID); err != nil {
		t.Fatal(err)
	}
	if err = drive.Purge(auth.WithUser(ctx, &auth.User{ID: owner.Subject}), w.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM resource_label WHERE source='document' AND source_id=$1", f.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("purge left %d label assignments (%v)", remaining, err)
	}
}
