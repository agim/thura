package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/office"
	"thura/schema"
)

func OpenOfficeDocument(ctx context.Context, r *router.Request[router.None]) (schema.OfficeConfig, error) {
	return office.OpenMode(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("theme"), r.Query("mode") == "view")
}
