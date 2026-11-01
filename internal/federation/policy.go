package federation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/internal/workspace"
	"thura/schema"
)

// MaxPolicyDomains bounds a workspace's domain list.
const MaxPolicyDomains = 200

// policy is a workspace's external sharing policy, read under the
// workspace lock wherever it decides admission or delivery. With no saved
// row it is "Only listed domains" with an empty list: nothing external is
// accepted until an owner or admin chooses otherwise.
type policy struct {
	chat, files schema.SmipSharingMode
	domains     map[string]bool
	blocked     map[string]bool
	updatedBy   string
	updatedAt   *time.Time
}

func loadPolicy(ctx context.Context, queries *q.Queries, w string) (policy, error) {
	p := policy{chat: schema.SmipSharingModeListed, files: schema.SmipSharingModeListed, domains: map[string]bool{}, blocked: map[string]bool{}}
	row, err := queries.GetSmipPolicy(ctx, w)
	if err == nil {
		p.chat, p.files, p.updatedBy, p.updatedAt = schema.SmipSharingMode(row.Chat), schema.SmipSharingMode(row.Files), row.UpdatedBy, &row.UpdatedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	domains, err := queries.ListSmipPolicyDomains(ctx, w)
	if err != nil {
		return p, err
	}
	for _, domain := range domains {
		p.domains[domain] = true
	}
	blocked, err := queries.ListSmipBlockedDomains(ctx, w)
	if err != nil {
		return p, err
	}
	for _, row := range blocked {
		p.blocked[row.Domain] = true
	}
	return p, nil
}

func (p policy) mode(kind string) schema.SmipSharingMode {
	if kind == "file" {
		return p.files
	}
	return p.chat
}

// allows reports whether kind ("chat" or "file") may be exchanged with
// domain through an external conversation. A blocked domain is refused in
// every mode.
func (p policy) allows(kind, domain string) bool {
	if p.blocked[domain] {
		return false
	}
	switch p.mode(kind) {
	case schema.SmipSharingModeAny:
		return true
	case schema.SmipSharingModeListed:
		return p.domains[domain]
	}
	return false
}

// allowsBinding is the policy for a legacy stream binding: an owner or
// admin's explicit consent for one exact peer, stream, sender and recipient
// counts as listing that peer for that stream, so only "Off" and a blocked
// domain stop it.
func (p policy) allowsBinding(kind, domain string) bool {
	return !p.blocked[domain] && p.mode(kind) != schema.SmipSharingModeOff
}

// refusal is the member-facing reason the policy refuses kind with domain.
func (p policy) refusal(kind, domain string) string {
	if kind == "file" {
		return "Your organisation doesn't allow sharing files with " + domain + "."
	}
	return "Your organisation doesn't allow chatting with " + domain + "."
}

// normalizeDomain lowercases and trims a hostname and checks it is a DNS
// name SMIP can use as an identity.
func normalizeDomain(raw string) (string, bool) {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	return domain, smip.ValidDomain(domain)
}

func validMode(mode schema.SmipSharingMode) bool {
	return mode == schema.SmipSharingModeOff || mode == schema.SmipSharingModeListed || mode == schema.SmipSharingModeAny
}

// GetPolicy is the owner/admin view: the policy, the blocked domains and an
// overview of the workspace's external conversations (addresses, state and
// counts; never message content).
func GetPolicy(ctx context.Context, w string) (schema.SmipPolicyView, error) {
	out := schema.SmipPolicyView{Domains: []string{}, Blocked: []schema.SmipBlockedDomainView{}, Conversations: []schema.SmipConversationOverview{}}
	if err := workspace.RequireManager(ctx, w); err != nil {
		return out, err
	}
	g := From(ctx)
	out.Configured, out.Domain = g.Enabled(), g.domain
	queries := q.New(db.From(ctx))
	p, err := loadPolicy(ctx, queries, w)
	if err != nil {
		return out, err
	}
	out.Chat, out.Files, out.UpdatedBy, out.UpdatedAt = p.chat, p.files, p.updatedBy, p.updatedAt
	for domain := range p.domains {
		out.Domains = append(out.Domains, domain)
	}
	sort.Strings(out.Domains)
	blocked, err := queries.ListSmipBlockedDomains(ctx, w)
	if err != nil {
		return out, err
	}
	for _, row := range blocked {
		out.Blocked = append(out.Blocked, schema.SmipBlockedDomainView{Domain: row.Domain, BlockedAt: row.BlockedAt})
	}
	rows, err := queries.ListSmipConversationOverview(ctx, w)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		out.Conversations = append(out.Conversations, schema.SmipConversationOverview{ID: row.ID, LocalAddress: row.LocalAddress, RemoteAddress: row.RemoteAddress, RemoteDomain: row.RemoteDomain, State: row.State, Messages: int(row.Messages), LastActivity: row.UpdatedAt})
	}
	return out, nil
}

