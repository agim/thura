// Package mailaccess owns the personal/shared mailbox boundary for readers and jobs.
package mailaccess

import (
	"context"
	"github.com/agim/lidza/pkg/router"
	q "thura/db/queries/gen"
)

// Owns never grants administrators implicit access to a personal inbox. A
// reserved inbox with no owner is inaccessible until invitation acceptance.
func Owns(box q.Mailbox, subject string) bool {
	return !box.Private || (subject != "" && box.OwnerSubject != nil && *box.OwnerSubject == subject)
}

func Check(ctx context.Context, queries *q.Queries, box q.Mailbox, subject string) error {
	if !Owns(box, subject) {
		return router.Errorf(404, "mailbox not found")
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: box.WorkspaceID, Subject: subject})
	if err != nil {
		return err
	}
	if !member {
		return router.Errorf(404, "mailbox not found")
	}
	return nil
}
