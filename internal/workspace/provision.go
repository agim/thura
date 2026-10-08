package workspace

import (
	"context"
	"strings"

	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	queries "thura/db/queries/gen"
	"thura/schema"
)

// Create is an operator operation, intentionally absent from the public API.
// The owner must already have a provisioned account. Workspace creation and
// ownership are committed together, so a workspace never has no owner.
func Create(ctx context.Context, in schema.CreateWorkspaceInput) (schema.Workspace, error) {
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return schema.Workspace{}, err
	}
	p, err := auth.From(ctx).ProfileByEmail(ctx, strings.ToLower(strings.TrimSpace(in.OwnerEmail)))
	if err != nil {
		return schema.Workspace{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.Workspace{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	row, err := q.CreateWorkspace(ctx, in.Name)
	if err != nil {
		return schema.Workspace{}, err
	}
	if err = q.GrantWorkspaceOwner(ctx, queries.GrantWorkspaceOwnerParams{Subject: p.Subject, Scope: row.ID}); err != nil {
		return schema.Workspace{}, err
	}
	if err = audit.From(ctx).RecordTx(audit.System(ctx, "workspace-provision"), tx, audit.Event{Action: "workspace.create", Scope: row.ID, Resource: "workspace/" + row.ID, Meta: map[string]string{"owner_subject": p.Subject}}); err != nil {
		return schema.Workspace{}, err
	}
	return schema.Workspace{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, tx.Commit(ctx)
}
