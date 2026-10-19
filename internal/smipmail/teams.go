package smipmail

import (
	"context"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	"net"
	"net/http"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/tenancy"
)

// A team never inherits another identity or the operator's sending credentials.
// The legacy identity is available only to workspaces without team setup and
// domains that have not been claimed by a team.
func forDomain(ctx context.Context, workspaceID, domain string) (*Gateway, error) {
	queries := q.New(db.From(ctx))
	d, e := queries.BoundTeamDomain(ctx, domain)
	if e == nil {
		if workspaceID != "" && d.WorkspaceID != workspaceID {
			return nil, errors.New("SMIP domain belongs to another team")
		}
		if !d.Mail || !tenancy.Active(d, lidza.Now(ctx)) {
			return nil, errors.New("SMIP domain verification unavailable")
		}
		setup, e := queries.GetTeamSetup(ctx, d.WorkspaceID)
		if e != nil || setup.Status != "ready" {
			return nil, errors.New("team setup is not published")
		}
		row, secret, e := tenancy.LoadMail(ctx, d.WorkspaceID, d.ID)
		if e != nil || !row.Enabled {
			return nil, errors.New("team mail identity disabled")
		}
		g, e := NewGateway(Config{Domain: d.Name, Seed: secret.Seed, NotBefore: secret.NotBefore, NotAfter: secret.NotAfter, Keys: secret.Keys}, From(ctx).ingest)
		if e != nil {
			return nil, e
		}
		parent := From(ctx)
		g.roots = parent.roots
		g.origins = parent.origins
		g.required = parent.required
		g.denied = parent.denied
		g.resolver = parent.resolver
		g.slots = parent.slots
		g.admissions = parent.admissions
		g.teamID = d.WorkspaceID
		g.providerOnly = !row.Smip
		return g, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	if workspaceID != "" {
		_, e = queries.GetTeamSetup(ctx, workspaceID)
		if !errors.Is(e, pgx.ErrNoRows) {
			return nil, errors.New("team sending domain is not verified")
		}
	}
	g := From(ctx)
	if !g.Enabled() || g.domain != domain {
		return nil, errors.New("SMIP mail identity unavailable")
	}
	return g, nil
}
func requestGateway(w http.ResponseWriter, r *http.Request) (*Gateway, bool) {
	host := strings.ToLower(r.Host)
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	target := r.Header.Get("X-Thura-SMIP-Domain")
	scope := tenancy.Host(r.Context())
	if target != "" {
		if scope.Domain != "" && scope.Domain != target {
			http.NotFound(w, r)
			return nil, false
		}
		host = target
	}
	g, e := forDomain(r.Context(), "", host)
	if e != nil && strings.HasPrefix(host, "smip.") {
		g, e = forDomain(r.Context(), "", strings.TrimPrefix(host, "smip."))
	}
	if e != nil && target == "" && tenancy.Host(r.Context()).WorkspaceID == "" && From(r.Context()).Enabled() {
		g = From(r.Context())
		e = nil
	}
	if e != nil || g.providerOnly {
		http.NotFound(w, r)
		return nil, false
	}
	return g, true
}
