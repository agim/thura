package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/federation"
	"thura/schema"
)

func ListSmipBindings(ctx context.Context, r *router.Request[router.None]) (schema.SmipBindingList, error) {
	return federation.ListBindings(ctx, r.Param("workspaceId"))
}
func CreateSmipBinding(ctx context.Context, r *router.Request[schema.SmipBindingInput]) (schema.SmipBinding, error) {
	return federation.CreateBinding(ctx, r.Param("workspaceId"), r.Body)
}
func DisableSmipBinding(ctx context.Context, r *router.Request[router.None]) (router.None, error) {
	return router.None{}, federation.DisableBinding(ctx, r.Param("workspaceId"), r.Param("id"))
}
func ListSmipInbox(ctx context.Context, r *router.Request[router.None]) (schema.SmipInboxList, error) {
	return federation.ListInbox(ctx, r.Param("workspaceId"), r.Query("cursor"))
}
func ImportSmipFile(ctx context.Context, r *router.Request[schema.SmipImportInput]) (schema.DriveFile, error) {
	return federation.ImportFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}

func QueueSmipMessage(ctx context.Context, r *router.Request[schema.SmipSendInput]) (schema.SmipOutbound, error) {
	return federation.Queue(ctx, r.Param("workspaceId"), r.Body)
}
func ListSmipOutbox(ctx context.Context, r *router.Request[router.None]) (schema.SmipOutboxList, error) {
	return federation.ListOutbox(ctx, r.Param("workspaceId"), r.Query("cursor"))
}
func ResumeSmipMessage(ctx context.Context, r *router.Request[router.None]) (schema.SmipOutbound, error) {
	return federation.Resume(ctx, r.Param("workspaceId"), r.Param("id"))
}

func CancelSmipMessage(ctx context.Context, r *router.Request[router.None]) (schema.SmipOutbound, error) {
	return federation.Cancel(ctx, r.Param("workspaceId"), r.Param("id"))
}

// External SMIP chat and files: a workspace's sharing policy (owners and
// admins) and each member's own requests and conversations.
func GetSmipPolicy(ctx context.Context, r *router.Request[router.None]) (schema.SmipPolicyView, error) {
	return federation.GetPolicy(ctx, r.Param("workspaceId"))
}
func SaveSmipPolicy(ctx context.Context, r *router.Request[schema.SmipPolicyInput]) (schema.SmipPolicyView, error) {
	return federation.SavePolicy(ctx, r.Param("workspaceId"), r.Body)
}
func UnblockSmipDomain(ctx context.Context, r *router.Request[schema.SmipDomainInput]) (schema.SmipPolicyView, error) {
	return federation.UnblockDomain(ctx, r.Param("workspaceId"), r.Body)
}
func ListSmipConversations(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationList, error) {
	return federation.ListConversations(ctx, r.Param("workspaceId"))
}
func StartSmipConversation(ctx context.Context, r *router.Request[schema.SmipConversationInput]) (schema.SmipConversationDetail, error) {
	return federation.StartConversation(ctx, r.Param("workspaceId"), r.Body)
}
func GetSmipConversation(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.GetConversation(ctx, r.Param("workspaceId"), r.Param("id"))
}
func AcceptSmipConversation(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.Decide(ctx, r.Param("workspaceId"), r.Param("id"), federation.DecisionAccept)
}
func DeclineSmipConversation(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.Decide(ctx, r.Param("workspaceId"), r.Param("id"), federation.DecisionDecline)
}
func MarkSmipConversationSpam(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.Decide(ctx, r.Param("workspaceId"), r.Param("id"), federation.DecisionSpam)
}
func BlockSmipConversationDomain(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.Decide(ctx, r.Param("workspaceId"), r.Param("id"), federation.DecisionBlock)
}
func SendSmipConversationMessage(ctx context.Context, r *router.Request[schema.SmipConversationMessageInput]) (schema.SmipConversationItem, error) {
	return federation.SendConversationMessage(ctx, r.Param("workspaceId"), r.Param("id"), r.Body)
}
func SaveSmipConversationFile(ctx context.Context, r *router.Request[schema.SmipImportInput]) (schema.DriveFile, error) {
	return federation.SaveIncomingFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Param("itemId"), r.Body)
}
func DeclineSmipConversationFile(ctx context.Context, r *router.Request[router.None]) (schema.SmipConversationDetail, error) {
	return federation.DeclineIncomingFile(ctx, r.Param("workspaceId"), r.Param("id"), r.Param("itemId"))
}
