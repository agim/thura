package office

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"path"
	"strconv"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/documentprocess"
	provider "thura/internal/providers/onlyoffice"
	"thura/schema"
	"time"
)

const WordType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

func Conversion(ctx context.Context, w, id, target string) (schema.OfficeConversion, error) {
	f, err := drive.Get(ctx, w, id)
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	if f.Trashed {
		return schema.OfficeConversion{}, router.Errorf(410, "Restore the file before converting it")
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), "."))
	_, allowed, _ := Format(ext)
	if allowed == "" || target != allowed {
		return schema.OfficeConversion{}, router.Errorf(422, "Choose the supported editable format for this file")
	}
	cfg, err := provider.Load()
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	queries := q.New(db.From(ctx))
	subject := auth.CurrentUser(ctx).ID
	session, err := queries.FindOfficeSourceSession(ctx, q.FindOfficeSourceSessionParams{FileID: id, Subject: subject, SourceVersion: f.CurrentVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		session, err = queries.CreateOfficeSession(ctx, q.CreateOfficeSessionParams{FileID: id, Subject: subject, SourceVersion: f.CurrentVersion, DocumentKey: id + ".v" + strconv.Itoa(int(f.CurrentVersion)), ExpiresAt: lidza.Now(ctx).Add(8 * time.Hour)})
	}
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	ticket, err := cfg.Sign(jwt.MapClaims{"sub": session.ID, "aud": "thura-office-source", "exp": lidza.Now(ctx).Add(10 * time.Minute).Unix()})
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := cfg.Convert(call, ext, target, session.DocumentKey+".convert."+target, cfg.AppURL+"/api/v1/office/source?ticket="+ticket)
	if err != nil {
		return schema.OfficeConversion{}, router.Errorf(502, "The document service could not convert this file. For a scanned PDF, use Extract scanned PDF")
	}
	out := schema.OfficeConversion{Status: "pending", Percent: result.Percent, Name: strings.TrimSuffix(f.Name, path.Ext(f.Name)) + " (editable)." + target, ContentType: WordType}
	if target == "xlsx" {
		out.ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if target == "pptx" {
		out.ContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	if !result.End {
		return out, nil
	}
	data, err := cfg.Download(call, result.URL)
	if err != nil {
		return out, router.Errorf(502, "Converted file could not be downloaded from the document service")
	}
	// Verify the result is an Office archive rather than a mislabeled error page.
	if len(data) < 4 || string(data[:4]) != "PK\x03\x04" {
		return out, router.Errorf(502, "The document service did not return a valid Office file")
	}
	current, err := drive.Get(ctx, w, id)
	if err != nil {
		return out, err
	}
	if current.Trashed || current.CurrentVersion != f.CurrentVersion {
		return out, router.Errorf(409, "Source file changed during conversion. Start again with the current version")
	}
	out.Status = "ready"
	out.Percent = 100
	out.Data = base64.StdEncoding.EncodeToString(data)
	return out, nil
}

func ReviewedWord(ctx context.Context, w, id string, in schema.WordCopyInput) (schema.OfficeConversion, error) {
	f, err := drive.Get(ctx, w, id)
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	if f.Trashed || int(f.CurrentVersion) != in.SourceVersion {
		return schema.OfficeConversion{}, router.Errorf(409, "Source file changed. Review the current version first")
	}
	if strings.ToLower(path.Ext(f.Name)) != ".pdf" {
		return schema.OfficeConversion{}, router.Errorf(422, "Word reconstruction requires a PDF")
	}
	data, err := documentprocess.Word(in.Text)
	if err != nil {
		return schema.OfficeConversion{}, router.Errorf(422, "Add recognized text within the document limit")
	}
	return schema.OfficeConversion{Status: "ready", Percent: 100, Name: strings.TrimSuffix(f.Name, path.Ext(f.Name)) + " (reconstructed).docx", ContentType: WordType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}
