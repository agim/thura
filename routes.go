package main

import (
	"context"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/middleware"
	"github.com/agim/lidza/pkg/router"
	"thura/handlers"
	"thura/internal/office"

	"thura/schema"
)

// routes registers the app's API. Every route lives under /api; the frontend
// never defines one. GET /api/v1/health is built in. router.Route gives a
// handler typed input and output: the client in .lidza/client is generated
// from these types, so the frontend cannot drift from the API.
func routes(r *router.Router) {
	r.HandleFunc("GET /api/v1/office/source", office.Source)
	r.HandleFunc("POST /api/v1/office/callback/{id}", office.Callback)
	auth.Mount(r, auth.Options{NoRegister: true, Providers: []auth.Provider{}, Title: "Thura"})
	router.Route(r, "GET /api/v1/hello/{name}", hello)
	router.Route(r, "POST /api/v1/inbound/{mailboxId}", handlers.ReceiveMail, router.UploadLimit(12<<20))
	router.Route(r.Group("/api/v1/invitations", auth.Optional()), "POST /api/v1/invitations/accept", handlers.AcceptInvite, auth.Throttle())
	router.Route(r.Group("/api/v1/shares", auth.Optional()), "POST /api/v1/shares/open", handlers.OpenDriveShare, middleware.RateLimit(middleware.RateLimitOptions{RPS: 1, Burst: 10}))
	reply := r.Group("/api/v1/calendar-replies", auth.Optional(), middleware.RateLimit(middleware.RateLimitOptions{RPS: 1, Burst: 10}))
	router.Route(reply, "POST /api/v1/calendar-replies/open", handlers.OpenCalendarReply)
	router.Route(reply, "POST /api/v1/calendar-replies/respond", handlers.ReplyCalendar)
	router.Route(reply, "POST /api/v1/calendar-replies/ics", handlers.ReplyCalendarICS)
	g := r.Group("/api/v1", auth.Require())
	router.Route(g, "GET /api/v1/workspaces", handlers.ListWorkspaces)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/chat", handlers.ListChatRooms)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat", handlers.CreateChatRoom)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/chat/{id}/messages", handlers.ChatTimeline)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat/{id}/messages", handlers.SendChatMessage)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat/{id}/invitations", handlers.InviteChatRemote)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat/{id}/bans", handlers.BanChatRemote)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat/{id}/read", handlers.MarkChatRead)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/chat/{id}/files", handlers.SendChatFile)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/chat/{id}/files", handlers.DownloadChatFile)

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
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/drive", handlers.ListDrive)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/drive/folders", handlers.CreateDriveFolder)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/drive/uploads", handlers.BeginDriveUpload)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/drive/uploads/{id}", handlers.GetDriveUpload)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/drive/uploads/{id}/chunks/{number}", handlers.PutDriveChunk, router.UploadLimit(1<<20))
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/drive/uploads/{id}/finish", handlers.FinishDriveUpload)
	router.Route(g, "PATCH /api/v1/workspaces/{workspaceId}/drive/files/{id}", handlers.UpdateDriveFile)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/drive/files/{id}/content", handlers.DownloadDriveFile)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/drive/files/{id}/versions", handlers.ListDriveVersions)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/drive/files/{id}/shares", handlers.CreateDriveShare)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/drive/files/{id}/shares", handlers.ListDriveShares)
	router.Route(g, "DELETE /api/v1/workspaces/{workspaceId}/drive/files/{id}/shares/{shareId}", handlers.RevokeDriveShare)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/drive/files/{id}/office", handlers.OpenOfficeDocument)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars", handlers.ListCalendars)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/calendars", handlers.CreateCalendar)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events", handlers.ListCalendarEvents)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/instances", handlers.CalendarInstances)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events", handlers.CreateCalendarEvent)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}", handlers.GetCalendarEvent)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}", handlers.UpdateCalendarEvent)
	router.Route(g, "DELETE /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}", handlers.CancelCalendarEvent)
	router.Route(g, "PATCH /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}/occurrences", handlers.ChangeCalendarOccurrence)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/import", handlers.ImportCalendar, router.UploadLimit(1<<20))
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/export", handlers.ExportCalendar)
	router.Route(g, "POST /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}/invitations", handlers.SendCalendarInvitations)
	router.Route(g, "GET /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}/reminder", handlers.GetCalendarReminder)
	router.Route(g, "PUT /api/v1/workspaces/{workspaceId}/calendars/{calendarId}/events/{id}/reminder", handlers.SetCalendarReminder)
}

// hello greets by name. Greeting is defined in schema.lidza.
func hello(ctx context.Context, req *router.Request[router.None]) (schema.Greeting, error) {
	name := req.Param("name")
	return schema.Greeting{Name: name, Message: "hello, " + name}, nil
}
