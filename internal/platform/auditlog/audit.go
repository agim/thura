package auditlog

import (
	"context"
	"errors"
	"strconv"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

// Refused records only application-selected identifiers and the HTTP outcome.
// Request bodies, invitations, share tokens, email and error text never enter it.
// A denial in a workspace scope is recorded only for a member of that
// workspace: anyone signed in could otherwise fill another workspace's audit
// log without bound. The audit write never replaces the original refusal.
func Refused(ctx context.Context, action, scope, resource string, cause error) error {
	if cause == nil {
		return nil
	}
	outcome, status := audit.Failed, 500
	var httpError *router.HTTPError
	if errors.As(cause, &httpError) {
		status = httpError.Status
		if status < 500 {
			outcome = audit.Denied
		}
	}
	if scope != "" && uuid.Validate(scope) != nil {
		scope = ""
	}
	if len(resource) > 150 {
		resource = ""
	}
	if scope != "" && outcome == audit.Denied && !member(ctx, scope) {
		return cause
	}
	if err := audit.From(ctx).Record(audit.System(ctx, "anonymous-request"), audit.Event{
		Action: action, Scope: scope, Resource: resource, Outcome: outcome,
		Meta: map[string]string{"status": strconv.Itoa(status)},
	}); err != nil {
		lidza.Log(ctx).Warn("audit record failed", "action", action, "error", err)
	}
	return cause
}

func member(ctx context.Context, scope string) bool {
	user := auth.CurrentUser(ctx)
	if user == nil {
		return false
	}
	_, err := q.New(db.From(ctx)).WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: scope, Subject: user.ID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		lidza.Log(ctx).Warn("audit membership check failed", "error", err)
	}
	return err == nil
}
