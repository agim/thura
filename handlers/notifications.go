package handlers

import (
	"context"

	"github.com/agim/lidza/pkg/router"
	"thura/internal/push"
	"thura/schema"
)

// Notification settings and devices are the signed-in member's own in the
// workspace (internal/push); another member's device is a 404.
func GetNotificationSettings(ctx context.Context, r *router.Request[router.None]) (schema.NotificationSettings, error) {
	return push.Settings(ctx, r.Param("workspaceId"))
}
func SaveNotificationPreferences(ctx context.Context, r *router.Request[schema.NotificationPreferences]) (schema.NotificationSettings, error) {
	return push.SavePreferences(ctx, r.Param("workspaceId"), r.Body)
}
func SubscribePushDevice(ctx context.Context, r *router.Request[schema.PushSubscribeInput]) (schema.NotificationSettings, error) {
	return push.Subscribe(ctx, r.Param("workspaceId"), r.Body)
}
func UnsubscribePushDevice(ctx context.Context, r *router.Request[router.None]) (schema.NotificationSettings, error) {
	return push.Unsubscribe(ctx, r.Param("workspaceId"), r.Param("id"))
}
func TestPushNotification(ctx context.Context, r *router.Request[router.None]) (schema.PushTestResult, error) {
	return push.Test(ctx, r.Param("workspaceId"))
}
