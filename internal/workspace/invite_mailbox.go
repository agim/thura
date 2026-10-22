package workspace

import (
	"context"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/tenancy"
	"thura/schema"
)

// Reserve the address before sending activation. NULL owner plus private=true
// deliberately keeps mail received before acceptance unreadable by the team.
func invitationMailbox(ctx context.Context, tx pgx.Tx, queries *q.Queries, workspaceID string, in schema.InviteInput) (*string, string, error) {
	delivery := in.Email
	if in.DeliveryEmail != nil && strings.TrimSpace(*in.DeliveryEmail) != "" {
		delivery = strings.ToLower(strings.TrimSpace(*in.DeliveryEmail))
	}
	address := ""
	if in.MailboxAddress != nil {
		address = strings.ToLower(strings.TrimSpace(*in.MailboxAddress))
	}
	_, managedErr := queries.GetTeamSetup(ctx, workspaceID)
	if address == "" {
		_, domain, _ := strings.Cut(in.Email, "@")
		if d, e := queries.BoundTeamDomain(ctx, domain); e == nil && d.WorkspaceID == workspaceID && d.Mail {
			address = in.Email
		}
	}
	// A separate delivery address only carries activation for a new team inbox
	// that is not active yet. Every other invitation goes to the invited address
	// itself: acceptance creates an account for that email, so the token must
	// prove the recipient reads it, not an inviter-chosen inbox.
	if delivery != in.Email && in.Email != address {
		return nil, "", router.Errorf(422, "The invitation is delivered to the invited email address; a separate delivery address is only for a new team inbox")
	}
	if address == "" {
		if managedErr == nil {
			return nil, "", router.Errorf(422, "Choose a team email address for the new member")
		}
		if !errors.Is(managedErr, pgx.ErrNoRows) {
			return nil, "", managedErr
		}
		return nil, delivery, nil // Legacy workspaces without assigned mail domains.
	}
	_, domain, _ := strings.Cut(address, "@")
	if err := tenancy.LockDomain(ctx, tx, domain); err != nil {
		return nil, "", err
	}
	d, err := queries.BoundTeamDomain(ctx, domain)
	if err != nil || d.WorkspaceID != workspaceID || !d.Mail || !tenancy.Active(d, lidza.Now(ctx)) {
		return nil, "", router.Errorf(422, "Choose a verified email domain assigned to this team")
	}
	_, deliveryDomain, _ := strings.Cut(delivery, "@")
	if local, e := queries.BoundTeamDomain(ctx, deliveryDomain); e == nil && local.Mail {
		// An existing account can receive activation at an already-owned mailbox;
		// never bootstrap a new account by sending only to its reserved address.
		if _, e := queries.FindInvitedAccount(ctx, &in.Email); e != nil || delivery == address {
			return nil, "", router.Errorf(422, "Use an existing external email address to deliver the invitation; the new team inbox is not active yet")
		}
	} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, "", e
	}
	exists, err := queries.TeamMailboxAddressExists(ctx, address)
	if err != nil {
		return nil, "", err
	}
	if exists {
		box, err := queries.ReusableInvitationMailbox(ctx, q.ReusableInvitationMailboxParams{WorkspaceID: workspaceID, Address: address, Email: in.Email, Now: lidza.Now(ctx)})
		if err == nil {
			if err = queries.RevokeReservedMailboxInvitations(ctx, q.RevokeReservedMailboxInvitationsParams{WorkspaceID: workspaceID, MailboxID: &box.ID}); err != nil {
				return nil, "", err
			}
			return &box.ID, delivery, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, "", err
		}
		return nil, "", router.Errorf(409, "This email address is already assigned to a mailbox, alias or active invitation")
	}
	box, err := queries.ReservePersonalMailbox(ctx, q.ReservePersonalMailboxParams{WorkspaceID: workspaceID, Name: address, Address: address})
	if err != nil {
		return nil, "", err
	}
	return &box.ID, delivery, nil
}

func legacyInvitationMailbox(ctx context.Context, tx pgx.Tx, queries *q.Queries, id, email, subject string) error {
	// Older unconfigured workspaces have no assigned mail domain. Their account
	// invitations remain valid; personal inbox provisioning starts when the
	// administrator finishes that team's mail setup.
	if _, err := queries.GetTeamSetup(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	_, domain, _ := strings.Cut(email, "@")
	d, err := queries.BoundTeamDomain(ctx, domain)
	if errors.Is(err, pgx.ErrNoRows) {
		return tenancy.EnsurePersonalMailboxes(ctx, tx, id)
	}
	if err != nil {
		return err
	}
	if d.WorkspaceID != id || !d.Mail || !tenancy.Active(d, lidza.Now(ctx)) {
		return router.Errorf(409, "The invited email domain is not assigned to this team")
	}
	if err = tenancy.LockDomain(ctx, tx, domain); err != nil {
		return err
	}
	boxes, err := queries.FindSmipReceivingMailbox(ctx, email)
	if err != nil {
		return err
	}
	if len(boxes) > 0 {
		if len(boxes) != 1 || boxes[0].WorkspaceID != id || !boxes[0].Private || boxes[0].OwnerSubject == nil || *boxes[0].OwnerSubject != subject {
			return router.Errorf(409, "This older invitation conflicts with an existing mailbox or alias. Ask the administrator to issue an invitation for an unused personal address")
		}
	} else {
		box, err := queries.ReservePersonalMailbox(ctx, q.ReservePersonalMailboxParams{WorkspaceID: id, Name: email, Address: email})
		if err != nil {
			return err
		}
		n, err := queries.AssignPersonalMailbox(ctx, q.AssignPersonalMailboxParams{ID: box.ID, WorkspaceID: id, Subject: &subject})
		if err != nil {
			return err
		}
		if n != 1 {
			return router.Errorf(409, "Personal mailbox could not be assigned")
		}
	}
	return tenancy.EnsurePersonalMailboxes(ctx, tx, id)
}
