package setup

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/tenancy"
)

// Exact aliases go to the initial administrator's workspace mailbox. Never add
// a catch-all, forward externally or claim another workspace's receiving address.
func createOperatorAliases(ctx context.Context, tx pgx.Tx, mailbox q.Mailbox) error {
	_, domain, ok := strings.Cut(strings.ToLower(mailbox.Address), "@")
	if !ok || domain == "" {
		return errors.New("administrator mailbox requires an email domain")
	}
	if err := tenancy.LockDomain(ctx, tx, domain); err != nil {
		return err
	}
	queries := q.New(tx)
	for _, local := range []string{"postmaster", "abuse", "hostmaster", "webmaster", "security", "noc", "admin", "administrator", "root", "mailer-daemon"} {
		address := local + "@" + domain
		existing, err := queries.FindSmipReceivingMailbox(ctx, address)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			if len(existing) == 1 && existing[0].ID == mailbox.ID {
				continue
			}
			return errors.New("a standard operator address is already assigned to another mailbox; resolve the conflict before publishing")
		}
		if _, err := queries.CreateMailboxAlias(ctx, q.CreateMailboxAliasParams{MailboxID: mailbox.ID, Address: address}); err != nil {
			return err
		}
	}
	return nil
}
