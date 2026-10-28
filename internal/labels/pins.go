package labels

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

// PinApps are the apps with a label sidebar. A pin stored for "all" shows in
// each of them unless the member unpinned the label in that app.
func PinApps() []string { return []string{"mail", "drive", "calendar", "contacts", "chat"} }

func pinApp(app string, all bool) bool {
	for _, a := range PinApps() {
		if a == app {
			return true
		}
	}
	return all && app == "all"
}

type pinKey struct{ label, app string }

// Pins are a member's own: every read and write is scoped to the workspace
// and the signed-in subject, so pinning never changes another sidebar.
func member(ctx context.Context, w string) (string, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return "", err
	}
	if err := ensureAll(ctx, w); err != nil {
		return "", err
	}
	return auth.CurrentUser(ctx).ID, nil
}

// resolve returns the effective pins for each app: an explicit row for the
// app wins, then a row for "all", then a system label's default app.
func resolve(ctx context.Context, queries *q.Queries, w, subject string) ([]schema.LabelPinView, map[pinKey]q.LabelPin, error) {
	catalog, err := queries.ListWorkspaceLabels(ctx, &w)
	if err != nil {
		return nil, nil, err
	}
	rows, err := queries.ListLabelPins(ctx, q.ListLabelPinsParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return nil, nil, err
	}
	stored := map[pinKey]q.LabelPin{}
	for _, r := range rows {
		stored[pinKey{r.LabelID, r.App}] = r
	}
	out := []schema.LabelPinView{}
	for _, l := range catalog {
		for _, app := range PinApps() {
			view := schema.LabelPinView{LabelID: l.ID, App: app, Label: View(l)}
			if r, ok := stored[pinKey{l.ID, app}]; ok {
				if !r.Pinned {
					continue
				}
				view.Position = int(r.Position)
			} else if r, ok := stored[pinKey{l.ID, "all"}]; ok {
				if !r.Pinned {
					continue
				}
				view.Position = int(r.Position)
			} else if l.SystemKey != nil && systemLabel(*l.SystemKey).app == app {
				view.Position, view.IsDefault = -1, true
			} else {
				continue
			}
			out = append(out, view)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.App != b.App {
			return a.App < b.App
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return strings.ToLower(a.Label.Name) < strings.ToLower(b.Label.Name)
	})
	return out, stored, nil
}

func Pins(ctx context.Context, w string) (schema.LabelPinList, error) {
	subject, err := member(ctx, w)
	if err != nil {
		return schema.LabelPinList{Items: []schema.LabelPinView{}}, err
	}
	items, _, err := resolve(ctx, q.New(db.From(ctx)), w, subject)
	return schema.LabelPinList{Items: items}, err
}

// label checks the label belongs to this workspace; any other is a 404.
func label(ctx context.Context, queries *q.Queries, w, id string) error {
	if !workspace.ValidID(id) {
		return router.Errorf(404, "label not found")
	}
	_, err := queries.GetWorkspaceLabel(ctx, q.GetWorkspaceLabelParams{ID: id, WorkspaceID: &w})
	if errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "label not found")
	}
	return err
}

func Pin(ctx context.Context, w string, in schema.LabelPinInput) (schema.LabelPinList, error) {
	empty := schema.LabelPinList{Items: []schema.LabelPinView{}}
	subject, err := member(ctx, w)
	if err != nil {
		return empty, err
	}
	if !pinApp(in.App, true) {
		return empty, router.Errorf(422, "unknown app")
	}
	queries := q.New(db.From(ctx))
	if err = label(ctx, queries, w, in.LabelID); err != nil {
		return empty, err
	}
	rows, err := queries.ListLabelPins(ctx, q.ListLabelPinsParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return empty, err
	}
	next := int32(0)
	for _, r := range rows {
		if r.Position >= next {
			next = r.Position + 1
		}
	}
	if in.App == "all" {
		// Pinning everywhere also lifts earlier per-app unpins of this label.
		if err = queries.DeleteLabelPins(ctx, q.DeleteLabelPinsParams{WorkspaceID: w, Subject: subject, LabelID: in.LabelID}); err != nil {
			return empty, err
		}
	}
	if err = queries.SaveLabelPin(ctx, q.SaveLabelPinParams{WorkspaceID: w, Subject: subject, LabelID: in.LabelID, App: in.App, Pinned: true, Position: next}); err != nil {
		return empty, err
	}
	items, _, err := resolve(ctx, queries, w, subject)
	return schema.LabelPinList{Items: items}, err
}

func Unpin(ctx context.Context, w, app, id string) (schema.LabelPinList, error) {
	empty := schema.LabelPinList{Items: []schema.LabelPinView{}}
	subject, err := member(ctx, w)
	if err != nil {
		return empty, err
	}
	if !pinApp(app, true) {
		return empty, router.Errorf(404, "unknown app")
	}
	queries := q.New(db.From(ctx))
	if err = label(ctx, queries, w, id); err != nil {
		return empty, err
	}
	apps := []string{app}
	if app == "all" {
		apps = append([]string{"all"}, PinApps()...)
	}
	// An unpin is a stored pinned=false row, so a system label's default pin
	// stays off without a row per member until they unpin it.
	for _, a := range apps {
		if err = queries.SaveLabelPin(ctx, q.SaveLabelPinParams{WorkspaceID: w, Subject: subject, LabelID: id, App: a, Pinned: false}); err != nil {
			return empty, err
		}
	}
	items, _, err := resolve(ctx, queries, w, subject)
	return schema.LabelPinList{Items: items}, err
}

func Reorder(ctx context.Context, w string, in schema.LabelPinOrder) (schema.LabelPinList, error) {
	empty := schema.LabelPinList{Items: []schema.LabelPinView{}}
	subject, err := member(ctx, w)
	if err != nil {
		return empty, err
	}
	if !pinApp(in.App, false) {
		return empty, router.Errorf(422, "unknown app")
	}
	if len(in.LabelIDs) > 100 {
		return empty, router.Errorf(422, "too many labels")
	}
	queries := q.New(db.From(ctx))
	items, _, err := resolve(ctx, queries, w, subject)
	if err != nil {
		return empty, err
	}
	pinned := map[string]bool{}
	for _, p := range items {
		if p.App == in.App {
			pinned[p.LabelID] = true
		}
	}
	seen := map[string]bool{}
	for _, id := range in.LabelIDs {
		if !pinned[id] || seen[id] {
			return empty, router.Errorf(422, "order lists a label that is not pinned in this app, or lists it twice")
		}
		seen[id] = true
	}
	for i, id := range in.LabelIDs {
		if err = queries.SaveLabelPin(ctx, q.SaveLabelPinParams{WorkspaceID: w, Subject: subject, LabelID: id, App: in.App, Pinned: true, Position: int32(i)}); err != nil {
			return empty, err
		}
	}
	items, _, err = resolve(ctx, queries, w, subject)
	return schema.LabelPinList{Items: items}, err
}
