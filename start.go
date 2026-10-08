package main

import (
	"context"
	"os"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/middleware"
	"thura/internal/mailbox"
	provider "thura/internal/providers/onlyoffice"
)

// onStart runs after the packs have started and before the app listens:
// register job handlers (jobs.FromServices(s).Handle(kind, fn)), warm a
// cache, lidza.Provide a service the packs do not. main.go is generated
// once and left alone; this file is the app's.
func onStart(ctx context.Context, s *lidza.Services) error {
	jobs.FromServices(s).Handle(mailbox.DeliveryJob, mailbox.Deliver)
	return nil
}

// appMiddleware wraps the whole app, pages and API alike, outermost
// first: rate limits, a redirect from www to the bare domain, headers.
// Middleware for the API only goes on the router (r.Use in routes.go).
func appMiddleware() []middleware.Middleware {
	if os.Getenv("LIDZA_MODE") == "dev" {
		return nil
	}
	cfg, err := provider.Load()
	if err != nil {
		return nil
	}
	policy := middleware.AddCSP(middleware.DefaultCSP, "script-src", cfg.ServerURL)
	policy = middleware.AddCSP(policy, "frame-src", "'self'", cfg.ServerURL)
	policy = middleware.AddCSP(policy, "connect-src", cfg.ServerURL)
	return []middleware.Middleware{middleware.SecureHeaders(middleware.SecureHeadersOptions{CSP: policy})}
}
