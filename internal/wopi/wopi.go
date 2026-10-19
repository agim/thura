// Package wopi implements the file-scoped WOPI operations used by Collabora.
package wopi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/objectstore"
	"thura/internal/providers/collabora"
	"thura/schema"
	"time"
)

func Open(ctx context.Context, w, id, theme, origin string, readOnly bool) (schema.OfficeConfig, error) {
	f, e := drive.Get(ctx, w, id)
	if e != nil {
		return schema.OfficeConfig{}, e
	}
	if f.Trashed {
		return schema.OfficeConfig{}, router.Errorf(410, "Restore the file before opening it")
	}
	cfg, e := collabora.Load()
	if e != nil {
		return schema.OfficeConfig{}, e
	}
	editor, readOnly, e := cfg.Editor(ctx, strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), ".")), f.ContentType, readOnly)
	if e != nil {
		return schema.OfficeConfig{}, e
	}
	expiry := lidza.Now(ctx).Add(8 * time.Hour)
	s, e := q.New(db.From(ctx)).CreateOfficeSession(ctx, q.CreateOfficeSessionParams{FileID: id, Subject: auth.CurrentUser(ctx).ID, SourceVersion: f.CurrentVersion, DocumentKey: "collabora:" + f.ID, ExpiresAt: expiry})
	if e != nil {
		return schema.OfficeConfig{}, e
	}
	token, e := cfg.Sign(jwt.MapClaims{"sub": s.ID, "fid": id, "ro": readOnly, "origin": origin, "aud": "thura-collabora", "exp": expiry.Unix()})
	if e != nil {
		return schema.OfficeConfig{}, e
	}
	// PDF ink is fixed by the source document; CODE's dark paper can make
	// imported black text unreadable. Keep PDF paper in its original light theme.
	if strings.EqualFold(path.Ext(f.Name), ".pdf") {
		theme = "light"
	}
	params := url.Values{"WOPISrc": {cfg.AppURL + "/api/v1/wopi/files/" + id}, "lang": {"en-US"}, "closebutton": {"false"}, "revisionhistory": {"false"}, "ui_defaults": {"UITheme=" + theme}}
	editor += "?" + params.Encode()
	ttl := int(expiry.UnixMilli())
	return schema.OfficeConfig{Provider: "collabora", EditorURL: editor, AccessToken: token, AccessTokenTTL: ttl, ReadOnly: readOnly}, nil
}

type grant struct {
	s      q.OfficeSession
	f      q.DriveFile
	ro     bool
	origin string
}

