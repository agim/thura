package tenancy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
	q "thura/db/queries/gen"
)

var unsafeMailboxLocal = regexp.MustCompile(`[^a-z0-9._+-]+`)

// Existing members also receive personal inboxes on upgrade. Stable suffixes
// resolve address/alias collisions without stealing or exposing an old inbox.
func EnsurePersonalMailboxes(ctx context.Context, tx pgx.Tx, id string) error {
	queries := q.New(tx)
	domains, err := queries.ListTeamDomains(ctx, id)
	if err != nil {
		return err
	}
	domain := ""
	for _, d := range domains {
		if d.Mail && Active(d, lidza.Now(ctx)) {
			c, e := queries.GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: id, DomainID: d.ID})
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && c.Enabled {
				domain = d.Name
				break
			}
		}
	}
	if domain == "" {
		return nil
	}
	if err = LockDomain(ctx, tx, domain); err != nil {
		return err
	}
	members, err := queries.ListWorkspaceMembers(ctx, id)
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.MailboxAddress != "" {
			continue
		}
		local, _, _ := strings.Cut(strings.ToLower(member.Email), "@")
		local = strings.Trim(unsafeMailboxLocal.ReplaceAllString(local, "-"), ".-+")
		if len(local) > 40 {
			local = local[:40]
		}
		if local == "" {
			local = "member"
		}
		hash := sha256.Sum256([]byte(id + ":" + member.Subject))
		suffix := hex.EncodeToString(hash[:8])
		address := local + "@" + domain
		exists, e := queries.TeamMailboxAddressExists(ctx, address)
		if e != nil {
			return e
		}
		if exists {
			address = local + "-" + suffix + "@" + domain
		}
		exists, e = queries.TeamMailboxAddressExists(ctx, address)
		if e != nil {
			return e
		}
		if exists {
			return fmt.Errorf("personal mailbox address conflict requires administrator review")
		}
		name := member.Name
		if name == "" {
			name = address
		}
		box, e := queries.ReservePersonalMailbox(ctx, q.ReservePersonalMailboxParams{WorkspaceID: id, Name: name, Address: address})
		if e != nil {
			return e
		}
		n, e := queries.AssignPersonalMailbox(ctx, q.AssignPersonalMailboxParams{WorkspaceID: id, ID: box.ID, Subject: &member.Subject})
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("personal mailbox ownership was not assigned")
		}
		if e = audit.From(ctx).RecordTx(audit.System(ctx, "mailbox-reconciliation"), tx, audit.Event{Action: "team.mailbox.provision", Scope: id, Resource: "mailbox/" + box.ID, Meta: map[string]string{"owner_subject": member.Subject}}); e != nil {
			return e
		}
	}
	return nil
}

func ReconcilePersonalMailboxes(ctx context.Context) error {
	rows, err := q.New(db.From(ctx)).ReadyTeamWorkspaces(ctx)
	if err != nil {
		return err
	}
	for _, id := range rows {
		tx, err := db.From(ctx).Begin(ctx)
		if err != nil {
			return err
		}
		queries := q.New(tx)
		if _, err = queries.LockWorkspace(ctx, id); err == nil {
			err = EnsurePersonalMailboxes(ctx, tx, id)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
	}
	return nil
}