// SavePolicy replaces the policy and its domain list under the workspace
// lock, audited in the same transaction.
func SavePolicy(ctx context.Context, w string, in schema.SmipPolicyInput) (schema.SmipPolicyView, error) {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return schema.SmipPolicyView{}, err
	}
	defer tx.Rollback(ctx)
	if !validMode(in.Chat) || !validMode(in.Files) {
		return schema.SmipPolicyView{}, router.Errorf(422, "choose Off, Only listed domains or Any verified domain")
	}
	if len(in.Domains) > MaxPolicyDomains {
		return schema.SmipPolicyView{}, router.Errorf(422, "at most %d domains can be listed", MaxPolicyDomains)
	}
	seen := map[string]bool{}
	var domains []string
	for _, raw := range in.Domains {
		domain, ok := normalizeDomain(raw)
		if !ok {
			return schema.SmipPolicyView{}, router.Errorf(422, "%q is not a domain name such as partner.example", strings.TrimSpace(raw))
		}
		if !seen[domain] {
			seen[domain] = true
			domains = append(domains, domain)
		}
	}
	user := auth.CurrentUser(ctx).ID
	if err = queries.SaveSmipPolicy(ctx, q.SaveSmipPolicyParams{WorkspaceID: w, Chat: q.SmipSharingMode(in.Chat), Files: q.SmipSharingMode(in.Files), UpdatedBy: user, UpdatedAt: lidza.Now(ctx)}); err != nil {
		return schema.SmipPolicyView{}, err
	}
	if err = queries.ClearSmipPolicyDomains(ctx, w); err != nil {
		return schema.SmipPolicyView{}, err
	}
	for _, domain := range domains {
		if err = queries.AddSmipPolicyDomain(ctx, q.AddSmipPolicyDomainParams{WorkspaceID: w, Domain: domain}); err != nil {
			return schema.SmipPolicyView{}, err
		}
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.policy.update", Scope: w, Resource: "smip-policy/" + w, Meta: map[string]string{"chat": string(in.Chat), "files": string(in.Files), "domains": fmt.Sprint(len(domains))}}); err != nil {
		return schema.SmipPolicyView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipPolicyView{}, err
	}
	return GetPolicy(ctx, w)
}

// UnblockDomain removes a domain an owner or admin blocked from a request.
func UnblockDomain(ctx context.Context, w string, in schema.SmipDomainInput) (schema.SmipPolicyView, error) {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return schema.SmipPolicyView{}, err
	}
	defer tx.Rollback(ctx)
	domain, ok := normalizeDomain(in.Domain)
	if !ok {
		return schema.SmipPolicyView{}, router.Errorf(422, "enter a domain name such as partner.example")
	}
	n, err := queries.UnblockSmipDomain(ctx, q.UnblockSmipDomainParams{WorkspaceID: w, Domain: domain})
	if err != nil {
		return schema.SmipPolicyView{}, err
	}
	if n == 0 {
		return schema.SmipPolicyView{}, router.Errorf(404, "that domain is not blocked")
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.domain.unblock", Scope: w, Resource: "smip-policy/" + w, Meta: map[string]string{"domain": domain}}); err != nil {
		return schema.SmipPolicyView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipPolicyView{}, err
	}
	return GetPolicy(ctx, w)
}
