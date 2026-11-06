package app

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"thura/internal/calendar"
	"thura/internal/chat"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/middleware"
	"thura/internal/drive"
	"thura/internal/federation"
	"thura/internal/mailabuse"
	"thura/internal/mailbox"
	"thura/internal/meet"
	"thura/internal/platform/objectgc"
	"thura/internal/platform/objectstore"
	livekit "thura/internal/providers/livekit"
	"thura/internal/setup"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
)

// onStart runs after the packs have started and before the app listens:
// register job handlers (jobs.FromServices(s).Handle(kind, fn)), warm a
// cache, lidza.Provide a service the packs do not. main.go is generated
// once and left alone; this file is the app's.
func onStart(ctx context.Context, s *lidza.Services) error {
	tenancy.Configure(s, setup.Allow)
	jobs.FromServices(s).Handle(tenancy.RecheckJob, tenancy.Recheck, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(tenancy.RecheckJob, jobs.Every(time.Hour), nil); err != nil {
		return err
	}
	if err := setup.Activate(lidza.WithServices(ctx, s), s); err != nil {
		return err
	}
	if err := objectstore.Configure(lidza.WithServices(ctx, s), s); err != nil {
		return err
	}
	if err := smipmail.Configure(s, mailbox.ReceiveSMIP); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(smipmail.DispatchJob, smipmail.Dispatch, jobs.Concurrency(2))
	jobs.FromServices(s).Handle(smipmail.ReconcileJob, smipmail.Reconcile, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(smipmail.ReconcileJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	if err := federation.Configure(s); err != nil {
		return err
	}
	gateway := federation.From(lidza.WithServices(ctx, s))
	if gateway.Enabled() {
		jobs.FromServices(s).Handle(gateway.DispatchKind(), federation.Dispatch, jobs.Concurrency(2))
		jobs.FromServices(s).Handle(gateway.ReconcileKind(), federation.Reconcile, jobs.Concurrency(1))
		if err := jobs.FromServices(s).Schedule(gateway.ReconcileKind(), jobs.Every(time.Minute), nil); err != nil {
			return err
		}
	}
	jobs.FromServices(s).Handle(mailabuse.ScreenJob, mailabuse.Screen, jobs.Concurrency(1))
	jobs.FromServices(s).Handle(mailabuse.FeedbackJob, mailabuse.LearnFeedback, jobs.Concurrency(1))
	jobs.FromServices(s).Handle(mailabuse.ReconcileJob, mailabuse.Reconcile, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(mailabuse.ReconcileJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(mailbox.DeliveryJob, mailbox.Deliver)
	jobs.FromServices(s).Handle(calendar.ReminderJob, calendar.Remind, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(calendar.ReminderJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(chat.ReconcileJob, chat.Reconcile, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(chat.ReconcileJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(meet.ReconcileJob, meet.Reconcile, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(meet.ReconcileJob, jobs.Every(time.Minute), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(drive.PreviewJob, drive.GeneratePreview, jobs.Concurrency(1))
	jobs.FromServices(s).Handle(drive.CleanupJob, drive.Cleanup, jobs.Concurrency(1))
	if err := jobs.FromServices(s).Schedule(drive.CleanupJob, jobs.Every(time.Hour), nil); err != nil {
		return err
	}
	jobs.FromServices(s).Handle(objectgc.DeleteJob, objectgc.Delete, jobs.Concurrency(2))
	jobs.FromServices(s).Handle(objectgc.SweepJob, objectgc.Sweep, jobs.Concurrency(2))
	return jobs.FromServices(s).Schedule(objectgc.SweepJob, jobs.Every(24*time.Hour), nil)
}

// appMiddleware wraps the whole app, pages and API alike, outermost
// first: rate limits, a redirect from www to the bare domain, headers.
// Middleware for the API only goes on the router (r.Use in routes.go).
func appMiddleware() []middleware.Middleware {
	if os.Getenv("LIDZA_MODE") == "dev" {
		return []middleware.Middleware{setup.Gate, tenancy.Middleware}
	}
	return []middleware.Middleware{setup.Gate, tenancy.Middleware, liveHeaders(livekit.Load, time.Second)}
}

// headerState is the security-header handler built for one LiveKit state:
// whether Meet is configured and the origins its browser connects to.
type headerState struct {
	handler   http.Handler
	ok        bool
	origins   string
	checkedAt time.Time
}

// liveHeaders follows the LiveKit settings without a restart: a request
// reuses the handler built for the current settings, and at most once per
// interval one request reloads them, rebuilding the handler only when they
// changed. The hot path is an atomic load and a clock read.
func liveHeaders(load func() (livekit.Config, error), interval time.Duration) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		var current atomic.Pointer[headerState]
		var mu sync.Mutex
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := current.Load()
			if now := time.Now(); state == nil || now.Sub(state.checkedAt) >= interval {
				mu.Lock()
				if state = current.Load(); state == nil || now.Sub(state.checkedAt) >= interval {
					cfg, err := load()
					fresh := &headerState{ok: err == nil, checkedAt: now}
					if fresh.ok {
						fresh.origins = strings.Join(cfg.PublicOrigins(), " ")
					}
					if state != nil && state.ok == fresh.ok && state.origins == fresh.origins {
						fresh.handler = state.handler
					} else {
						fresh.handler = appHeaders(cfg, fresh.ok)[0](next)
					}
					current.Store(fresh)
					state = fresh
				}
				mu.Unlock()
			}
			state.handler.ServeHTTP(w, r)
		})
	}
}

// readerStyleHashes are the SHA-256 hashes of htmlStyle and markdownStyle in
// src/components/MailReader.tsx. Update them with the strings;
// e2e/mail-markdown.spec.ts fails when the reader's stylesheet is blocked.
// A function, not a package-level slice: callers get a fresh copy and no
// node-local state can grow or be mutated.
func readerStyleHashes() []string {
	return []string{
		"'sha256-/x4nHdW7FHYkGp+z6wuAlwCvSBuTvUb4HTk9kYJiI80='",
		"'sha256-gLs+gvFSQCGCwwUgQPLt43lJEGzX92GKSDW4bj+iHGo='",
	}
}

func appHeaders(cfg livekit.Config, meet bool) []middleware.Middleware {
	// FullCalendar creates an empty style element and adds its rules through
	// CSSOM. Allow only that empty element's hash, keeping other inline styles
	// blocked and retaining the default script policy.
	policy := middleware.AddCSP(middleware.DefaultCSP, "style-src", "'sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU='")
	// The mail reader's sandboxed srcdoc frame inherits this policy, so its two
	// fixed stylesheets (src/components/MailReader.tsx) are admitted by hash.
	policy = middleware.AddCSP(policy, "style-src", readerStyleHashes()...)
	policy = middleware.AddCSP(policy, "frame-src", "'self'")
	// FullCalendar ships its arrow icon font inline as a data: URL; without it
	// the toolbar's previous/next buttons render empty boxes. Fonts cannot run
	// script and style-src stays restricted, so data: fonts add no script path.
	policy = middleware.AddCSP(policy, "font-src", "data:")
	permissions := middleware.DefaultPermissionsPolicy
	if meet {
		policy = middleware.AddCSP(policy, "connect-src", cfg.PublicOrigins()...)
		policy = middleware.AddCSP(policy, "media-src", "'self'", "blob:")
		policy = middleware.AddCSP(policy, "style-src-attr", "'unsafe-inline'")
		permissions = "camera=(self), microphone=(self), display-capture=(self), geolocation=()"
	}
	return []middleware.Middleware{middleware.SecureHeaders(middleware.SecureHeadersOptions{CSP: policy, PermissionsPolicy: permissions})}
}
