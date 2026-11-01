package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/meet"
	"thura/internal/platform/providererr"
	"thura/schema"
)

func ListMeetings(ctx context.Context, r *router.Request[router.None]) (schema.MeetingList, error) {
	return meet.List(ctx, r.Param("workspaceId"))
}
func CreateMeeting(ctx context.Context, r *router.Request[schema.MeetingInput]) (schema.Meeting, error) {
	out, err := meet.Create(ctx, r.Param("workspaceId"), r.Body)
	return out, providererr.Hide(ctx, "livekit", providererr.Meet, err)
}
func JoinMeeting(ctx context.Context, r *router.Request[router.None]) (schema.MeetingAccess, error) {
	out, err := meet.Join(ctx, r.Param("workspaceId"), r.Param("id"))
	return out, providererr.Hide(ctx, "livekit", providererr.Meet, err)
}
func EndMeeting(ctx context.Context, r *router.Request[router.None]) (schema.Meeting, error) {
	out, err := meet.End(ctx, r.Param("workspaceId"), r.Param("id"))
	return out, providererr.Hide(ctx, "livekit", providererr.Meet, err)
}

func JoinGuestMeeting(ctx context.Context, r *router.Request[schema.GuestMeetingInput]) (schema.GuestMeetingAccess, error) {
	out, err := meet.JoinGuest(ctx, r.Body)
	return out, providererr.Hide(ctx, "livekit", providererr.Meet, err)
}
