package handlers

import (
	"context"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"net/http"
	queries "thura/db/queries/gen"
	"thura/internal/platform/auditlog"
	"thura/internal/workspace"
	"thura/schema"
)

func ListMembers(ctx context.Context, r *router.Request[router.None]) (schema.MemberList, error) {
	id := r.Param("workspaceId")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return schema.MemberList{}, err
	}
	rows, err := queries.New(db.From(ctx)).ListWorkspaceMembers(ctx, id)
	out := schema.MemberList{Items: []schema.MemberView{}}
	for _, m := range rows {
		out.Items = append(out.Items, schema.MemberView{Subject: m.Subject, Email: m.Email, Name: m.Name, Role: schema.WorkspaceRole(m.Role)})
	}
	return out, err
}
func InviteMember(ctx context.Context, r *router.Request[schema.InviteInput]) (schema.InviteView, error) {
	out, err := workspace.Invite(ctx, r.Param("workspaceId"), r.Body)
	if err == nil {
		r.Status(http.StatusCreated)
	}
	return out, auditlog.Refused(ctx, "invitation.create", r.Param("workspaceId"), "invitation", err)
}
func AcceptInvite(ctx context.Context, r *router.Request[schema.AcceptInviteInput]) (schema.Workspace, error) {
	out, err := workspace.Accept(ctx, r.Body)
	return out, auditlog.Refused(ctx, "invitation.accept", "", "invitation", err)
}
func ChangeRole(ctx context.Context, r *router.Request[schema.RoleInput]) (router.None, error) {
	err := workspace.ChangeMember(ctx, r.Param("workspaceId"), r.Param("subject"), string(r.Body.Role))
	return router.None{}, auditlog.Refused(ctx, "member.role", r.Param("workspaceId"), "member", err)
}
func RemoveMember(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	err := workspace.ChangeMember(ctx, r.Param("workspaceId"), r.Param("subject"), "")
	return router.None{}, auditlog.Refused(ctx, "member.remove", r.Param("workspaceId"), "member", err)
}
func RevokeInvite(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	err := workspace.Revoke(ctx, r.Param("workspaceId"), r.Param("id"))
	return router.None{}, auditlog.Refused(ctx, "invitation.revoke", r.Param("workspaceId"), "invitation", err)
}
func ListInvites(ctx context.Context, r *router.Request[router.None]) (schema.InviteList, error) {
	id := r.Param("workspaceId")
	if err := workspace.RequireManager(ctx, id); err != nil {
		return schema.InviteList{}, err
	}
	rows, err := queries.New(db.From(ctx)).ListInvitations(ctx, id)
	out := schema.InviteList{Items: []schema.InviteView{}}
	for _, i := range rows {
		out.Items = append(out.Items, workspace.InviteView(i))
	}
	return out, err
}
