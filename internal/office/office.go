package office

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"path"
	"strconv"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	provider "thura/internal/providers/onlyoffice"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func authorized(ctx context.Context, queries *q.Queries, s q.OfficeSession, f q.DriveFile) error {
	if !s.ExpiresAt.After(lidza.Now(ctx)) || f.Trashed {
		return router.Errorf(410, "document session expired or file is in trash")
	}
	ok, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: f.WorkspaceID, Subject: s.Subject})
	if err != nil {
		return err
	}
	if !ok {
		return router.Errorf(403, "document access was revoked")
	}
	return nil
}
func Open(ctx context.Context, w, id string) (schema.OfficeConfig, error) {
	f, err := drive.Get(ctx, w, id)
	if err != nil {
		return schema.OfficeConfig{}, err
	}
	if f.Trashed {
		return schema.OfficeConfig{}, router.Errorf(409, "restore the file before editing")
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), "."))
	kind := "word"
	if ext == "xlsx" {
		kind = "cell"
	} else if ext != "docx" {
		return schema.OfficeConfig{}, router.Errorf(422, "editing supports DOCX and XLSX files")
	}
	cfg, err := provider.Load()
	if err != nil {
		return schema.OfficeConfig{}, err
	}
	key := f.ID + ".v" + strconv.Itoa(int(f.CurrentVersion))
	s, err := q.New(db.From(ctx)).CreateOfficeSession(ctx, q.CreateOfficeSessionParams{FileID: id, Subject: auth.CurrentUser(ctx).ID, SourceVersion: f.CurrentVersion, DocumentKey: key, ExpiresAt: lidza.Now(ctx).Add(8 * time.Hour)})
	if err != nil {
		return schema.OfficeConfig{}, err
	}
	ticket, err := cfg.Sign(jwt.MapClaims{"sub": s.ID, "aud": "thura-office-source", "exp": lidza.Now(ctx).Add(10 * time.Minute).Unix()})
	if err != nil {
		return schema.OfficeConfig{}, err
	}
	config := jwt.MapClaims{"exp": s.ExpiresAt.Unix(), "documentType": kind, "document": map[string]any{"fileType": ext, "key": key, "title": f.Name, "url": cfg.AppURL + "/api/v1/office/source?ticket=" + ticket, "permissions": map[string]bool{"edit": true, "download": true, "print": false}}, "editorConfig": map[string]any{"mode": "edit", "callbackUrl": cfg.AppURL + "/api/v1/office/callback/" + s.ID, "user": map[string]string{"id": s.Subject, "name": "Workspace member"}, "customization": map[string]any{"forcesave": true}}, "type": "desktop"}
	token, err := cfg.Sign(config)
	if err != nil {
		return schema.OfficeConfig{}, err
	}
	config["token"] = token
	raw, err := json.Marshal(config)
	return schema.OfficeConfig{ScriptURL: cfg.ServerURL + "/web-apps/apps/api/documents/api.js", Config: raw}, err
}
func Source(w http.ResponseWriter, r *http.Request) {
	cfg, err := provider.Load()
	if err != nil {
		router.Error(w, 503, "Document editing is not configured")
		return
	}
	claims, err := cfg.Verify(r.URL.Query().Get("ticket"))
	if err != nil {
		router.Error(w, 401, "invalid document ticket")
		return
	}
	aud, _ := claims["aud"].(string)
	id, _ := claims["sub"].(string)
	exp, _ := claims["exp"].(float64)
	if aud != "thura-office-source" || !workspace.ValidID(id) || exp <= float64(lidza.Now(r.Context()).Unix()) {
		router.Error(w, 401, "invalid document ticket")
		return
	}
	queries := q.New(db.From(r.Context()))
	s, err := queries.GetOfficeSession(r.Context(), id)
	if err != nil {
		router.Error(w, 404, "document session not found")
		return
	}
	f, err := queries.GetSharedFile(r.Context(), s.FileID)
	if err == nil {
		err = authorized(r.Context(), queries, s, f)
	}
	if err != nil {
		router.Error(w, 403, "document access unavailable")
		return
	}
	content, err := drive.Content(r.Context(), f, int(s.SourceVersion))
	if err != nil {
		router.Error(w, 503, "document source unavailable")
		return
	}
	b, err := base64.StdEncoding.DecodeString(content.Data)
	if err != nil {
		router.Error(w, 503, "document source unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", "attachment")
	if _, err = w.Write(b); err != nil {
		lidza.Log(r.Context()).Error("office source interrupted", "error", err)
	}
}
func Save(ctx context.Context, id string, in schema.OfficeCallback, header string) (schema.OfficeResult, error) {
	fail := schema.OfficeResult{Error: 1}
	if !workspace.ValidID(id) {
		return fail, router.Errorf(404, "document session not found")
	}
	cfg, err := provider.Load()
	if err != nil {
		return fail, err
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if in.Token != nil {
		token = *in.Token
	}
	claims, err := cfg.Verify(token)
	if err != nil {
		return fail, err
	}
	// ONLYOFFICE can put the callback fields at the root or inside payload.
	if p, ok := claims["payload"].(map[string]any); ok {
		claims = jwt.MapClaims(p)
	}
	signedKey, _ := claims["key"].(string)
	signedStatus, _ := claims["status"].(float64)
	signedURL, _ := claims["url"].(string)
	address := ""
	if in.URL != nil {
		address = *in.URL
	}
	if signedKey != in.Key || signedStatus != float64(in.Status) || signedURL != address {
		return fail, router.Errorf(401, "callback fields do not match the signed payload")
	}
	preSession, err := q.New(db.From(ctx)).GetOfficeSession(ctx, id)
	if err != nil {
		return fail, router.Errorf(404, "document session not found")
	}
	preFile, err := q.New(db.From(ctx)).GetSharedFile(ctx, preSession.FileID)
	if err != nil {
		return fail, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return fail, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, preFile.WorkspaceID); err != nil {
		return fail, err
	}
	s, err := queries.LockOfficeSession(ctx, id)
	if err != nil {
		return fail, router.Errorf(404, "document session not found")
	}
	f, err := queries.GetSharedFile(ctx, s.FileID)
	if err != nil {
		return fail, err
	}
	if err = authorized(ctx, queries, s, f); err != nil {
		return fail, err
	}
	if s.DocumentKey != in.Key {
		return fail, router.Errorf(401, "document key mismatch")
	}
	if in.Status != 2 && in.Status != 6 {
		return schema.OfficeResult{}, nil
	}
	b, err := cfg.Download(ctx, address)
	if err != nil {
		return fail, err
	}
	checksum := drive.Hash(b)
	if checksum == s.LastChecksum {
		return schema.OfficeResult{}, nil
	}
	f, err = queries.LockDriveFile(ctx, q.LockDriveFileParams{WorkspaceID: f.WorkspaceID, ID: f.ID})
	if err != nil {
		return fail, err
	}
	if f.Trashed || f.CurrentVersion != s.BaseVersion {
		return fail, router.Errorf(409, "document changed; recover the editor copy before reopening")
	}
	if f.CurrentVersion >= 100 {
		return fail, router.Errorf(409, "file version limit reached; recover the editor copy")
	}
	if err = drive.CheckQuota(ctx, queries, f.WorkspaceID, int64(len(b)), nil); err != nil {
		return fail, err
	}
	key := "drive/files/" + f.ID + "/" + uuid.NewString()
	store := storage.From(ctx)
	if _, err = store.Put(ctx, key, bytes.NewReader(b), storage.PutOptions{ContentType: f.ContentType}); err != nil {
		return fail, err
	}
	f, err = queries.UpdateDriveVersion(ctx, q.UpdateDriveVersionParams{WorkspaceID: f.WorkspaceID, ID: f.ID, Size: int32(len(b))})
	if err == nil {
		_, err = queries.AddFileVersion(ctx, q.AddFileVersionParams{FileID: f.ID, Number: f.CurrentVersion, ObjectKey: key, Checksum: checksum, Size: f.Size, CreatedBy: s.Subject})
	}
	if err == nil {
		err = queries.SaveOfficeSession(ctx, q.SaveOfficeSessionParams{ID: id, BaseVersion: f.CurrentVersion, LastChecksum: checksum})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return fail, errors.Join(err, store.Delete(ctx, key))
	}
	return schema.OfficeResult{}, nil
}

// Callback always uses ONLYOFFICE's response envelope. Save failures return
// error:1 so the service retains its recoverable copy and retries the save.
func Callback(w http.ResponseWriter, r *http.Request) {
	var in schema.OfficeCallback
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(&in); err != nil {
		router.Error(w, 400, "invalid callback")
		return
	}
	result, err := Save(r.Context(), r.PathValue("id"), in, r.Header.Get("Authorization"))
	if err != nil {
		result.Error = 1
		lidza.Log(r.Context()).Error("office save rejected", "error", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if encodeErr := json.NewEncoder(w).Encode(result); encodeErr != nil {
		lidza.Log(r.Context()).Error("office callback response interrupted", "error", encodeErr)
	}
}
