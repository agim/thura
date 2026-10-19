// Package tenancy owns verified team domains and sealed per-domain mail settings.
package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/schema"
)

const RecheckJob = "thura.teams.recheck-domains"
const OwnershipLifetime = 7 * 24 * time.Hour

type Resolver interface {
	LookupTXT(context.Context, string) ([]string, error)
}
type DNS struct{ Resolver Resolver }
type Administrator struct{ Allow func(context.Context) bool }

func Configure(s *lidza.Services, allow func(context.Context) bool) {
	lidza.Provide(s, DNS{Resolver: net.DefaultResolver})
	lidza.Provide(s, Administrator{Allow: allow})
}
func resolver(ctx context.Context) Resolver {
	if d, ok := lidza.Optional[DNS](ctx); ok && d.Resolver != nil {
		return d.Resolver
	}
	return net.DefaultResolver
}
func Domain(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if !smip.ValidDomain(name) || strings.Contains(name, ":") || net.ParseIP(name) != nil || name == "localhost" || strings.HasSuffix(name, ".localhost") || strings.HasSuffix(name, ".local") {
		return "", router.Errorf(422, "Enter a DNS domain such as team.example.com, without a URL or port")
	}
	return name, nil
}
func Active(d q.TeamDomain, now time.Time) bool {
	return d.Status == "verified" && (d.Verification == "administrator" || d.ValidUntil != nil && d.ValidUntil.After(now))
}
func Manage(ctx context.Context, id string) error {
	u := auth.CurrentUser(ctx)
	if u == nil {
		return router.Errorf(401, "Sign in required")
	}
	if !validID(id) || !Allows(ctx, id) {
		return router.Errorf(404, "Team not found")
	}
	admin, ok := lidza.Optional[Administrator](ctx)
	if !ok || admin.Allow == nil || !admin.Allow(ctx) {
		return router.Errorf(403, "Only the server administrator can configure teams and domains")
	}
	_, e := q.New(db.From(ctx)).LockWorkspace(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return router.Errorf(404, "Team not found")
	}
	return e
}
func validID(id string) bool { _, e := pgUUID(id); return e == nil }
func pgUUID(id string) (string, error) {
	if len(id) != 36 {
		return "", errors.New("invalid UUID")
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", errors.New("invalid UUID")
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return "", errors.New("invalid UUID")
		}
	}
	return id, nil
}

