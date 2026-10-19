package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/calendar"
	"thura/internal/chat"
	"thura/internal/drive"
	"thura/internal/meet"
	"thura/schema"
)

func GetDriveFile(ctx context.Context, r *router.Request[router.None]) (schema.DriveFile, error) {
	v, err := drive.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	return drive.File(v), err
}
func GetChatRoom(ctx context.Context, r *router.Request[router.None]) (schema.ChatRoom, error) {
	v, err := chat.Access(ctx, r.Param("workspaceId"), r.Param("id"))
	return chat.View(v), err
}
func GetMeeting(ctx context.Context, r *router.Request[router.None]) (schema.Meeting, error) {
	v, err := meet.Get(ctx, r.Param("workspaceId"), r.Param("id"))
	return meet.View(v), err
}
func GetCalendar(ctx context.Context, r *router.Request[router.None]) (schema.Calendar, error) {
	v, err := calendar.Access(ctx, r.Param("workspaceId"), r.Param("calendarId"))
	return calendar.View(v), err
}
