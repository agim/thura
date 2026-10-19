package tenancy

import (
	"context"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	q "thura/db/queries/gen"
	"thura/schema"
)

type hostKey struct{}

func Host(ctx context.Context) schema.TenantContext {
	v, _ := ctx.Value(hostKey{}).(schema.TenantContext)
	return v
}
func Allows(ctx context.Context, id string) bool {
	h := Host(ctx)
	return h.WorkspaceID == "" || h.WorkspaceID == id
}
func hostName(raw string) string {
	if h, _, e := net.SplitHostPort(raw); e == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(raw)
}

// Bind only the HTTP Host supplied by the trusted ingress. Forwarded/Origin
// headers never select a team. TLS must terminate at the documented ingress.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostName(r.Host)
		var cfg struct {
			URL string `env:"APP_URL"`
		}
		if e := env.Load(".", &cfg); e != nil {
			http.Error(w, "Host configuration unavailable", http.StatusServiceUnavailable)
			return
		}
		u, parseErr := url.Parse(cfg.URL)
		if parseErr != nil {
			http.Error(w, "Host configuration unavailable", http.StatusServiceUnavailable)
			return
		}
		allowed := false
		for _, alias := range strings.Split(os.Getenv("THURA_PUBLIC_HOSTS"), ",") {
			allowed = allowed || strings.EqualFold(strings.TrimSpace(alias), host) && host != ""
		}
		ip := net.ParseIP(host)
		if os.Getenv("LIDZA_MODE") == "dev" && ip != nil && ip.IsPrivate() {
			allowed = true
		}
		if allowed || (u != nil && strings.EqualFold(u.Hostname(), host)) || host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		// Health probes do not expose tenant data and may use an internal hostname.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		d, e := q.New(db.From(r.Context())).BoundTeamDomain(r.Context(), host)
		if errors.Is(e, pgx.ErrNoRows) && strings.HasPrefix(host, "smip.") && strings.HasPrefix(r.URL.Path, "/api/v1/smip/v0.2/") {
			d, e = q.New(db.From(r.Context())).BoundTeamDomain(r.Context(), strings.TrimPrefix(host, "smip."))
		}
		if errors.Is(e, pgx.ErrNoRows) {
			http.Error(w, "This domain is not configured for Thura", 421)
			return
		}
		if e != nil {
			http.Error(w, "Team domain unavailable", http.StatusServiceUnavailable)
			return
		}
		if !Active(d, lidza.Now(r.Context())) {
			http.Error(w, "Team domain verification has expired or been suspended", 421)
			return
		}
		if !d.Web && !strings.HasPrefix(r.URL.Path, "/api/v1/smip/v0.2/") {
			http.Error(w, "This domain is configured for email only", 421)
			return
		}
		setup, setupErr := q.New(db.From(r.Context())).GetTeamSetup(r.Context(), d.WorkspaceID)
		if setupErr != nil || setup.Status != "ready" {
			http.Error(w, "The administrator has not activated this team yet", http.StatusServiceUnavailable)
			return
		}
		// Every workspace API is bound to this host, even for an account belonging
		// to several teams. Authentication and membership are still required.
		if strings.HasPrefix(r.URL.Path, "/api/v1/workspaces/") {
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"), "/")[0]
			if id != d.WorkspaceID {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Add("Vary", "Host")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), hostKey{}, schema.TenantContext{WorkspaceID: d.WorkspaceID, Domain: d.Name})))
	})
}