// Serialize configuration with membership changes. Authorization is rechecked
// after acquiring the workspace lock; revoked administrators cannot commit.
func management(ctx context.Context, id string) (pgx.Tx, *q.Queries, error) {
	if e := Manage(ctx, id); e != nil {
		return nil, nil, e
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return nil, nil, e
	}
	queries := q.New(tx)
	if _, e = queries.LockWorkspace(ctx, id); e == nil {
		admin, ok := lidza.Optional[Administrator](ctx)
		if !ok || admin.Allow == nil || !admin.Allow(ctx) {
			e = router.Errorf(403, "Server administrator required")
		}
	}
	if e != nil {
		return nil, nil, errors.Join(e, tx.Rollback(ctx))
	}
	return tx, queries, nil
}
func DomainView(d q.TeamDomain) schema.TeamDomainView {
	return schema.TeamDomainView{ID: d.ID, Name: d.Name, Status: d.Status, Web: d.Web, Mail: d.Mail, TxtName: "_thura-verify." + d.Name, TxtValue: d.Challenge, VerifiedAt: d.VerifiedAt, ValidUntil: d.ValidUntil}
}
func AddDomain(ctx context.Context, id string, in schema.TeamDomainInput) (schema.TeamDomainView, error) {
	var out schema.TeamDomainView
	name, e := Domain(in.Name)
	if e != nil {
		return out, e
	}
	if reserved(ctx, name) {
		return out, router.Errorf(409, "The deployment host cannot be assigned to a team")
	}
	if !in.Web && !in.Mail {
		return out, router.Errorf(422, "Choose web, email, or both")
	}
	tx, queries, e := management(ctx, id)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = queries.EnsureTeamSetup(ctx, id); e != nil {
		return out, e
	}
	rows, e := queries.ListTeamDomains(ctx, id)
	if e != nil {
		return out, e
	}
	if len(rows) >= 25 {
		return out, router.Errorf(409, "A team may configure up to 25 domains")
	}
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return out, e
	}
	row, e := queries.AddTeamDomain(ctx, q.AddTeamDomainParams{WorkspaceID: id, Name: name, Challenge: "thura-verification=" + base64.RawURLEncoding.EncodeToString(raw), Web: in.Web, Mail: in.Mail})
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return out, router.Errorf(409, "This domain is already in your team")
	}
	if e != nil {
		return out, e
	}
	if e = LockDomain(ctx, tx, row.Name); e != nil {
		return out, e
	}
	if e = bindDomain(ctx, queries, id, row); e != nil {
		return out, e
	}
	row, e = queries.ApproveTeamDomain(ctx, q.ApproveTeamDomainParams{WorkspaceID: id, ID: row.ID})
	if e != nil {
		return out, e
	}
	if e = record(ctx, tx, id, "team.domain.assign", row.ID); e != nil {
		return out, e
	}
	return DomainView(row), tx.Commit(ctx)
}
func Verify(ctx context.Context, id, domainID string) (schema.TeamDomainView, error) {
	var out schema.TeamDomainView
	if e := Manage(ctx, id); e != nil {
		return out, e
	}
	if !validID(domainID) {
		return out, router.Errorf(404, "Domain not found")
	}
	snapshot, e := q.New(db.From(ctx)).GetTeamDomain(ctx, q.GetTeamDomainParams{WorkspaceID: id, ID: domainID})
	if errors.Is(e, pgx.ErrNoRows) {
		return out, router.Errorf(404, "Domain not found")
	}
	if e != nil {
		return out, e
	}
	// Do not hold a transaction or DB connection while waiting on DNS.
	check, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	values, e := resolver(ctx).LookupTXT(check, "_thura-verify."+snapshot.Name)
	found := false
	for _, v := range values {
		found = found || v == snapshot.Challenge
	}
	if e != nil || !found {
		return out, router.Errorf(422, "Verification TXT record not found. Keep the exact record published and try again after DNS updates")
	}
	tx, queries, e := management(ctx, id)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	row, e := queries.LockTeamDomain(ctx, q.LockTeamDomainParams{WorkspaceID: id, ID: domainID})
	if e != nil {
		return out, e
	}
	if row.Challenge != snapshot.Challenge {
		return out, router.Errorf(409, "Verification changed; try again")
	}
	if e = LockDomain(ctx, tx, row.Name); e != nil {
		return out, e
	}
	if e = bindDomain(ctx, queries, id, row); e != nil {
		return out, e
	}
	now := lidza.Now(ctx)
	row, e = queries.VerifyTeamDomain(ctx, q.VerifyTeamDomainParams{WorkspaceID: id, ID: row.ID, CheckedAt: &now, ValidUntil: ptrTime(now.Add(OwnershipLifetime))})
	if e != nil {
		return out, e
	}
	if e = record(ctx, tx, id, "team.domain.verify", row.ID); e != nil {
		return out, e
	}
	return DomainView(row), tx.Commit(ctx)
}
func ptrTime(v time.Time) *time.Time { return &v }
func record(ctx context.Context, tx pgx.Tx, id, action, resource string) error {
	return audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: action, Scope: id, Resource: resource})
}
func Remove(ctx context.Context, id, domainID string) error {
	tx, queries, e := management(ctx, id)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if !validID(domainID) {
		return router.Errorf(404, "Domain not found")
	}
	row, e := queries.GetTeamDomain(ctx, q.GetTeamDomainParams{WorkspaceID: id, ID: domainID})
	if e != nil {
		return router.Errorf(404, "Domain not found")
	}
	if e = queries.UnbindTeamDomain(ctx, q.UnbindTeamDomainParams{WorkspaceID: id, DomainID: domainID}); e != nil {
		return e
	}
	if e = queries.DeleteTeamMailConfig(ctx, q.DeleteTeamMailConfigParams{WorkspaceID: id, DomainID: domainID}); e != nil {
		return e
	}
	if e = queries.DeleteTeamDomain(ctx, q.DeleteTeamDomainParams{WorkspaceID: id, ID: domainID}); e != nil {
		return e
	}
	setup, e := queries.GetTeamSetup(ctx, id)
	if e == nil && setup.PrimaryDomain == row.Name {
		if _, e = queries.SaveTeamSetup(ctx, q.SaveTeamSetupParams{WorkspaceID: id, Status: "draft", PrimaryDomain: ""}); e != nil {
			return e
		}
	}
	if e = record(ctx, tx, id, "team.domain.remove", domainID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func Seal(v any) (string, error) {
	key, e := credentials.Key(".")
	if e != nil {
		return "", router.Errorf(503, "Server encryption key unavailable")
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return credentials.Encrypt(key, raw)
}
func Open(raw string, v any) error {
	key, e := credentials.Key(".")
	if e != nil {
		return router.Errorf(503, "Server encryption key unavailable")
	}
	plain, e := credentials.Decrypt(key, raw)
	if e != nil {
		return errors.New("team configuration could not be decrypted")
	}
	return json.Unmarshal(plain, v)
}
func PrimaryURL(ctx context.Context, id string) string {
	queries := q.New(db.From(ctx))
	s, e := queries.GetTeamSetup(ctx, id)
	if e == nil && s.PrimaryDomain != "" {
		d, e := queries.BoundTeamDomain(ctx, s.PrimaryDomain)
		if e == nil && d.WorkspaceID == id && d.Web && Active(d, lidza.Now(ctx)) {
			return "https://" + d.Name
		}
	}
	var cfg struct {
		URL string `env:"APP_URL"`
	}
	if e := env.Load(".", &cfg); e != nil {
		return ""
	}
	return strings.TrimRight(cfg.URL, "/")
}
func Link(ctx context.Context, id, path string) string { return PrimaryURL(ctx, id) + path }
func reserved(ctx context.Context, name string) bool {
	var cfg struct {
		URL string `env:"APP_URL"`
	}
	if e := env.Load(".", &cfg); e != nil {
		return true
	}
	u, e := url.Parse(cfg.URL)
	return e != nil || u != nil && strings.EqualFold(u.Hostname(), name)
}

func bindDomain(ctx context.Context, queries *q.Queries, id string, row q.TeamDomain) error {
	if strings.HasPrefix(row.Name, "smip.") {
		parent, e := queries.BoundTeamDomain(ctx, strings.TrimPrefix(row.Name, "smip."))
		if e == nil && parent.Mail && parent.WorkspaceID != id {
			return router.Errorf(409, "This hostname is reserved for another team's SMIP mail")
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
	}
	if row.Mail {
		child, e := queries.BoundTeamDomain(ctx, "smip."+row.Name)
		if e == nil && child.WorkspaceID != id {
			return router.Errorf(409, "The SMIP hostname is assigned to another team")
		}
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
	}

	conflicts, e := queries.ConflictingTeamDomainMailboxes(ctx, q.ConflictingTeamDomainMailboxesParams{WorkspaceID: id, Address: row.Name})
	if e != nil {
		return e
	}
	if conflicts > 0 {
		return router.Errorf(409, "Another team has mailboxes on this domain; resolve the assignment first")
	}
	n, e := queries.BindTeamDomain(ctx, q.BindTeamDomainParams{Name: row.Name, WorkspaceID: id, DomainID: row.ID})
	if e != nil {
		return e
	}
	if n != 1 {
		return router.Errorf(409, "This domain is assigned to another team")
	}
	return nil
}

// LockDomain serializes assignments against operator mailbox provisioning and
// reserves the derived smip.<domain> namespace without locking another team.
func LockDomain(ctx context.Context, tx pgx.Tx, name string) error {
	names := []string{name}
	if strings.HasPrefix(name, "smip.") {
		names = append(names, strings.TrimPrefix(name, "smip."))
	}
	sort.Strings(names)
	for _, key := range names {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "team-domain:"+key); e != nil {
			return e
		}
	}
	return nil
}
