package drive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"unicode/utf8"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/platform/objectgc"
	"thura/internal/platform/safeimage"
	"thura/schema"
)

const PreviewJob = "thura.drive.preview"
const previewTextLimit = 64 << 10

type previewPayload struct {
	WorkspaceID string `json:"workspaceId"`
	FileID      string `json:"fileId"`
	Version     int32  `json:"version"`
}

type previewBytes struct {
	data          []byte
	contentType   string
	width, height int
	truncated     bool
}

// Never serve the source document as an embedded preview. Images are decoded
// within a pixel budget and re-encoded; text is UTF-8 and rendered as text.
func buildPreview(source []byte, contentType string) (previewBytes, bool) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return previewBytes{}, false
	}
	if mediaType == "text/plain" {
		if !utf8.Valid(source) || bytes.ContainsRune(source, '\x00') {
			return previewBytes{}, false
		}
		n := min(len(source), previewTextLimit)
		for n > 0 && !utf8.Valid(source[:n]) {
			n--
		}
		return previewBytes{data: source[:n], contentType: "text/plain", truncated: n < len(source)}, true
	}
	result, supported := safeimage.Convert(source, mediaType)
	return previewBytes{data: result.Data, contentType: "image/png", width: result.Width, height: result.Height}, supported
}

func previewView(p q.DrivePreview) schema.FilePreview {
	return schema.FilePreview{FileID: p.FileID, Version: int(p.Version), Status: p.Status, ContentType: p.ContentType, Width: int(p.Width), Height: int(p.Height), Truncated: p.Truncated}
}

func Preview(ctx context.Context, w, id string) (schema.FilePreview, error) {
	f, err := Get(ctx, w, id)
	if err != nil {
		return schema.FilePreview{}, err
	}
	if f.Trashed {
		return schema.FilePreview{}, router.Errorf(410, "file is in trash")
	}
	out := schema.FilePreview{FileID: id, Version: int(f.CurrentVersion), Status: "missing"}
	p, err := q.New(db.From(ctx)).GetDrivePreview(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if p.Version != f.CurrentVersion {
		return out, nil
	}
	out = previewView(p)
	if p.Status == "pending" && p.JobID != "" {
		job, err := jobs.From(ctx).Get(ctx, p.JobID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if job == nil || job.State == "failed" || job.State == "done" {
			out.Status = "failed"
		}
	}
	if p.Status != "ready" {
		return out, nil
	}
	b, err := read(ctx, p.ObjectKey)
	if err != nil {
		return out, err
	}
	if Hash(b) != p.Checksum {
		return out, errors.New("preview checksum mismatch")
	}
	out.Data = base64.StdEncoding.EncodeToString(b)
	return out, nil
}

func RequestPreview(ctx context.Context, w, id string) (schema.FilePreview, error) {
	if _, err := Get(ctx, w, id); err != nil {
		return schema.FilePreview{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.FilePreview{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.FilePreview{}, err
	}
	// Recheck membership inside the same workspace lock as access changes.
	if _, err = queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: auth.CurrentUser(ctx).ID}); errors.Is(err, pgx.ErrNoRows) {
		return schema.FilePreview{}, router.Errorf(404, "workspace not found")
	} else if err != nil {
		return schema.FilePreview{}, err
	}
	f, err := queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: w, ID: id})
	if err != nil {
		return schema.FilePreview{}, err
	}
	if f.Trashed {
		return schema.FilePreview{}, router.Errorf(410, "file is in trash")
	}
	previous, err := queries.LockDrivePreview(ctx, id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return schema.FilePreview{}, err
	}
	if err == nil && previous.Version == f.CurrentVersion {
		if previous.Status == "ready" || previous.Status == "unavailable" {
			return previewView(previous), nil
		}
		if previous.JobID != "" {
			job, err := jobs.From(ctx).Get(ctx, previous.JobID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return schema.FilePreview{}, err
			}
			if job != nil && (job.State == "pending" || job.State == "running") {
				return previewView(previous), nil
			}
		}
	}
	if previous.ObjectKey != "" {
		if err = objectgc.QueueDelete(ctx, tx, previous.ObjectKey); err != nil {
			return schema.FilePreview{}, err
		}
	}
	p, err := queries.ResetDrivePreview(ctx, q.ResetDrivePreviewParams{FileID: id, Version: f.CurrentVersion})
	if err != nil {
		return schema.FilePreview{}, err
	}
	jobID, err := jobs.From(ctx).EnqueueTx(ctx, tx, PreviewJob, previewPayload{WorkspaceID: w, FileID: id, Version: f.CurrentVersion})
	if err != nil {
		return schema.FilePreview{}, err
	}
	if err = queries.SetDrivePreviewJob(ctx, q.SetDrivePreviewJobParams{FileID: id, JobID: jobID}); err != nil {
		return schema.FilePreview{}, err
	}
	return previewView(p), tx.Commit(ctx)
}

func GeneratePreview(ctx context.Context, raw json.RawMessage) error {
	var in previewPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if uuid.Validate(in.WorkspaceID) != nil || uuid.Validate(in.FileID) != nil || in.Version < 1 {
		return fmt.Errorf("invalid preview job")
	}
	queries := q.New(db.From(ctx))
	f, err := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: in.WorkspaceID, ID: in.FileID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.Trashed || f.CurrentVersion != in.Version {
		return nil
	}
	v, err := queries.GetFileVersion(ctx, q.GetFileVersionParams{FileID: in.FileID, Number: in.Version})
	if err != nil {
		return err
	}
	b, err := read(ctx, v.ObjectKey)
	if err != nil {
		return err
	}
	if Hash(b) != v.Checksum {
		return errors.New("preview source checksum mismatch")
	}
	result, supported := buildPreview(b, f.ContentType)
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	if _, err = queries.LockWorkspace(ctx, in.WorkspaceID); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	f, err = queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: in.WorkspaceID, ID: in.FileID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := queries.LockDrivePreview(ctx, in.FileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.Trashed || f.CurrentVersion != in.Version || p.Version != in.Version || p.Status != "pending" {
		return nil
	}
	completed := q.CompleteDrivePreviewParams{FileID: in.FileID, Version: in.Version, Status: "unavailable"}
	if supported {
		completed.Status = "ready"
		completed.ObjectKey = "drive/previews/" + in.FileID + "/" + uuid.NewString()
		completed.Checksum = Hash(result.data)
		completed.ContentType = result.contentType
		completed.Width, completed.Height, completed.Truncated = int32(result.width), int32(result.height), result.truncated
		if _, err = storage.From(ctx).Put(ctx, completed.ObjectKey, bytes.NewReader(result.data), storage.PutOptions{ContentType: result.contentType}); err != nil {
			return err
		}
	}
	err = queries.CompleteDrivePreview(ctx, completed)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil && completed.ObjectKey != "" {
		return errors.Join(err, storage.From(ctx).Delete(ctx, completed.ObjectKey))
	}
	return err
}
