package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/chat"
	"thura/internal/labels"
	"thura/internal/platform/providererr"
	"thura/schema"
)

func ListChatRooms(ctx context.Context, r *router.Request[router.None]) (schema.ChatRoomList, error) {
	out, err := chat.List(ctx, r.Param("workspaceId"))
	if err != nil {
		return out, chatError(ctx, err)
	}
	// ?labelId= keeps rooms carrying that workspace label (a pinned label's filter).
	keep, err := labels.Filter(ctx, r.Param("workspaceId"), "chat", r.Query("labelId"))
	if err != nil || keep == nil {
		return out, err
	}
	rooms := []schema.ChatRoom{}
	for _, room := range out.Items {
		if keep[room.ID] {
			rooms = append(rooms, room)
		}
	}
	out.Items = rooms
	return out, nil
}
func CreateChatRoom(ctx context.Context, r *router.Request[schema.ChatRoomInput]) (schema.ChatRoom, error) {
	out, err := chat.Create(ctx, r.Param("workspaceId"), r.Body)
	return out, chatError(ctx, err)
}
func ChatTimeline(ctx context.Context, r *router.Request[router.None]) (schema.ChatTimeline, error) {
	out, err := chat.Timeline(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("from"))
	return out, chatError(ctx, err)
}
func SendChatMessage(ctx context.Context, r *router.Request[schema.ChatSendInput]) (schema.ChatEventResult, error) {
	out, err := chat.Send(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
	return out, chatError(ctx, err)
}
func InviteChatRemote(ctx context.Context, r *router.Request[schema.ChatRemoteInput]) (router.None, error) {
	err := chat.Remote(ctx, r.Param("workspaceId"), r.Param("id"), r.Body, false)
	return router.None{}, chatError(ctx, err)
}
func BanChatRemote(ctx context.Context, r *router.Request[schema.ChatRemoteInput]) (router.None, error) {
	err := chat.Remote(ctx, r.Param("workspaceId"), r.Param("id"), r.Body, true)
	return router.None{}, chatError(ctx, err)
}
func MarkChatRead(ctx context.Context, r *router.Request[schema.ChatReceiptInput]) (router.None, error) {
	err := chat.Receipt(ctx, r.Param("workspaceId"), r.Param("id"), r.Body.EventID)
	return router.None{}, chatError(ctx, err)
}

func SendChatFile(ctx context.Context, r *router.Request[schema.ChatFileInput]) (schema.ChatEventResult, error) {
	out, err := chat.SendFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
	return out, chatError(ctx, err)
}
func DownloadChatFile(ctx context.Context, r *router.Request[router.None]) (schema.FileContent, error) {
	out, err := chat.DownloadFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("eventId"))
	return out, chatError(ctx, err)
}

func SearchChatMessages(ctx context.Context, r *router.Request[router.None]) (schema.ChatTimeline, error) {
	out, err := chat.Search(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("search"), r.Query("cursor"))
	return out, chatError(ctx, err)
}

// chatError keeps Matrix's own wording (homeserver errors, configuration) in
// the log; the member reads one sentence (internal/platform/providererr).
func chatError(ctx context.Context, err error) error {
	return providererr.Hide(ctx, "matrix", providererr.Chat, err)
}
