package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/schema"
	"time"
)

func View(ctx context.Context, id string) (schema.TeamSetupView, error) {
	out := schema.TeamSetupView{WorkspaceID: id, Domains: []schema.TeamDomainView{}, Mail: []schema.TeamMailView{}}
	if e := Manage(ctx, id); e != nil {
		return out, e
	}
	out.CanManage = true
	queries := q.New(db.From(ctx))
	w, e := queries.LockWorkspace(ctx, id)
	if e != nil {
		return out, e
	}
	out.Name = w.Name
	setup, e := queries.GetTeamSetup(ctx, id)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	out.Status = setup.Status
	if out.Status == "" {
		out.Status = "draft"
	}
	out.PrimaryDomain = setup.PrimaryDomain
	rows, e := queries.ListTeamDomains(ctx, id)
	if e != nil {
		return out, e
	}
	for _, row := range rows {
		v := DomainView(row)
		if v.Status == "verified" && !Active(row, lidza.Now(ctx)) {
			v.Status = "expired"
		}
		out.Domains = append(out.Domains, v)
	}
	configs, e := queries.ListTeamMailConfigs(ctx, id)
	if e != nil {
		return out, e
	}
	for _, row := range configs {
		var secret MailSecrets
		if e = Open(row.Sealed, &secret); e != nil {
			return out, e
		}
		if secret.WorkspaceID != id || secret.DomainID != row.DomainID {
			return out, errors.New("team secret binding mismatch")
		}
		out.Mail = append(out.Mail, MailView(row, secret))
	}
	return out, nil
}
func Publish(ctx context.Context, id string, in schema.TeamPublishInput) (schema.TeamSetupView, error) {
	tx, queries, e := management(ctx, id)
	if e != nil {
		return schema.TeamSetupView{}, e
	}
	defer tx.Rollback(ctx)
	name, e := Domain(in.PrimaryDomain)
	if e != nil {
		return schema.TeamSetupView{}, e
	}
	d, e := queries.BoundTeamDomain(ctx, name)
	if e != nil || d.WorkspaceID != id || !d.Web || !Active(d, lidza.Now(ctx)) {
		return schema.TeamSetupView{}, router.Errorf(409, "Choose a verified web domain owned by this team")
	}
	rows, e := queries.ListTeamDomains(ctx, id)
	if e != nil {
		return schema.TeamSetupView{}, e
	}
	for _, d := range rows {
		if d.Mail {
			if !Active(d, lidza.Now(ctx)) {
				return schema.TeamSetupView{}, router.Errorf(409, "Verify every configured email domain before publishing")
			}
			c, e := queries.GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: id, DomainID: d.ID})
			if e != nil || !c.Enabled {
				return schema.TeamSetupView{}, router.Errorf(409, "Configure and enable a provider for each email domain")
			}
		}
	}
	boxes, boxErr := queries.ListMailboxes(ctx, id)
	if boxErr != nil {
		return schema.TeamSetupView{}, boxErr
	}
	for _, box := range boxes {
		_, domainName, ok := strings.Cut(strings.ToLower(box.Address), "@")
		d, err := queries.BoundTeamDomain(ctx, domainName)
		if !ok || err != nil || d.WorkspaceID != id || !d.Mail || !Active(d, lidza.Now(ctx)) {
			return schema.TeamSetupView{}, router.Errorf(409, "Assign every existing mailbox's email domain to this team before activating")
		}
		matches, err := queries.FindSmipReceivingMailbox(ctx, box.Address)
		if err != nil {
			return schema.TeamSetupView{}, err
		}
		if len(matches) != 1 {
			return schema.TeamSetupView{}, router.Errorf(409, "Resolve duplicate mailbox addresses before activating this team")
		}
	}
	if _, e = queries.SaveTeamSetup(ctx, q.SaveTeamSetupParams{WorkspaceID: id, PrimaryDomain: name, Status: "ready"}); e != nil {
		return schema.TeamSetupView{}, e
	}
	if e = EnsurePersonalMailboxes(ctx, tx, id); e != nil {
		return schema.TeamSetupView{}, e
	}
	if e = record(ctx, tx, id, "team.setup.publish", id); e != nil {
		return schema.TeamSetupView{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return schema.TeamSetupView{}, e
	}
	return View(ctx, id)
}
func CreateMailbox(ctx context.Context, id string, in schema.TeamMailboxInput) (schema.Mailbox, error) {
	var out schema.Mailbox
	tx, queries, e := management(ctx, id)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	address := strings.ToLower(strings.TrimSpace(in.Address))
	if e = in.Validate(); e != nil {
		return out, e
	}
	_, name, ok := strings.Cut(address, "@")
	if !ok {
		return out, router.Errorf(422, "Enter a valid email address")
	}
	d, e := queries.BoundTeamDomain(ctx, name)
	if e != nil || d.WorkspaceID != id || !d.Mail || !Active(d, lidza.Now(ctx)) {
		return out, router.Errorf(409, "Mailbox address must use this team's verified email domain")
	}
	if !smip.MailAddressAt(address, d.Name) {
		return out, router.Errorf(422, "Use a canonical mailbox address on the configured domain")
	}
	// The domain binding plus workspace lock makes an address exclusive across
	// teams and serializes same-team mailbox creation.
	exists, e := queries.TeamMailboxAddressExists(ctx, address)
	if e != nil {
		return out, e
	}
	if exists {
		return out, router.Errorf(409, "This mailbox address already exists")
	}
	row, e := queries.CreateMailbox(ctx, q.CreateMailboxParams{WorkspaceID: id, Name: strings.TrimSpace(in.Name), Address: address, ConfigPrefix: ""})
	if e != nil {
		return out, e
	}
	if e = record(ctx, tx, id, "team.mailbox.create", row.ID); e != nil {
		return out, e
	}
	return schema.Mailbox{ID: row.ID, WorkspaceID: id, Name: row.Name, Address: row.Address, ConfigPrefix: "", Signature: row.Signature, CreatedAt: row.CreatedAt}, tx.Commit(ctx)
}
func Recheck(ctx context.Context, _ json.RawMessage) error {
	queries := q.New(db.From(ctx))
	rows, e := queries.DueTeamDomains(ctx, ptrTime(lidza.Now(ctx).Add(-24*time.Hour)))
	if e != nil {
		return e
	}
	for _, d := range rows {
		check, cancel := context.WithTimeout(ctx, 8*time.Second)
		values, err := resolver(ctx).LookupTXT(check, "_thura-verify."+d.Name)
		cancel()
		found := false
		for _, v := range values {
			found = found || v == d.Challenge
		}
		// A transient resolver failure does not immediately revoke a domain. The
		// existing seven-day lease is never extended without affirmative proof.
		if err != nil {
			continue
		}
		now := lidza.Now(ctx)
		if !found {
			if e = queries.SuspendTeamDomain(ctx, q.SuspendTeamDomainParams{WorkspaceID: d.WorkspaceID, ID: d.ID, CheckedAt: &now}); e != nil {
				return e
			}
			continue
		}
		if _, e = queries.VerifyTeamDomain(ctx, q.VerifyTeamDomainParams{WorkspaceID: d.WorkspaceID, ID: d.ID, CheckedAt: &now, ValidUntil: ptrTime(now.Add(OwnershipLifetime))}); e != nil {
			return e
		}
	}
	return nil
}
