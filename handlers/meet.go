package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/meet"
	"thura/schema"
)

func ListMeetings(ctx context.Context, r *router.Request[router.None]) (schema.MeetingList, error) {
	return meet.List(ctx, r.Param("workspaceId"))
}
func CreateMeeting(ctx context.Context, r *router.Request[schema.MeetingInput]) (schema.Meeting, error) {
	return meet.Create(ctx, r.Param("workspaceId"), r.Body)
}
func JoinMeeting(ctx context.Context, r *router.Request[router.None]) (schema.MeetingAccess, error) {
	return meet.Join(ctx, r.Param("workspaceId"), r.Param("id"))
}
func EndMeeting(ctx context.Context, r *router.Request[router.None]) (schema.Meeting, error) {
	return meet.End(ctx, r.Param("workspaceId"), r.Param("id"))
}
