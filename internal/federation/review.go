package federation

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/smip"
	"thura/internal/workspace"
	"thura/schema"
)

func bindingView(r q.SmipBinding) schema.SmipBinding {
	return schema.SmipBinding{ID: r.ID, WorkspaceID: r.WorkspaceID, Peer: r.Peer, Stream: r.Stream, Sender: r.Sender, Recipient: r.Recipient, Enabled: r.Enabled, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}
func ListBindings(ctx context.Context, w string) (schema.SmipBindingList, error) {
	out := schema.SmipBindingList{Items: []schema.SmipBinding{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	out.Configured = From(ctx).domain != ""
	rows, err := q.New(db.From(ctx)).ListSmipBindings(ctx, w)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		out.Items = append(out.Items, bindingView(row))
	}
	return out, nil
}

// lockAccess serializes consent/import against workspace membership changes.
func lockAccess(ctx context.Context, w string, manager bool) (pgx.Tx, *q.Queries, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return nil, nil, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err == nil {
		var role string
		role, err = queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: auth.CurrentUser(ctx).ID})
		if errors.Is(err, pgx.ErrNoRows) {
			err = router.Errorf(404, "workspace not found")
		}
		if err == nil && (role != "owner" && role != "admin" && (manager || role != "member")) {
			err = router.Errorf(403, "workspace permission required")
		}
	}
	if err != nil {
		return nil, nil, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, nil
}
func CreateBinding(ctx context.Context, w string, in schema.SmipBindingInput) (schema.SmipBinding, error) {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return schema.SmipBinding{}, err
	}
	defer tx.Rollback(ctx)
	g := From(ctx)
	if g.domain == "" {
		return schema.SmipBinding{}, router.Errorf(503, "SMIP is disabled")
	}
	if err = in.Validate(); err != nil {
		return schema.SmipBinding{}, err
	}
	if _, err = uuid.Parse(in.RequestID); err != nil {
		return schema.SmipBinding{}, router.Errorf(422, "request ID required")
	}
	// Reuse the wire validator for identity, recipient and stream alphabets.
	e := smip.Envelope{Version: smip.Version, ID: in.RequestID, From: in.Peer, To: g.domain, Sender: in.Sender, Recipient: in.Recipient, Stream: in.Stream, Kind: "chat", Created: 1000, Expires: 2000}
	// The public key type is Ed25519; Sign fills the fingerprint before validation.
	_, err = smip.Sign(e, g.signer.Private)
	if err != nil || len(g.peers[in.Peer]) == 0 {
		return schema.SmipBinding{}, router.Errorf(422, "choose a configured peer and valid SMIP identities")
	}
	old, err := queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: w, ID: in.RequestID})
	if err == nil {
		if old.Peer != in.Peer || old.Stream != in.Stream || old.Sender != in.Sender || old.Recipient != in.Recipient {
			return schema.SmipBinding{}, router.Errorf(409, "request ID already used")
		}
		return bindingView(old), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return schema.SmipBinding{}, err
	}
	count, err := queries.CountSmipBindings(ctx, w)
	if err != nil {
		return schema.SmipBinding{}, err
	}
	if count >= 50 {
		return schema.SmipBinding{}, router.Errorf(409, "SMIP binding limit reached")
	}
	row, err := queries.AddSmipBinding(ctx, q.AddSmipBindingParams{ID: in.RequestID, WorkspaceID: w, Peer: in.Peer, Stream: in.Stream, Sender: in.Sender, Recipient: in.Recipient, CreatedBy: auth.CurrentUser(ctx).ID})
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return schema.SmipBinding{}, router.Errorf(409, "SMIP stream is already bound")
		}
		return schema.SmipBinding{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.binding.create", Scope: w, Resource: "smip-binding/" + row.ID}); err != nil {
		return schema.SmipBinding{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipBinding{}, err
	}
	return bindingView(row), nil
}
func DisableBinding(ctx context.Context, w, id string) error {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if !workspace.ValidID(id) {
		return router.Errorf(404, "binding not found")
	}
	if _, err = queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: w, ID: id}); errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "binding not found")
	}
	if err != nil {
		return err
	}
	if err = queries.DisableSmipBinding(ctx, q.DisableSmipBindingParams{WorkspaceID: w, ID: id}); err != nil {
		return err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.binding.disable", Scope: w, Resource: "smip-binding/" + id}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func item(row q.ListSmipInboxRow) schema.SmipInboxItem {
	return schema.SmipInboxItem{ID: row.ID, BindingID: row.BindingID, Origin: row.Origin, MessageID: row.MessageID, Sender: row.Sender, Recipient: row.Recipient, Stream: row.Stream, Kind: row.Kind, Body: row.Body, Name: row.Name, Size: int(row.Size), Digest: row.Digest, AcceptedAt: row.AcceptedAt, ImportedFileID: row.ImportedFileID, ImportedAt: row.ImportedAt}
}
func ListInbox(ctx context.Context, w, cursor string) (schema.SmipInboxList, error) {
	out := schema.SmipInboxList{Items: []schema.SmipInboxItem{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	if cursor != "" && !workspace.ValidID(cursor) {
		return out, router.Errorf(422, "invalid inbox cursor")
	}
	rows, err := q.New(db.From(ctx)).ListSmipInbox(ctx, q.ListSmipInboxParams{WorkspaceID: w, ID: cursor})
	if err != nil {
		return out, err
	}
	if len(rows) > 50 {
		out.NextCursor = rows[49].ID
		rows = rows[:50]
	}
	for _, row := range rows {
		out.Items = append(out.Items, item(row))
	}
	return out, nil
}
func ImportFile(ctx context.Context, w, id string, in schema.SmipImportInput) (schema.DriveFile, error) {
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return schema.DriveFile{}, err
	}
	defer tx.Rollback(ctx)
	if !workspace.ValidID(id) {
		return schema.DriveFile{}, router.Errorf(404, "transfer not found")
	}
	row, err := queries.LockSmipInbox(ctx, q.LockSmipInboxParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.DriveFile{}, router.Errorf(404, "transfer not found")
	}
	if err != nil {
		return schema.DriveFile{}, err
	}
	if row.ImportedFileID != nil {
		file, err := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w, ID: *row.ImportedFileID})
		if errors.Is(err, pgx.ErrNoRows) || err == nil && file.Trashed {
			return schema.DriveFile{}, router.Errorf(410, "imported file was removed; refusing duplicate import")
		}
		return drive.File(file), err
	}
	if row.Kind != "file" {
		return schema.DriveFile{}, router.Errorf(422, "transfer is not a file")
	}
	record, err := decode(row)
	if err != nil {
		return schema.DriveFile{}, err
	}
	data, err := record.Packet.Envelope.Bytes()
	if err != nil {
		return schema.DriveFile{}, err
	}
	nameBytes, err := base64.RawURLEncoding.DecodeString(record.Packet.Envelope.Name)
	if err != nil {
		return schema.DriveFile{}, err
	}
	name, err := drive.Name(string(nameBytes))
	if err != nil || name != string(nameBytes) || len(data) > drive.MaxSize {
		return schema.DriveFile{}, router.Errorf(422, "file exceeds Drive policy")
	}
	if err = drive.CheckFolder(ctx, queries, w, in.FolderID); err != nil {
		return schema.DriveFile{}, err
	}
	if err = drive.CheckQuota(ctx, queries, w, int64(len(data)), nil); err != nil {
		return schema.DriveFile{}, err
	}
	count, err := queries.CountWorkspaceFiles(ctx, w)
	if err != nil {
		return schema.DriveFile{}, err
	}
	pending, err := queries.CountPendingNewFiles(ctx, w)
	if err != nil {
		return schema.DriveFile{}, err
	}
	if count+pending >= 500 {
		return schema.DriveFile{}, router.Errorf(409, "workspace file limit reached")
	}
	file, err := queries.CreateDriveFile(ctx, q.CreateDriveFileParams{WorkspaceID: w, FolderID: in.FolderID, Name: name, ContentType: "application/octet-stream", Size: int32(len(data))})
	if err != nil {
		return schema.DriveFile{}, err
	}
	key := "drive/files/" + file.ID + "/" + uuid.NewString()
	if _, err = storage.From(ctx).Put(ctx, key, bytes.NewReader(data), storage.PutOptions{ContentType: "application/octet-stream"}); err != nil {
		return schema.DriveFile{}, err
	}
	if _, err = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: file.ID, Number: file.CurrentVersion, ObjectKey: key, Checksum: drive.Hash(data), Size: file.Size, CreatedBy: auth.CurrentUser(ctx).ID}); err != nil {
		return schema.DriveFile{}, err
	}
	if err = queries.ImportSmipFile(ctx, q.ImportSmipFileParams{WorkspaceID: w, ID: id, ImportedFileID: &file.ID, ImportedAt: timePointer(lidza.Now(ctx))}); err != nil {
		return schema.DriveFile{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.file.import", Scope: w, Resource: "drive-file/" + file.ID}); err != nil {
		return schema.DriveFile{}, err
	}
	// A commit error may be ambiguous. Never delete an object immediately here:
	// ordinary reference-aware Drive sweeping reclaims only proven old orphans.
	if err = tx.Commit(ctx); err != nil {
		return schema.DriveFile{}, err
	}
	return drive.File(file), nil
}
func timePointer(t time.Time) *time.Time { return &t }