func authorize(ctx context.Context, id, token string) (grant, error) {
	if len(token) > 8192 {
		return grant{}, router.Errorf(401, "Invalid document access")
	}
	cfg, e := collabora.Load()
	if e != nil {
		return grant{}, e
	}
	c, e := cfg.Verify(token)
	if e != nil {
		return grant{}, router.Errorf(401, "Document access expired. Reopen the file")
	}
	sid, _ := c["sub"].(string)
	fid, _ := c["fid"].(string)
	ro, ok := c["ro"].(bool)
	if !ok || fid != id || !uuidValid(sid) {
		return grant{}, router.Errorf(401, "Invalid document access")
	}
	queries := q.New(db.From(ctx))
	s, e := queries.GetOfficeSession(ctx, sid)
	if e != nil || s.FileID != id || s.DocumentKey != "collabora:"+id || !s.ExpiresAt.After(lidza.Now(ctx)) {
		return grant{}, router.Errorf(401, "Document access expired")
	}
	f, e := queries.GetSharedFile(ctx, id)
	if e != nil {
		return grant{}, router.Errorf(404, "File unavailable")
	}
	member, e := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: f.WorkspaceID, Subject: s.Subject})
	if e != nil {
		return grant{}, e
	}
	if !member || f.Trashed {
		return grant{}, router.Errorf(403, "Document access was revoked")
	}
	origin, _ := c["origin"].(string)
	return grant{s, f, ro, origin}, nil
}
func uuidValid(value string) bool  { _, e := uuid.Parse(value); return e == nil }
func version(f q.DriveFile) string { return strconv.Itoa(int(f.CurrentVersion)) }
func identity(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	g, e := authorize(r.Context(), r.PathValue("id"), r.URL.Query().Get("access_token"))
	if e != nil {
		writeError(w, e)
		return
	}
	ctx := r.Context()
	contents := strings.HasSuffix(r.URL.Path, "/contents")
	if r.Method == "GET" && !contents {
		info := map[string]any{"BaseFileName": g.f.Name, "OwnerId": identity(g.f.WorkspaceID), "Size": g.f.Size, "UserId": identity(g.s.Subject), "UserFriendlyName": "Workspace member", "Version": version(g.f), "UserCanWrite": !g.ro, "ReadOnly": g.ro, "SupportsUpdate": !g.ro, "SupportsLocks": !g.ro, "SupportsGetLock": !g.ro, "SupportsExtendedLockLength": true, "UserCanNotWriteRelative": true, "DisablePrint": true, "DisableExport": true, "HideUserList": "true", "PostMessageOrigin": g.origin, "EnableOwnerTermination": false}
		w.Header().Set("Content-Type", "application/json")
		if e = json.NewEncoder(w).Encode(info); e != nil {
			lidza.Log(ctx).Error("document metadata interrupted", "error", e)
		}
		return
	}
	if r.Method == "GET" && contents {
		data, e := drive.Content(ctx, g.f, 0)
		if e != nil {
			writeError(w, e)
			return
		}
		raw, e := base64.StdEncoding.DecodeString(data.Data)
		if e != nil {
			writeError(w, e)
			return
		}
		w.Header().Set("Content-Type", g.f.ContentType)
		w.Header().Set("X-WOPI-ItemVersion", version(g.f))
		if _, e = w.Write(raw); e != nil {
			lidza.Log(ctx).Error("document read interrupted", "error", e)
		}
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if g.ro {
		router.Error(w, 403, "This document session is view only")
		return
	}
	override := r.Header.Get("X-WOPI-Override")
	if contents && override != "PUT" {
		w.WriteHeader(501)
		return
	}
	if !contents && override != "LOCK" && override != "UNLOCK" && override != "REFRESH_LOCK" && override != "GET_LOCK" {
		w.WriteHeader(501)
		return
	}
	var raw []byte
	if contents {
		raw, e = io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if e != nil || len(raw) == 0 {
			router.Error(w, 413, "Choose a document between 1 byte and 10 MB")
			return
		}
	}
	e = change(ctx, w, r, g, override, raw)
	if e != nil {
		writeError(w, e)
	}
}
func writeError(w http.ResponseWriter, e error) {
	var failure *router.HTTPError
	if errors.As(e, &failure) {
		router.Error(w, failure.Status, failure.Message)
		return
	}
	router.Error(w, 500, "Document operation failed")
}
func change(ctx context.Context, w http.ResponseWriter, r *http.Request, g grant, operation string, raw []byte) error {
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, e = queries.LockWorkspace(ctx, g.f.WorkspaceID); e != nil {
		return e
	}
	f, e := queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: g.f.WorkspaceID, ID: g.f.ID})
	if e != nil {
		return e
	}
	member, e := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: f.WorkspaceID, Subject: g.s.Subject})
	if e != nil {
		return e
	}
	if !member || f.Trashed {
		return router.Errorf(403, "Document access was revoked")
	}
	if e = queries.EnsureWopiLock(ctx, q.EnsureWopiLockParams{FileID: f.ID, BaseVersion: f.CurrentVersion}); e != nil {
		return e
	}
	lock, e := queries.LockWopiLock(ctx, f.ID)
	if e != nil {
		return e
	}
	active := lock.LockID != "" && lock.ExpiresAt.After(lidza.Now(ctx))
	current := ""
	if active {
		current = lock.LockID
	}
	want := r.Header.Get("X-WOPI-Lock")
	old := r.Header.Get("X-WOPI-OldLock")
	conflict := func(reason string) error {
		w.Header().Set("X-WOPI-Lock", current)
		w.Header().Set("X-WOPI-LockFailureReason", reason)
		return router.Errorf(409, "%s", reason)
	}
	if operation == "GET_LOCK" {
		w.Header().Set("X-WOPI-Lock", current)
		return nil
	}
	if len(want) < 1 || len(want) > 1024 {
		return router.Errorf(400, "Invalid document lock")
	}
	if operation == "LOCK" {
		if active && (old != "" && old != current || old == "" && want != current) {
			return conflict("Another editor holds this file")
		}
		base := lock.BaseVersion
		if !active {
			base = f.CurrentVersion
		}
		e = queries.SetWopiLock(ctx, q.SetWopiLockParams{FileID: f.ID, LockID: want, BaseVersion: base, ExpiresAt: lidza.Now(ctx).Add(30 * time.Minute)})
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if !active || current != want {
		return conflict("The document lock changed. Keep a copy and reopen the file")
	}
	if operation == "REFRESH_LOCK" || operation == "UNLOCK" {
		id := current
		expiry := lidza.Now(ctx).Add(30 * time.Minute)
		if operation == "UNLOCK" {
			id = ""
			expiry = lidza.Now(ctx)
		}
		if e = queries.SetWopiLock(ctx, q.SetWopiLockParams{FileID: f.ID, LockID: id, BaseVersion: lock.BaseVersion, ExpiresAt: expiry}); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
	if f.CurrentVersion != lock.BaseVersion {
		return conflict("A newer Drive version exists. Recover your editor copy")
	}
	if f.CurrentVersion >= 100 {
		return router.Errorf(409, "File version limit reached. Save a copy")
	}
	sum := drive.Hash(raw)
	previous, e := queries.GetFileVersion(ctx, q.GetFileVersionParams{FileID: f.ID, Number: f.CurrentVersion})
	if e != nil {
		return e
	}
	if previous.Checksum == sum {
		w.Header().Set("X-WOPI-ItemVersion", version(f))
		return nil
	}
	if e = drive.CheckQuota(ctx, queries, f.WorkspaceID, int64(len(raw)), nil); e != nil {
		return e
	}
	key := "drive/files/" + f.ID + "/" + uuid.NewString()
	store := storage.From(ctx)
	if _, e = objectstore.Put(ctx, key, bytes.NewReader(raw), storage.PutOptions{ContentType: f.ContentType}); e != nil {
		return e
	}
	f, e = queries.UpdateDriveVersion(ctx, q.UpdateDriveVersionParams{WorkspaceID: f.WorkspaceID, ID: f.ID, Size: int32(len(raw))})
	if e == nil {
		_, e = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: f.CurrentVersion, ObjectKey: key, Checksum: sum, Size: f.Size, CreatedBy: g.s.Subject})
	}
	if e == nil {
		e = queries.SetWopiLock(ctx, q.SetWopiLockParams{FileID: f.ID, LockID: current, BaseVersion: f.CurrentVersion, ExpiresAt: lidza.Now(ctx).Add(30 * time.Minute)})
	}
	if e == nil {
		e = tx.Commit(ctx)
	}
	if e != nil {
		return errors.Join(e, store.Delete(ctx, key))
	}
	w.Header().Set("X-WOPI-ItemVersion", version(f))
	return nil
}
