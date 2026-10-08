package drive

import (
	"context"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

type limits struct {
	Bytes int64 `env:"DRIVE_QUOTA_BYTES" default:"1073741824"`
}

func quotaLimit() (int64, error) {
	var cfg limits
	if err := env.Load(".", &cfg); err != nil {
		return 0, err
	}
	if cfg.Bytes < 1<<20 || cfg.Bytes > 1<<40 {
		return 0, router.Errorf(503, "Drive quota must be between 1 MiB and 1 TiB")
	}
	return cfg.Bytes, nil
}

// Caller holds the workspace lock before reserving or retaining new bytes.
func CheckQuota(ctx context.Context, queries *q.Queries, w string, additional int64, ignore *string) error {
	limit, err := quotaLimit()
	if err != nil {
		return err
	}
	usage, err := queries.DriveWorkspaceUsage(ctx, q.DriveWorkspaceUsageParams{WorkspaceID: w, IgnoreUploadID: ignore})
	if err != nil {
		return err
	}
	if usage.Retained+usage.Reserved+additional > limit {
		return router.Errorf(409, "workspace Drive quota exceeded; free retained versions or let pending uploads expire")
	}
	return nil
}
func Quota(ctx context.Context, w string) (schema.DriveQuota, error) {
	out := schema.DriveQuota{}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	limit, err := quotaLimit()
	if err != nil {
		return out, err
	}
	usage, err := q.New(db.From(ctx)).DriveWorkspaceUsage(ctx, q.DriveWorkspaceUsageParams{WorkspaceID: w})
	if err != nil {
		return out, err
	}
	return schema.DriveQuota{Retained: int(usage.Retained), Reserved: int(usage.Reserved), Limit: int(limit)}, nil
}
