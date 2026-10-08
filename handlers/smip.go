package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/federation"
	"thura/schema"
)

func ListSmipBindings(ctx context.Context, r *router.Request[router.None]) (schema.SmipBindingList, error) {
	return federation.ListBindings(ctx, r.Param("workspaceId"))
}
func CreateSmipBinding(ctx context.Context, r *router.Request[schema.SmipBindingInput]) (schema.SmipBinding, error) {
	return federation.CreateBinding(ctx, r.Param("workspaceId"), r.Body)
}
func DisableSmipBinding(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	return router.None{}, federation.DisableBinding(ctx, r.Param("workspaceId"), r.Param("id"))
}
func ListSmipInbox(ctx context.Context, r *router.Request[router.None]) (schema.SmipInboxList, error) {
	return federation.ListInbox(ctx, r.Param("workspaceId"), r.Query("cursor"))
}
func ImportSmipFile(ctx context.Context, r *router.Request[schema.SmipImportInput]) (schema.DriveFile, error) {
	return federation.ImportFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
