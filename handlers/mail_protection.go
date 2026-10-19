package handlers

import (
	"context"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/mailabuse"
	"thura/internal/mailbox"
	"thura/schema"
)

func GetMailProtection(ctx context.Context, r *router.Request[router.None]) (schema.MailProtectionView, error) {
	return mailabuse.Settings(ctx, r.Param("workspaceId"), auth.CurrentUser(ctx).ID)
}
func SaveMailProtection(ctx context.Context, r *router.Request[schema.MailProtectionInput]) (schema.MailProtectionView, error) {
	return mailabuse.SaveSettings(ctx, r.Param("workspaceId"), auth.CurrentUser(ctx).ID, r.Body)
}
func ReviewMail(ctx context.Context, r *router.Request[schema.MailReviewInput]) (schema.MailScreeningView, error) {
	if _, err := mailbox.ItemAccess(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id")); err != nil {
		return schema.MailScreeningView{}, err
	}
	if err := mailabuse.Review(ctx, r.Param("workspaceId"), r.Param("id"), auth.CurrentUser(ctx).ID, r.Body.Action); err != nil {
		return schema.MailScreeningView{}, err
	}
	view, err := mailabuse.View(ctx, r.Param("id"), auth.CurrentUser(ctx).ID)
	if err != nil {
		return schema.MailScreeningView{}, err
	}
	if view == nil {
		return schema.MailScreeningView{}, router.Errorf(409, "No screening record")
	}
	return *view, nil
}
