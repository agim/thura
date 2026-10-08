package handlers

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/platform/paging"
	"thura/internal/workspace"
	"thura/schema"
)

func ListWorkspaceAudit(ctx context.Context, req *router.Request[router.None]) (schema.WorkspaceAuditList, error) {
	out := schema.WorkspaceAuditList{Items: []schema.WorkspaceAuditRecord{}}
	id := req.Param("workspaceId")
	if err := workspace.RequireManager(ctx, id); err != nil {
		return out, err
	}
	page, err := paging.Read(req.Raw, 50)
	if err != nil {
		return out, err
	}
	if page.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		at, eventID, _ := strings.Cut(string(data), " ")
		_, timeErr := time.Parse(time.RFC3339Nano, at)
		if err != nil || timeErr != nil || !workspace.ValidID(eventID) {
			return out, router.Errorf(422, "invalid cursor")
		}
	}
	action, outcome := req.Query("action"), req.Query("outcome")
	if len(action) > 200 || (outcome != "" && outcome != audit.OK && outcome != audit.Denied && outcome != audit.Failed) {
		return out, router.Errorf(422, "invalid audit filter")
	}
	records, err := audit.From(ctx).List(ctx, audit.Query{Scope: id, Limit: int(page.Limit), Cursor: page.Cursor, Action: action, Outcome: outcome})
	if err != nil {
		return out, err
	}
	out.NextCursor = records.Next
	for _, record := range records.Records {
		item := schema.WorkspaceAuditRecord{ID: record.ID, At: record.At, Actor: record.Actor, Action: record.Action, Resource: record.Resource, Outcome: record.Outcome, RequestID: record.RequestID, Details: []schema.AuditDetail{}}
		var names []string
		for name := range record.Meta {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			item.Details = append(item.Details, schema.AuditDetail{Name: name, Value: record.Meta[name]})
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
