package office

import (
	"context"
	"encoding/base64"
	"github.com/agim/lidza/pkg/router"
	"path"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/platform/documentprocess"
	"thura/schema"
)

const WordType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

func Converted(ctx context.Context, w string, f q.DriveFile, target string, data []byte) (schema.OfficeConversion, error) {
	out := schema.OfficeConversion{Name: strings.TrimSuffix(f.Name, path.Ext(f.Name)) + " (editable)." + target, ContentType: WordType}
	if target == "xlsx" {
		out.ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if target == "pptx" {
		out.ContentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	// Verify the result is an Office archive rather than a mislabeled error page.
	if len(data) < 4 || string(data[:4]) != "PK\x03\x04" {
		return out, router.Errorf(502, "The document service did not return a valid Office file")
	}
	current, err := drive.Get(ctx, w, f.ID)
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
