package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/correspondence"
	"thura/internal/platform/paging"
	"thura/schema"
)

func GetCorrespondence(ctx context.Context, req *router.Request[router.None]) (schema.CorrespondenceTimeline, error) {
	return correspondenceTimeline(ctx, req, false)
}

func ListCorrespondenceSources(ctx context.Context, req *router.Request[router.None]) (schema.CorrespondenceTimeline, error) {
	return correspondenceTimeline(ctx, req, true)
}

func correspondenceTimeline(ctx context.Context, req *router.Request[router.None], browse bool) (schema.CorrespondenceTimeline, error) {
	page, err := paging.Read(req.Raw, 50)
	if err != nil {
		return schema.CorrespondenceTimeline{}, err
	}
	return correspondence.Timeline(ctx, req.Param("workspaceId"), req.Param("id"), req.Query("scope"), req.Query("source"), page, browse)
}
func LinkCorrespondence(ctx context.Context, req *router.Request[schema.CorrespondenceLinkInput]) (schema.CorrespondenceLinkInput, error) {
	return req.Body, correspondence.Link(ctx, req.Param("workspaceId"), req.Param("id"), req.Body.Source, req.Body.SourceID, false)
}
func UnlinkCorrespondence(ctx context.Context, req *router.Request[router.None]) (router.None, error) {
	return router.None{}, correspondence.Link(ctx, req.Param("workspaceId"), req.Param("id"), req.Param("source"), req.Param("sourceId"), true)
}
