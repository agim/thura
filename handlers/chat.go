package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/chat"
	"thura/schema"
)

func ListChatRooms(ctx context.Context, r *router.Request[router.None]) (schema.ChatRoomList, error) {
	return chat.List(ctx, r.Param("workspaceId"))
}
func CreateChatRoom(ctx context.Context, r *router.Request[schema.ChatRoomInput]) (schema.ChatRoom, error) {
	return chat.Create(ctx, r.Param("workspaceId"), r.Body)
}
func ChatTimeline(ctx context.Context, r *router.Request[router.None]) (schema.ChatTimeline, error) {
	return chat.Timeline(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("from"))
}
func SendChatMessage(ctx context.Context, r *router.Request[schema.ChatSendInput]) (schema.ChatEventResult, error) {
	return chat.Send(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
func InviteChatRemote(ctx context.Context, r *router.Request[schema.ChatRemoteInput]) (router.None, error) {
	err := chat.Remote(ctx, r.Param("workspaceId"), r.Param("id"), r.Body, false)
	return router.None{}, err
}
func BanChatRemote(ctx context.Context, r *router.Request[schema.ChatRemoteInput]) (router.None, error) {
	err := chat.Remote(ctx, r.Param("workspaceId"), r.Param("id"), r.Body, true)
	return router.None{}, err
}
func MarkChatRead(ctx context.Context, r *router.Request[schema.ChatReceiptInput]) (router.None, error) {
	err := chat.Receipt(ctx, r.Param("workspaceId"), r.Param("id"), r.Body.EventID)
	return router.None{}, err
}

func SendChatFile(ctx context.Context, r *router.Request[schema.ChatFileInput]) (schema.ChatEventResult, error) {
	return chat.SendFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
func DownloadChatFile(ctx context.Context, r *router.Request[router.None]) (schema.FileContent, error) {
	return chat.DownloadFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Query("eventId"))
}
