package handlers

import (
	"context"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	q "thura/db/queries/gen"
	"thura/internal/setup"
	"thura/internal/tenancy"
	"thura/internal/workspace"
	"thura/schema"
)

func CreateTeam(ctx context.Context, r *router.Request[schema.NewTeamInput]) (schema.Workspace, error) {
	if !setup.Allow(ctx) || tenancy.Host(ctx).WorkspaceID != "" {
		return schema.Workspace{}, router.Errorf(403, "Only the deployment administrator can provision teams")
	}
	profile, e := auth.From(ctx).Profile(ctx, auth.CurrentUser(ctx).ID)
	if e != nil {
		return schema.Workspace{}, e
	}
	out, e := workspace.CreateManaged(ctx, schema.CreateWorkspaceInput{Name: r.Body.Name, OwnerEmail: profile.Email})
	if e == nil {
		r.Status(201)
	}
	return out, e
}
func GetTenantContext(ctx context.Context, r *router.Request[router.None]) (schema.TenantContext, error) {
	return tenancy.Host(ctx), nil
}
func GetTeamSetup(ctx context.Context, r *router.Request[router.None]) (schema.TeamSetupView, error) {
	return tenancy.View(ctx, r.Param("workspaceId"))
}
func AddTeamDomain(ctx context.Context, r *router.Request[schema.TeamDomainInput]) (schema.TeamDomainView, error) {
	out, e := tenancy.AddDomain(ctx, r.Param("workspaceId"), r.Body)
	if e == nil {
		r.Status(201)
	}
	return out, e
}
func VerifyTeamDomain(ctx context.Context, r *router.Request[router.None]) (schema.TeamDomainView, error) {
	return tenancy.Verify(ctx, r.Param("workspaceId"), r.Param("id"))
}
func RemoveTeamDomain(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	return router.None{}, tenancy.Remove(ctx, r.Param("workspaceId"), r.Param("id"))
}
func SaveTeamMail(ctx context.Context, r *router.Request[schema.TeamMailInput]) (schema.TeamMailView, error) {
	return tenancy.SaveMail(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
func PublishTeamSetup(ctx context.Context, r *router.Request[schema.TeamPublishInput]) (schema.TeamSetupView, error) {
	return tenancy.Publish(ctx, r.Param("workspaceId"), r.Body)
}
func CreateTeamMailbox(ctx context.Context, r *router.Request[schema.TeamMailboxInput]) (schema.Mailbox, error) {
	out, e := tenancy.CreateMailbox(ctx, r.Param("workspaceId"), r.Body)
	if e == nil {
		r.Status(201)
	}
	return out, e
}
func RotateTeamIdentity(ctx context.Context, r *router.Request[router.None]) (schema.TeamMailView, error) {
	return tenancy.RotateIdentity(ctx, r.Param("workspaceId"), r.Param("id"))
}

func ListManagedTeams(ctx context.Context, r *router.Request[router.None]) (schema.WorkspaceList, error) {
	out := schema.WorkspaceList{Items: []schema.Workspace{}}
	if !setup.Allow(ctx) || tenancy.Host(ctx).WorkspaceID != "" {
		return out, router.Errorf(403, "Server administrator required")
	}
	rows, e := q.New(db.From(ctx)).ListManagedTeams(ctx)
	for _, w := range rows {
		out.Items = append(out.Items, schema.Workspace{ID: w.ID, Name: w.Name, CreatedAt: w.CreatedAt})
	}
	return out, e
}

func ListTeamMailboxes(ctx context.Context, r *router.Request[router.None]) (schema.MailboxList, error) {
	id := r.Param("workspaceId")
	out := schema.MailboxList{Items: []schema.Mailbox{}}
	if e := tenancy.Manage(ctx, id); e != nil {
		return out, e
	}
	rows, e := q.New(db.From(ctx)).ListMailboxes(ctx, id)
	for _, m := range rows {
		out.Items = append(out.Items, schema.Mailbox{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name, Address: m.Address, ConfigPrefix: m.ConfigPrefix, Signature: m.Signature, CreatedAt: m.CreatedAt})
	}
	return out, e
}
