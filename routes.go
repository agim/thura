package main

import (
	"context"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/router"
	"thura/handlers"

	"thura/schema"
)

// routes registers the app's API. Every route lives under /api; the frontend
// never defines one. GET /api/v1/health is built in. router.Route gives a
// handler typed input and output: the client in .lidza/client is generated
// from these types, so the frontend cannot drift from the API.
func routes(r *router.Router) {
	auth.Mount(r, auth.Options{NoRegister: true, Providers: []auth.Provider{}, Title: "Thura"})
	router.Route(r, "GET /api/v1/hello/{name}", hello)
	router.Route(r, "POST /api/v1/inbound/{mailboxId}", handlers.ReceiveMail, router.UploadLimit(12<<20))
	router.Route(r.Group("/api/v1/invitations", auth.Optional()), "POST /api/v1/invitations/accept", handlers.AcceptInvite, auth.Throttle())
	g := r.Group("/api/v1", auth.Require())
	router.Route(g, "GET /api/v1/workspaces", handlers.ListWorkspaces)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/contacts", handlers.ListContacts)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/contacts", handlers.CreateContact)
	router.Route(g, "PATCH /api/v1/workspaces/{workspaceId}/contacts/{id}", handlers.UpdateContact)
	router.Route(g, "DELETE /api/v1/workspaces/{workspaceId}/contacts/{id}", handlers.DeleteContact)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/members", handlers.ListMembers)
	router.Route(g, "PATCH /api/v1/workspaces/{workspaceId}/members/{subject}", handlers.ChangeRole)
	router.Route(g, "DELETE /api/v1/workspaces/{workspaceId}/members/{subject}", handlers.RemoveMember)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/invitations", handlers.ListInvites)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/invitations", handlers.InviteMember)
	router.Route(g, "DELETE /api/v1/workspaces/{workspaceId}/invitations/{id}", handlers.RevokeInvite)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/mailboxes", handlers.ListMailboxes)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages", handlers.ListMail)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages", handlers.CreateMailDraft)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}", handlers.GetMail)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}", handlers.UpdateMailDraft)
	router.Route(g, "PATCH /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}", handlers.UpdateMailFlags)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}/send", handlers.SendMail)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}/undo", handlers.UndoMail)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}/attachments", handlers.UploadMailAttachment, router.UploadLimit(10<<20))
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/messages/{id}/attachments/{attachmentId}", handlers.DownloadMailAttachment)
}

// hello greets by name. Greeting is defined in schema.lidza.
func hello(ctx context.Context, req *router.Request[router.None]) (schema.Greeting, error) {
	name := req.Param("name")
	return schema.Greeting{Name: name, Message: "hello, " + name}, nil
}
