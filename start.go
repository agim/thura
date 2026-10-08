package main

import (
	"context"
	"os"
	"thura/internal/calendar"
	"thura/internal/chat"
	"time"

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
	jobs.FromServices(s).Handle(calendar.ReminderJob, calendar.Remind, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(calendar.ReminderJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(chat.ReconcileJob, chat.Reconcile, jobs.Concurrency(1))
	return jobs.FromServices(s).Schedule(chat.ReconcileJob, jobs.Every(time.Minute), nil)
}

// appMiddleware wraps the whole app, pages and API alike, outermost
// first: rate limits, a redirect from www to the bare domain, headers.
// Middleware for the API only goes on the router (r.Use in routes.go).
func appMiddleware() []middleware.Middleware {
	if os.Getenv("LIDZA_MODE") == "dev" {
		return nil
	}
	// FullCalendar creates an empty style element and adds its rules through
	// CSSOM. Allow only that empty element's hash, keeping other inline styles
	// blocked and retaining the default script policy.
	policy := middleware.AddCSP(middleware.DefaultCSP, "style-src", "'sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU='")
	if cfg, err := provider.Load(); err == nil {
		policy = middleware.AddCSP(policy, "script-src", cfg.ServerURL)
		policy = middleware.AddCSP(policy, "frame-src", "'self'", cfg.ServerURL)
		policy = middleware.AddCSP(policy, "connect-src", cfg.ServerURL)
	}
	return []middleware.Middleware{middleware.SecureHeaders(middleware.SecureHeadersOptions{CSP: policy})}
}
