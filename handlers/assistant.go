package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/assistant"
	"thura/schema"
)

func GetAISettings(ctx context.Context, r *router.Request[router.None]) (schema.AISettings, error) {
	return assistant.Settings(ctx, r.Param("workspaceId"))
}
func SaveAISettings(ctx context.Context, r *router.Request[schema.AISettingsInput]) (schema.AISettings, error) {
	return assistant.Save(ctx, r.Param("workspaceId"), r.Body)
}
func RunAssistant(ctx context.Context, r *router.Request[schema.AssistantInput]) (schema.AssistantResult, error) {
	return assistant.Generate(ctx, r.Param("workspaceId"), r.Body)
}
func TestAIConnection(ctx context.Context, r *router.Request[router.None]) (schema.AssistantResult, error) {
	return assistant.TestConnection(ctx, r.Param("workspaceId"))
}
