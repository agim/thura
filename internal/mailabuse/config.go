// Package mailabuse screens all inbound transports through the same durable policy.
package mailabuse

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

type Config struct {
	AIScreen  bool   `env:"MAIL_AI_SCREENING" default:"true"`
	Clamd     string `env:"MAIL_CLAMD_ADDRESS"`
	AIPerHour int    `env:"MAIL_AI_PER_HOUR" default:"120"`
}

func configuration() (Config, error) { var c Config; err := env.Load(".", &c); return c, err }
func Policy(ctx context.Context, queries *q.Queries, w string) (q.MailProtection, error) {
	cfg, err := configuration()
	if err != nil {
		return q.MailProtection{}, err
	}
	if err = queries.EnsureMailProtection(ctx, q.EnsureMailProtectionParams{WorkspaceID: w, AiEnabled: cfg.AIScreen}); err != nil {
		return q.MailProtection{}, err
	}
	return queries.GetMailProtection(ctx, w)
}
func manager(ctx context.Context, queries *q.Queries, w, subject string) (bool, error) {
	role, err := queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return role == "owner" || role == "admin", err
}
func save(ctx context.Context, queries *q.Queries, s q.MailScreening) error {
	return queries.SaveMailScreening(ctx, q.SaveMailScreeningParams{ItemID: s.ItemID, State: s.State, Score: s.Score, Findings: s.Findings, AiState: s.AiState, AvState: s.AvState, ApprovalRequired: s.ApprovalRequired, Released: s.Released, Attempts: s.Attempts, AttemptID: s.AttemptID, PolicyRevision: s.PolicyRevision, NextAttempt: s.NextAttempt, UpdatedAt: lidza.Now(ctx)})
}
func Settings(ctx context.Context, w, subject string) (schema.MailProtectionView, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.MailProtectionView{}, err
	}
	queries := q.New(db.From(ctx))
	p, err := Policy(ctx, queries, w)
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	can, err := manager(ctx, queries, w, subject)
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	cfg, err := configuration()
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	return schema.MailProtectionView{AiEnabled: p.AiEnabled, ApproveUnknown: p.ApproveUnknown, SenderPerHour: int(p.SenderPerHour), DomainPerHour: int(p.DomainPerHour), SpamThreshold: int(p.SpamThreshold), Revision: int(p.Revision), CanConfigure: can, AiReady: aiReady(ctx), AntivirusConfigured: cfg.Clamd != ""}, nil
}
func SaveSettings(ctx context.Context, w, subject string, in schema.MailProtectionInput) (schema.MailProtectionView, error) {
	if err := in.Validate(); err != nil {
		return schema.MailProtectionView{}, router.Errorf(422, "Invalid mail protection settings")
	}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.MailProtectionView{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.MailProtectionView{}, err
	}
	can, err := manager(ctx, queries, w, subject)
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	if !can {
		return schema.MailProtectionView{}, router.Errorf(403, "Workspace administrator required")
	}
	if _, err = Policy(ctx, queries, w); err != nil {
		return schema.MailProtectionView{}, err
	}
	_, err = queries.SaveMailProtection(ctx, q.SaveMailProtectionParams{WorkspaceID: w, AiEnabled: in.AiEnabled, ApproveUnknown: in.ApproveUnknown, SenderPerHour: int32(in.SenderPerHour), DomainPerHour: int32(in.DomainPerHour), SpamThreshold: int32(in.SpamThreshold), Revision: int32(in.Revision)})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.MailProtectionView{}, router.Errorf(409, "Settings changed; reload before saving")
	}
	if err != nil {
		return schema.MailProtectionView{}, err
	}
	if err = record(ctx, tx, w, subject, "mail.protection.update", w); err != nil {
		return schema.MailProtectionView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.MailProtectionView{}, err
	}
	return Settings(ctx, w, subject)
}
func reputation(ctx context.Context, queries *q.Queries, w, identity string) (string, int, error) {
	policy, err := queries.GetMailSenderPolicy(ctx, q.GetMailSenderPolicyParams{WorkspaceID: w, Identity: identity})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", 0, err
	}
	count, err := queries.MailComplaintCount(ctx, q.MailComplaintCountParams{WorkspaceID: w, Identity: identity})
	if err != nil {
		return "", 0, err
	}
	decision := policy.Decision
	if decision == "" {
		decision = "neutral"
	}
	if decision != "blocked" && count >= 3 {
		decision = "poor"
	}
	_, domain, _ := address(strings.TrimPrefix(strings.TrimPrefix(identity, "smip:"), "unverified:"))
	if domain != "" {
		domainCount, err := queries.MailDomainComplaintCount(ctx, q.MailDomainComplaintCountParams{WorkspaceID: w, Domain: registrable(domain), Authenticated: strings.HasPrefix(identity, "smip:")})
		if err != nil {
			return "", 0, err
		}
		if decision != "blocked" && domainCount >= 10 {
			decision = "poor-domain"
		}
	}
	return decision, int(count), nil
}
func GuardAttachment(ctx context.Context, item string) error {
	s, err := q.New(db.From(ctx)).GetMailScreening(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.State != "clean" && !s.Released {
		return router.Errorf(403, "Attachment is quarantined until required checks and review complete")
	}
	if s.AvState == "infected" {
		return router.Errorf(403, "Malware detected; attachment access is blocked")
	}
	return nil
}

const retryDelay = time.Minute

func GuardObject(ctx context.Context, key string) error {
	blocked, err := q.New(db.From(ctx)).QuarantinedMailObject(ctx, key)
	if err != nil {
		return err
	}
	if blocked {
		return router.Errorf(403, "This attachment is quarantined")
	}
	return nil
}
