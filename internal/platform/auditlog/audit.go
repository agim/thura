package auditlog

import (
	"context"
	"errors"
	"strconv"

	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
)

// Refused records only application-selected identifiers and the HTTP outcome.
// Request bodies, invitations, share tokens, email and error text never enter it.
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
	if err := audit.From(ctx).Record(audit.System(ctx, "anonymous-request"), audit.Event{
		Action: action, Scope: scope, Resource: resource, Outcome: outcome,
		Meta: map[string]string{"status": strconv.Itoa(status)},
	}); err != nil {
		return err
	}
	return cause
}
