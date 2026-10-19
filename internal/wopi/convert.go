package wopi

import (
	"context"
	"encoding/base64"
	"github.com/agim/lidza/pkg/router"
	"path"
	"strings"
	"thura/internal/drive"
	"thura/internal/office"
	"thura/internal/providers/collabora"
	"thura/schema"
)

func Convert(ctx context.Context, w, id, target string) (schema.OfficeConversion, error) {
	f, err := drive.Get(ctx, w, id)
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	if f.Trashed {
		return schema.OfficeConversion{}, router.Errorf(410, "Restore the file before converting it")
	}
	_, allowed, _ := office.Format(strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), ".")))
	if allowed == "" || target != allowed {
		return schema.OfficeConversion{}, router.Errorf(422, "Choose the supported editable format for this file")
	}
	source, err := drive.Content(ctx, f, int(f.CurrentVersion))
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	data, err := base64.StdEncoding.DecodeString(source.Data)
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	config, err := collabora.Load()
	if err != nil {
		return schema.OfficeConversion{}, err
	}
	converted, err := config.Convert(ctx, f.Name, target, data)
	if err != nil {
		return schema.OfficeConversion{}, router.Errorf(502, "Collabora could not convert this file. For a PDF, use Extract scanned PDF to review its recognized text and save a Word copy")
	}
	return office.Converted(ctx, w, f, target, converted)
}
