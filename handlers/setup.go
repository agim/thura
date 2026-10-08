package handlers

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	"thura/internal/setup"
	"thura/schema"
)

func SetupStatus(ctx context.Context, r *router.Request[router.None]) (schema.SetupStatus, error) {
	return setup.Status(ctx)
}
func ClaimSetup(ctx context.Context, r *router.Request[schema.SetupClaimInput]) (schema.SetupStatus, error) {
	return setup.Claim(ctx, r.Body)
}
