package drive

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/platform/objectgc"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

const CleanupJob = "thura.drive.uploads"

func removeUpload(ctx context.Context, tx pgx.Tx, queries *q.Queries, id string) error {
	chunks, err := queries.ListUploadChunks(ctx, id)
	if err != nil {
		return err
	}
	for _, c := range chunks {
		if err = objectgc.QueueDelete(ctx, tx, c.ObjectKey); err != nil {
			return err
		}
	}
	if err = queries.DeleteUploadChunks(ctx, id); err != nil {
		return err
	}
	return queries.DeleteUploadSession(ctx, id)
}
func Cleanup(ctx context.Context, _ json.RawMessage) error {
	now := lidza.Now(ctx)
	completedCutoff := now.Add(-24 * time.Hour)
	queries := q.New(db.From(ctx))
	rows, err := queries.ListExpiredUploads(ctx, q.ListExpiredUploadsParams{ExpiresAt: now, CompletedAt: &completedCutoff})
	if err != nil {
		return err
	}
	for _, u := range rows {
		if err = cleanupOne(ctx, u, now); err != nil {
			return err
		}
	}
	return nil
}
func cleanupOne(ctx context.Context, u q.UploadSession, now time.Time) error {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, u.WorkspaceID); err != nil {
		return err
	}
	current, err := queries.LockUploadForCleanup(ctx, u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.ExpiresAt.After(now) && (current.CompletedAt == nil || current.CompletedAt.After(now.Add(-24*time.Hour))) {
		return nil
	}
	if err = removeUpload(ctx, tx, queries, u.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func Purge(ctx context.Context, w, id string) error {
	if err := workspace.RequireManager(ctx, w); err != nil {
		return err
	}
	if !workspace.ValidID(id) {
		return router.Errorf(404, "file not found")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return err
	}
	role, err := queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: auth.CurrentUser(ctx).ID})
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return router.Errorf(403, "workspace administrator required")
	}
	f, err := queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "file not found")
	}
	if err != nil {
		return err
	}
	if !f.Trashed {
		return router.Errorf(409, "move the file to trash before permanently deleting it")
	}
	versions, err := queries.ListFileVersions(ctx, id)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if err = objectgc.QueueDelete(ctx, tx, v.ObjectKey); err != nil {
			return err
		}
	}
	uploads, err := queries.ListFileUploads(ctx, &id)
	if err != nil {
		return err
	}
	for _, u := range uploads {
		if err = removeUpload(ctx, tx, queries, u.ID); err != nil {
			return err
		}
	}
	for _, remove := range []func(context.Context, string) error{queries.DeleteFileShares, queries.DeleteFileOfficeSessions, queries.DeleteFileVersions} {
		if err = remove(ctx, id); err != nil {
			return err
		}
	}
	if err = queries.DeleteDriveFile(ctx, q.DeleteDriveFileParams{WorkspaceID: w, ID: id}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func CreateFolder(ctx context.Context, w string, in schema.FolderInput) (schema.DriveFolder, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.DriveFolder{}, err
	}
	name, err := Name(in.Name)
	if err != nil {
		return schema.DriveFolder{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.DriveFolder{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.DriveFolder{}, err
	}
	count, err := queries.CountWorkspaceFolders(ctx, w)
	if err != nil {
		return schema.DriveFolder{}, err
	}
	if count >= 500 {
		return schema.DriveFolder{}, router.Errorf(409, "workspace folder limit reached")
	}
	if err = CheckFolder(ctx, queries, w, in.ParentID); err != nil {
		return schema.DriveFolder{}, err
	}
	f, err := queries.CreateDriveFolder(ctx, q.CreateDriveFolderParams{WorkspaceID: w, ParentID: in.ParentID, Name: name})
	if err != nil {
		return schema.DriveFolder{}, err
	}
	return Folder(f), tx.Commit(ctx)
}
