package drive

import (
	"context"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	q "thura/db/queries/gen"
	"thura/internal/platform/documenttext"
)

// SearchText reads only the current immutable version. No extracted copy is
// retained: replacement, trash, and permission changes take effect immediately.
func SearchText(ctx context.Context, w, id string) (string, error) {
	f, err := Get(ctx, w, id)
	if err != nil {
		return "", err
	}
	if f.Trashed {
		return "", router.Errorf(404, "file not found")
	}
	v, err := q.New(db.From(ctx)).GetFileVersion(ctx, q.GetFileVersionParams{FileID: id, Number: f.CurrentVersion})
	if err != nil {
		return "", err
	}
	data, err := read(ctx, v.ObjectKey)
	if err != nil {
		return "", err
	}
	if Hash(data) != v.Checksum {
		return "", router.Errorf(503, "file content unavailable")
	}
	text, err := documenttext.Extract(ctx, data, f.ContentType)
	if err != nil {
		return "", err
	}
	current, err := Get(ctx, w, id)
	if err != nil {
		return "", err
	}
	if current.Trashed || current.CurrentVersion != f.CurrentVersion {
		return "", router.Errorf(409, "file changed; refresh search")
	}
	return text, nil
}
