package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/platform/servicehttp"
	"thura/internal/providers/collabora"
	"thura/internal/wopi"
	"thura/internal/workspace"
	"thura/schema"
)

func GetOfficeCapabilities(ctx context.Context, r *router.Request[router.None]) (schema.OfficeCapabilities, error) {
	if err := workspace.RequireMember(ctx, r.Param("workspaceId")); err != nil {
		return schema.OfficeCapabilities{}, err
	}
	result := schema.OfficeCapabilities{Extensions: []string{}, ContentTypes: []string{}}
	config, err := collabora.Load()
	if err != nil {
		return result, err
	}
	result.Extensions, result.ContentTypes, err = config.Formats(ctx)
	return result, err
}

func OpenOfficeDocument(ctx context.Context, r *router.Request[router.None]) (schema.OfficeConfig, error) {
	return wopi.Open(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("theme"), servicehttp.Origin(r.Raw), r.Query("mode") == "view")
}
