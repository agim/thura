package workspace

import (
	"context"
	"net/http"
	"regexp"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	queries "thura/db/queries/gen"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

// RequireMember reads current membership on every request, rather than trusting
// role claims in a session. Unknown roles and app-wide scopes grant no access.
func RequireMember(ctx context.Context, id string) error {
	u := auth.CurrentUser(ctx)
	if u == nil {
		return router.Errorf(http.StatusUnauthorized, "sign in required")
	}
	if !ValidID(id) {
		return router.Errorf(http.StatusNotFound, "workspace not found")
	}
	ok, err := queries.New(db.From(ctx)).IsWorkspaceMember(ctx, queries.IsWorkspaceMemberParams{Scope: id, Subject: u.ID})
	if err != nil {
		return err
	}
	if !ok {
		return router.Errorf(http.StatusNotFound, "workspace not found")
	}
	return nil
}
