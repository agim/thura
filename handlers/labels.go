package handlers

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"net/http"
	q "thura/db/queries/gen"
	"thura/internal/calendar"
	"thura/internal/chat"
	"thura/internal/drive"
	"thura/internal/labels"
	"thura/internal/mailbox"
	"thura/internal/meet"
	"thura/internal/workspace"
	"thura/schema"
)

// A catalog entry is workspace-shared; reading or assigning its resource tags
// still requires the resource's normal access check, including private calendars
// and direct conversations. Guest meeting links do not enter these routes.
func labelResourceAccess(ctx context.Context, w, source, id string) error {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return err
	}
	if !workspace.ValidID(id) {
		return router.Errorf(404, "resource not found")
	}
	queries := q.New(db.From(ctx))
	var err error
	switch source {
	case "mail":
		var m string
		m, err = queries.LabelMailContainer(ctx, id)
		if err == nil {
			_, err = mailbox.ItemAccess(ctx, w, m, id)
		}
	case "document":
		_, err = drive.Get(ctx, w, id)
	case "chat":
		_, err = chat.Access(ctx, w, id)
	case "meeting":
		_, err = meet.Get(ctx, w, id)
	case "calendar":
		var c string
		c, err = queries.LabelResourceContainer(ctx, id)
		if err == nil {
			_, err = calendar.Get(ctx, w, c, id)
		}
	case "contact":
		_, err = queries.LabelContactExists(ctx, q.LabelContactExistsParams{ID: id, WorkspaceID: w})
	default:
		return router.Errorf(404, "resource not found")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "resource not found")
	}
	return err
}
func ListWorkspaceLabels(ctx context.Context, r *router.Request[router.None]) (schema.MailLabelList, error) {
	return labels.List(ctx, r.Param("workspaceId"))
}
func CreateWorkspaceLabel(ctx context.Context, r *router.Request[schema.MailLabelInput]) (schema.MailLabel, error) {
	l, err := labels.Create(ctx, r.Param("workspaceId"), r.Body.Name)
	if err == nil {
		r.Status(http.StatusCreated)
	}
	return l, err
}
func DeleteWorkspaceLabel(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	return router.None{}, labels.Delete(ctx, r.Param("workspaceId"), r.Param("labelId"))
}
func ListResourceLabels(ctx context.Context, r *router.Request[router.None]) (schema.MailLabelList, error) {
	w, source, id := r.Param("workspaceId"), r.Param("source"), r.Param("id")
	out := schema.MailLabelList{Items: []schema.MailLabel{}}
	if err := labelResourceAccess(ctx, w, source, id); err != nil {
		return out, err
	}
	queries := q.New(db.From(ctx))
	var rows []q.MailLabel
	var err error
	if source == "mail" {
		rows, err = queries.ListItemLabels(ctx, id)
	} else {
		rows, err = queries.ListResourceLabels(ctx, q.ListResourceLabelsParams{WorkspaceID: &w, Source: source, SourceID: id})
	}
	for _, l := range rows {
		out.Items = append(out.Items, labels.View(l))
	}
	return out, err
}
func CreateResourceLabel(ctx context.Context, r *router.Request[schema.MailLabelInput]) (schema.MailLabel, error) {
	w, source, id := r.Param("workspaceId"), r.Param("source"), r.Param("id")
	if err := labelResourceAccess(ctx, w, source, id); err != nil {
		return schema.MailLabel{}, err
	}
	l, err := labels.CreateAndApply(ctx, w, source, id, r.Body.Name)
	if err == nil {
		r.Status(http.StatusCreated)
	}
	return l, err
}
func ChangeResourceLabel(ctx context.Context, r *router.Request[schema.ResourceLabelInput]) (router.None, error) {
	w, source, id := r.Param("workspaceId"), r.Param("source"), r.Param("id")
	if err := labelResourceAccess(ctx, w, source, id); err != nil {
		return router.None{}, err
	}
	return router.None{}, labels.Change(ctx, w, source, id, r.Body.LabelID, r.Body.Applied)
}

// Label pins belong to the signed-in member in this workspace.
func ListLabelPins(ctx context.Context, r *router.Request[router.None]) (schema.LabelPinList, error) {
	return labels.Pins(ctx, r.Param("workspaceId"))
}
func PinLabel(ctx context.Context, r *router.Request[schema.LabelPinInput]) (schema.LabelPinList, error) {
	return labels.Pin(ctx, r.Param("workspaceId"), r.Body)
}
func UnpinLabel(ctx context.Context, r *router.Request[router.None]) (schema.LabelPinList, error) {
	return labels.Unpin(ctx, r.Param("workspaceId"), r.Param("app"), r.Param("labelId"))
}
func ReorderLabelPins(ctx context.Context, r *router.Request[schema.LabelPinOrder]) (schema.LabelPinList, error) {
	return labels.Reorder(ctx, r.Param("workspaceId"), r.Body)
}
