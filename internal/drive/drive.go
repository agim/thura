package drive

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/platform/objectgc"
	"thura/internal/workspace"
	"thura/schema"
)

const ChunkSize = 1 << 20
const MaxSize = 10 << 20

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Name(s string) (string, error) {
	s = path.Base(strings.ReplaceAll(strings.TrimSpace(s), "\\", "/"))
	if s == "." || s == "/" || s == "" || strings.ContainsAny(s, "\x00\r\n") || len(s) > 200 {
		return "", router.Errorf(422, "invalid file name")
	}
	return s, nil
}
func File(v q.DriveFile) schema.DriveFile {
	return schema.DriveFile{ID: v.ID, WorkspaceID: v.WorkspaceID, FolderID: v.FolderID, Name: v.Name, ContentType: v.ContentType, Size: int(v.Size), CurrentVersion: int(v.CurrentVersion), Trashed: v.Trashed, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}
}
func Folder(v q.DriveFolder) schema.DriveFolder {
	return schema.DriveFolder{ID: v.ID, WorkspaceID: v.WorkspaceID, ParentID: v.ParentID, Name: v.Name, CreatedAt: v.CreatedAt}
}
func Session(v q.UploadSession) schema.UploadSession {
	return schema.UploadSession{ID: v.ID, WorkspaceID: v.WorkspaceID, Subject: v.Subject, FileID: v.FileID, FolderID: v.FolderID, Name: v.Name, ContentType: v.ContentType, ExpectedSize: int(v.ExpectedSize), BaseVersion: int(v.BaseVersion), ExpiresAt: v.ExpiresAt, CompletedAt: v.CompletedAt, CreatedAt: v.CreatedAt}
}
func Share(v q.ShareGrant) schema.ShareView {
	return schema.ShareView{ID: v.ID, TargetEmail: v.TargetEmail, ExpiresAt: v.ExpiresAt, RevokedAt: v.RevokedAt}
}
func Get(ctx context.Context, w, id string) (q.DriveFile, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return q.DriveFile{}, err
	}
	if !workspace.ValidID(id) {
		return q.DriveFile{}, router.Errorf(404, "file not found")
	}
	f, err := q.New(db.From(ctx)).GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "file not found")
	}
	return f, err
}
func CheckFolder(ctx context.Context, queries *q.Queries, w string, id *string) error {
	if id == nil {
		return nil
	}
	if !workspace.ValidID(*id) {
		return router.Errorf(404, "folder not found")
	}
	_, err := queries.GetDriveFolder(ctx, q.GetDriveFolderParams{WorkspaceID: w, ID: *id})
	if errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "folder not found")
	}
	return err
}
func Begin(ctx context.Context, w string, in schema.UploadInput) (schema.UploadState, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.UploadState{}, err
	}
	name, err := Name(in.Name)
	if err != nil {
		return schema.UploadState{}, err
	}
	if err = in.Validate(); err != nil {
		return schema.UploadState{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.UploadState{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.UploadState{}, err
	}
	if err = CheckQuota(ctx, queries, w, int64(in.Size), nil); err != nil {
		return schema.UploadState{}, err
	}
	activeCount, err := queries.CountWorkspaceUploads(ctx, w)
	if err != nil {
		return schema.UploadState{}, err
	}
	if activeCount >= 100 {
		return schema.UploadState{}, router.Errorf(409, "at most 100 pending uploads per workspace")
	}
	if in.FileID == nil {
		count, xerr := queries.CountWorkspaceFiles(ctx, w)
		if xerr != nil {
			return schema.UploadState{}, xerr
		}
		pending, xerr := queries.CountPendingNewFiles(ctx, w)
		if xerr != nil {
			return schema.UploadState{}, xerr
		}
		if count+pending >= 500 {
			return schema.UploadState{}, router.Errorf(409, "workspace file limit reached")
		}
	}
	if err = CheckFolder(ctx, queries, w, in.FolderID); err != nil {
		return schema.UploadState{}, err
	}
	if in.FileID != nil {
		f, e := Get(ctx, w, *in.FileID)
		if e != nil {
			return schema.UploadState{}, e
		}
		if f.Trashed || int(f.CurrentVersion) != in.BaseVersion {
			return schema.UploadState{}, router.Errorf(409, "file changed or is in trash")
		}
	}
	u, err := queries.CreateUpload(ctx, q.CreateUploadParams{WorkspaceID: w, Subject: auth.CurrentUser(ctx).ID, FileID: in.FileID, FolderID: in.FolderID, Name: name, ContentType: in.ContentType, ExpectedSize: int32(in.Size), BaseVersion: int32(in.BaseVersion), ExpiresAt: lidza.Now(ctx).Add(24 * time.Hour)})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return schema.UploadState{Session: Session(u), Chunks: []int{}}, err
}
func Upload(ctx context.Context, w, id string) (q.UploadSession, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return q.UploadSession{}, err
	}
	if !workspace.ValidID(id) {
		return q.UploadSession{}, router.Errorf(404, "upload not found")
	}
	u, err := q.New(db.From(ctx)).GetUpload(ctx, q.GetUploadParams{WorkspaceID: w, ID: id, Subject: auth.CurrentUser(ctx).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "upload not found")
	}
	return u, err
}
func active(ctx context.Context, u q.UploadSession) error {
	if u.CompletedAt != nil {
		return router.Errorf(409, "upload completed")
	}
	if !u.ExpiresAt.After(lidza.Now(ctx)) {
		return router.Errorf(410, "upload expired")
	}
	return nil
}
func lockUpload(ctx context.Context, w, id string) (pgx.Tx, *q.Queries, q.UploadSession, error) {
	if _, err := Upload(ctx, w, id); err != nil {
		return nil, nil, q.UploadSession{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, q.UploadSession{}, err
	}
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return nil, nil, q.UploadSession{}, errors.Join(err, tx.Rollback(ctx))
	}
	u, err := queries.LockUpload(ctx, q.LockUploadParams{WorkspaceID: w, ID: id, Subject: auth.CurrentUser(ctx).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "upload not found")
	}
	if err != nil {
		return nil, nil, u, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, u, nil
}
func PutChunk(ctx context.Context, w, id string, n int, b []byte) error {
	tx, queries, u, err := lockUpload(ctx, w, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = active(ctx, u); err != nil {
		return err
	}
	count := (int(u.ExpectedSize) + ChunkSize - 1) / ChunkSize
	expected := ChunkSize
	if n == count-1 {
		expected = int(u.ExpectedSize) - n*ChunkSize
	}
	if n < 0 || n >= count || len(b) != expected {
		return router.Errorf(422, "wrong chunk number or size")
	}
	previous, xerr := queries.GetUploadChunk(ctx, q.GetUploadChunkParams{SessionID: id, Number: int32(n)})
	if xerr != nil && !errors.Is(xerr, pgx.ErrNoRows) {
		return xerr
	}
	if xerr == nil && previous.Checksum == Hash(b) {
		if _, staterr := storage.From(ctx).Stat(ctx, previous.ObjectKey); staterr == nil {
			return nil
		} else if !errors.Is(staterr, storage.ErrNotFound) {
			return staterr
		}
	}
	key := "drive/uploads/" + u.ID + "/" + uuid.NewString()
	store := storage.From(ctx)
	if _, err = store.Put(ctx, key, bytes.NewReader(b), storage.PutOptions{ContentType: "application/octet-stream"}); err != nil {
		return err
	}
	_, err = queries.PutUploadChunk(ctx, q.PutUploadChunkParams{SessionID: id, Number: int32(n), Size: int32(len(b)), Checksum: Hash(b), ObjectKey: key})
	if err == nil && xerr == nil {
		err = objectgc.QueueDelete(ctx, tx, previous.ObjectKey)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		err = errors.Join(err, store.Delete(ctx, key))
	}
	return err
}
func read(ctx context.Context, key string) ([]byte, error) {
	r, _, err := storage.From(ctx).Get(ctx, key)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	err = errors.Join(err, r.Close())
	if len(b) > MaxSize {
		return nil, router.Errorf(413, "file too large")
	}
	return b, err
}
func Finish(ctx context.Context, w, id string) (schema.DriveFile, error) {
	tx, queries, u, err := lockUpload(ctx, w, id)
	if err != nil {
		return schema.DriveFile{}, err
	}
	defer tx.Rollback(ctx)
	if u.CompletedAt != nil && u.FileID != nil {
		f, e := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w, ID: *u.FileID})
		return File(f), e
	}
	if err = active(ctx, u); err != nil {
		return schema.DriveFile{}, err
	}
	chunks, err := queries.ListUploadChunks(ctx, id)
	if err != nil {
		return schema.DriveFile{}, err
	}
	if len(chunks) != (int(u.ExpectedSize)+ChunkSize-1)/ChunkSize {
		return schema.DriveFile{}, router.Errorf(409, "upload has missing chunks")
	}
	var content bytes.Buffer
	for n, c := range chunks {
		if int(c.Number) != n {
			return schema.DriveFile{}, router.Errorf(409, "missing chunk")
		}
		b, e := read(ctx, c.ObjectKey)
		if e != nil {
			return schema.DriveFile{}, e
		}
		if Hash(b) != c.Checksum {
			return schema.DriveFile{}, errors.New("chunk checksum mismatch")
		}
		content.Write(b)
	}
	if content.Len() != int(u.ExpectedSize) {
		return schema.DriveFile{}, router.Errorf(409, "size mismatch")
	}
	if err = CheckQuota(ctx, queries, w, int64(u.ExpectedSize), &u.ID); err != nil {
		return schema.DriveFile{}, err
	}
	var f q.DriveFile
	if u.FileID == nil {
		f, err = queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: w, FolderID: u.FolderID, Name: u.Name, ContentType: u.ContentType, Size: u.ExpectedSize})
	} else {
		f, err = queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: w, ID: *u.FileID})
		if err == nil && (f.Trashed || f.CurrentVersion != u.BaseVersion) {
			return schema.DriveFile{}, router.Errorf(409, "newer version exists or file is in trash")
		}
		if err == nil && f.CurrentVersion >= 100 {
			return schema.DriveFile{}, router.Errorf(409, "file version limit reached")
		}
		if err == nil {
			f, err = queries.UpdateDriveVersion(ctx, q.UpdateDriveVersionParams{WorkspaceID: w, ID: f.ID, Size: u.ExpectedSize})
		}
	}
	if err != nil {
		return schema.DriveFile{}, err
	}
	key := "drive/files/" + f.ID + "/" + uuid.NewString()
	store := storage.From(ctx)
	if _, err = store.Put(ctx, key, bytes.NewReader(content.Bytes()), storage.PutOptions{ContentType: f.ContentType}); err != nil {
		return schema.DriveFile{}, err
	}
	_, err = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: f.CurrentVersion, ObjectKey: key, Checksum: Hash(content.Bytes()), Size: f.Size, CreatedBy: u.Subject})
	if err == nil {
		err = queries.CompleteUpload(ctx, q.CompleteUploadParams{ID: id, FileID: &f.ID})
		if err == nil {
			for _, c := range chunks {
				if err = objectgc.QueueDelete(ctx, tx, c.ObjectKey); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = queries.DeleteUploadChunks(ctx, id)
		}
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return schema.DriveFile{}, errors.Join(err, store.Delete(ctx, key))
	}
	return File(f), nil
}
func Content(ctx context.Context, f q.DriveFile, number int) (schema.FileContent, error) {
	if f.Trashed {
		return schema.FileContent{}, router.Errorf(410, "file is in trash")
	}
	if number == 0 {
		number = int(f.CurrentVersion)
	}
	v, err := q.New(db.From(ctx)).GetFileVersion(ctx, q.GetFileVersionParams{FileID: f.ID, Number: int32(number)})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.FileContent{}, router.Errorf(404, "version not found")
	}
	if err != nil {
		return schema.FileContent{}, err
	}
	b, err := read(ctx, v.ObjectKey)
	if err != nil {
		return schema.FileContent{}, err
	}
	if Hash(b) != v.Checksum {
		return schema.FileContent{}, errors.New("file checksum mismatch")
	}
	return schema.FileContent{Name: f.Name, ContentType: f.ContentType, Data: base64.StdEncoding.EncodeToString(b)}, nil
}
func Grant(ctx context.Context, w, id string, in schema.ShareInput) (schema.ShareCreated, error) {
	f, err := Get(ctx, w, id)
	if err != nil {
		return schema.ShareCreated{}, err
	}
	if f.Trashed {
		return schema.ShareCreated{}, router.Errorf(409, "restore file before sharing")
	}
	if !in.ExpiresAt.After(lidza.Now(ctx)) || in.ExpiresAt.After(lidza.Now(ctx).Add(30*24*time.Hour)) {
		return schema.ShareCreated{}, router.Errorf(422, "share expiry must be within 30 days")
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return schema.ShareCreated{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	target := ""
	if in.TargetEmail != nil {
		target = strings.ToLower(strings.TrimSpace(*in.TargetEmail))
	}
	grant, err := q.New(db.From(ctx)).CreateShareGrant(ctx, q.CreateShareGrantParams{FileID: id, TokenHash: Hash([]byte(token)), TargetEmail: target, ExpiresAt: in.ExpiresAt})
	return schema.ShareCreated{Grant: Share(grant), Token: token}, err
}
func Open(ctx context.Context, token string) (schema.FileContent, error) {
	queries := q.New(db.From(ctx))
	g, err := queries.GetShareGrant(ctx, Hash([]byte(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.FileContent{}, router.Errorf(404, "share not found")
	}
	if err != nil {
		return schema.FileContent{}, err
	}
	if g.RevokedAt != nil || !g.ExpiresAt.After(lidza.Now(ctx)) {
		return schema.FileContent{}, router.Errorf(410, "share expired or revoked")
	}
	if g.TargetEmail != "" {
		u := auth.CurrentUser(ctx)
		if u == nil {
			return schema.FileContent{}, router.Errorf(401, "sign in with the invited email")
		}
		subject, accountErr := queries.FindInvitedAccount(ctx, &g.TargetEmail)
		if accountErr != nil && !errors.Is(accountErr, pgx.ErrNoRows) {
			return schema.FileContent{}, accountErr
		}
		if accountErr != nil || subject != u.ID {
			return schema.FileContent{}, router.Errorf(403, "share belongs to another account")
		}
	}
	f, err := queries.GetSharedFile(ctx, g.FileID)
	if err != nil {
		return schema.FileContent{}, err
	}
	return Content(ctx, f, 0)
}
