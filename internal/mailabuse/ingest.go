package mailabuse

import (
	"context"
	"encoding/json"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/schema"
)

const ScreenJob = "thura.mail.screen"
const ReconcileJob = "thura.mail.screen.reconcile"

func people(ctx context.Context, queries *q.Queries, w string) ([]Person, error) {
	contacts, err := queries.MailKnownContacts(ctx, w)
	if err != nil {
		return nil, err
	}
	accounts, err := queries.MailKnownAccounts(ctx, w)
	if err != nil {
		return nil, err
	}
	out := []Person{}
	for _, p := range contacts {
		out = append(out, Person{p.Name, p.Email})
	}
	for _, p := range accounts {
		out = append(out, Person{p.Name, p.Email})
	}
	return out, nil
}
func Preflight(ctx context.Context, queries *q.Queries, box q.Mailbox, from string, authenticated bool) (q.MailProtection, error) {
	p, err := Policy(ctx, queries, box.WorkspaceID)
	if err != nil {
		return p, err
	}
	sender, domain, _ := address(from)
	if sender == "" {
		sender = "invalid"
		domain = "invalid"
	}
	bucket := lidza.Now(ctx).UTC().Truncate(time.Hour)
	for _, limit := range []struct {
		key string
		n   int32
	}{{"domain:" + identity(registrable(domain), authenticated), p.DomainPerHour}, {"sender:" + identity(sender, authenticated), p.SenderPerHour}} {
		count, err := queries.MailRate(ctx, q.MailRateParams{WorkspaceID: box.WorkspaceID, Key: limit.key, Bucket: bucket})
		if err != nil {
			return p, err
		}
		if count > limit.n {
			return p, router.Errorf(429, "Incoming mail rate limit reached; retry later")
		}
	}
	return p, nil
}
func Enqueue(ctx context.Context, tx pgx.Tx, box q.Mailbox, item q.MailItem, p q.MailProtection, authenticated bool, files []File) (q.MailItem, error) {
	queries := q.New(tx)
	directory, err := people(ctx, queries, box.WorkspaceID)
	if err != nil {
		return item, err
	}
	e := Analyze(item.FromAddress, item.TextBody, item.HTMLBody, directory, files)
	sender, domain, _ := address(item.FromAddress)
	id := identity(sender, authenticated)
	rep, _, err := reputation(ctx, queries, box.WorkspaceID, id)
	if err != nil {
		return item, err
	}
	if rep == "blocked" {
		e.add("blocked_sender", "danger", "Sender is blocked by this workspace", 100)
	}
	if rep == "poor" || rep == "poor-domain" {
		e.add("sender_complaints", "danger", "Sender or domain has repeated workspace spam complaints", 80)
	}
	known := rep == "trusted"
	if authenticated {
		for _, person := range directory {
			email, _, _ := address(person.Email)
			known = known || email == sender
		}
	}
	cfg, err := configuration()
	if err != nil {
		return item, err
	}
	ai, av := "disabled", "not_configured"
	if p.AiEnabled {
		ai = "pending"
	}
	if cfg.Clamd != "" {
		av = "pending"
	}
	state := "clean"
	if e.Score >= int(p.SpamThreshold) || p.ApproveUnknown && !known {
		state = "quarantined"
	}
	if ai == "pending" || av == "pending" {
		state = "pending"
	}
	findingJSON, err := json.Marshal(e.Findings)
	if err != nil {
		return item, err
	}
	if _, err = queries.AddMailScreening(ctx, q.AddMailScreeningParams{ItemID: item.ID, WorkspaceID: box.WorkspaceID, Sender: sender, Domain: registrable(domain), Identity: id, Authenticated: authenticated, State: state, Score: int32(e.Score), Findings: string(findingJSON), AiState: ai, AvState: av, ApprovalRequired: p.ApproveUnknown && !known, PolicyRevision: p.Revision, NextAttempt: lidza.Now(ctx)}); err != nil {
		return item, err
	}
	if state != "clean" {
		item.Folder = q.MailFolderSpam
		if err = queries.MoveScreenedMail(ctx, q.MoveScreenedMailParams{ID: item.ID, Folder: item.Folder}); err != nil {
			return item, err
		}
	}
	if state == "pending" {
		_, err = jobs.From(ctx).EnqueueTx(ctx, tx, ScreenJob, schema.MailScreenJob{ID: item.ID}, jobs.Unique(item.ID))
	}
	return item, err
}
func parseFindings(raw string) ([]schema.MailFinding, error) {
	var out []schema.MailFinding
	err := json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = []schema.MailFinding{}
	}
	return out, err
}
func terminal(s q.MailScreening) bool {
	return s.State == "clean" || s.State == "quarantined" || s.Released
}
