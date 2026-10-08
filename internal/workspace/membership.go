package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	queries "thura/db/queries/gen"
	"thura/schema"
)

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func role(ctx context.Context, q *queries.Queries, id, subject string) (string, error) {
	r, err := q.WorkspaceRole(ctx, queries.WorkspaceRoleParams{Scope: id, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", router.Errorf(http.StatusForbidden, "workspace permission required")
	}
	return r, err
}

func management(ctx context.Context, id string) (pgx.Tx, *queries.Queries, string, error) {
	if err := RequireMember(ctx, id); err != nil {
		return nil, nil, "", err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	q := queries.New(tx)
	if _, err = q.LockWorkspace(ctx, id); err != nil {
		err = errors.Join(err, tx.Rollback(ctx))
		return nil, nil, "", err
	}
	r, err := role(ctx, q, id, auth.CurrentUser(ctx).ID)
	if err == nil && r != "owner" && r != "admin" {
		err = router.Errorf(http.StatusForbidden, "workspace administrator required")
	}
	if err != nil {
		err = errors.Join(err, tx.Rollback(ctx))
		return nil, nil, "", err
	}
	return tx, q, r, nil
}

func Invite(ctx context.Context, id string, in schema.InviteInput) (schema.InviteView, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if err := in.Validate(); err != nil {
		return schema.InviteView{}, err
	}
	tx, q, actorRole, err := management(ctx, id)
	if err != nil {
		return schema.InviteView{}, err
	}
	defer tx.Rollback(ctx)
	if actorRole != "owner" && in.Role != schema.WorkspaceRoleMember {
		return schema.InviteView{}, router.Errorf(http.StatusForbidden, "only owners can invite administrators or owners")
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return schema.InviteView{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	inv, err := q.CreateInvitation(ctx, queries.CreateInvitationParams{WorkspaceID: id, Email: in.Email, Role: queries.WorkspaceRole(in.Role), TokenHash: hashToken(token), InvitedBy: auth.CurrentUser(ctx).ID, ExpiresAt: lidza.Now(ctx).Add(72 * time.Hour)})
	if err != nil {
		return schema.InviteView{}, err
	}
	w, err := q.LockWorkspace(ctx, id)
	if err != nil {
		return schema.InviteView{}, err
	}
	_, err = mail.From(ctx).SendTx(ctx, tx, mail.Message{To: in.Email, Subject: "Invitation to " + w.Name, Text: "You have been invited to " + w.Name + " on Thura.\n\n" + mail.From(ctx).Link("/invite?token="+token) + "\n\nThis invitation expires in 72 hours. If you did not expect it, ignore this email."})
	if err != nil {
		return schema.InviteView{}, err
	}
	return InviteView(inv), tx.Commit(ctx)
}

func InviteView(i queries.Invitation) schema.InviteView {
	return schema.InviteView{ID: i.ID, Email: i.Email, Role: schema.WorkspaceRole(i.Role), ExpiresAt: i.ExpiresAt, AcceptedAt: i.AcceptedAt, RevokedAt: i.RevokedAt}
}

// Accept locks workspace then invitation, the same order as revocation, so
// concurrent acceptance cannot reuse a link or race role administration.
func Accept(ctx context.Context, in schema.AcceptInviteInput) (schema.Workspace, error) {
	pool := db.From(ctx)
	snapshot, err := queries.New(pool).FindInvitation(ctx, hashToken(in.Token))
	gone := router.Errorf(http.StatusGone, "invitation is invalid, used, revoked, or expired")
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.Workspace{}, gone
	}
	if err != nil {
		return schema.Workspace{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return schema.Workspace{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	w, err := q.LockWorkspace(ctx, snapshot.WorkspaceID)
	if err != nil {
		return schema.Workspace{}, err
	}
	inv, err := q.LockInvitation(ctx, hashToken(in.Token))
	if err != nil {
		return schema.Workspace{}, err
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil || !inv.ExpiresAt.After(lidza.Now(ctx)) {
		return schema.Workspace{}, gone
	}
	inviterRole, err := role(ctx, q, inv.WorkspaceID, inv.InvitedBy)
	if err != nil || (inviterRole != "owner" && (inviterRole != "admin" || inv.Role != queries.WorkspaceRoleMember)) {
		return schema.Workspace{}, gone
	}
	subject, err := q.FindInvitedAccount(ctx, &inv.Email)
	if err == nil {
		u := auth.CurrentUser(ctx)
		if u == nil || u.ID != subject {
			return schema.Workspace{}, router.Errorf(http.StatusConflict, "sign in with the invited email before accepting")
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		if auth.CurrentUser(ctx) != nil {
			return schema.Workspace{}, router.Errorf(http.StatusConflict, "sign out before creating the invited account")
		}
		if in.Password == nil || in.Name == nil || strings.TrimSpace(*in.Name) == "" {
			return schema.Workspace{}, router.Errorf(http.StatusUnprocessableEntity, "name and password are required for a new account")
		}
		if err = auth.From(ctx).ValidatePassword(*in.Password, inv.Email); err != nil {
			return schema.Workspace{}, err
		}
		hash, err := auth.HashPassword(*in.Password)
		if err != nil {
			return schema.Workspace{}, err
		}
		name := strings.TrimSpace(*in.Name)
		subject, err = q.CreateInvitedAccount(ctx, queries.CreateInvitedAccountParams{Subject: uuid.NewString(), Email: &inv.Email, Name: &name, PasswordHash: &hash})
		if err != nil {
			var p *pgconn.PgError
			if errors.As(err, &p) && p.Code == "23505" {
				return schema.Workspace{}, router.Errorf(http.StatusConflict, "account was created; sign in before accepting")
			}
			return schema.Workspace{}, err
		}
	} else {
		return schema.Workspace{}, err
	}
	// Joining never changes an existing member's role through an old invite.
	if _, err := q.WorkspaceRole(ctx, queries.WorkspaceRoleParams{Scope: inv.WorkspaceID, Subject: subject}); errors.Is(err, pgx.ErrNoRows) {
		if err = q.GrantWorkspaceMember(ctx, queries.GrantWorkspaceMemberParams{Scope: inv.WorkspaceID, Subject: subject, Role: string(inv.Role), GrantedBy: &inv.InvitedBy}); err != nil {
			return schema.Workspace{}, err
		}
	} else if err != nil {
		return schema.Workspace{}, err
	}
	if err = q.AcceptInvitation(ctx, inv.ID); err != nil {
		return schema.Workspace{}, err
	}
	return schema.Workspace{ID: w.ID, Name: w.Name, CreatedAt: w.CreatedAt}, tx.Commit(ctx)
}

func ChangeMember(ctx context.Context, id, subject, newRole string) error {
	tx, q, actorRole, err := management(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	targetRole, err := role(ctx, q, id, subject)
	if err != nil {
		return err
	}
	if actorRole != "owner" && (targetRole != "member" || (newRole != "" && newRole != "member")) {
		return router.Errorf(http.StatusForbidden, "only owners can change administrator or owner roles")
	}
	if targetRole == "owner" && newRole != "owner" {
		n, err := q.CountWorkspaceOwners(ctx, id)
		if err != nil {
			return err
		}
		if n <= 1 {
			return router.Errorf(http.StatusConflict, "workspace must keep at least one owner")
		}
	}
	if err = q.RemoveWorkspaceMember(ctx, queries.RemoveWorkspaceMemberParams{Scope: id, Subject: subject}); err != nil {
		return err
	}
	if newRole != "" {
		actor := auth.CurrentUser(ctx).ID
		if err = q.GrantWorkspaceMember(ctx, queries.GrantWorkspaceMemberParams{Scope: id, Subject: subject, Role: newRole, GrantedBy: &actor}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func Revoke(ctx context.Context, id, invitationID string) error {
	tx, q, _, err := management(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if !ValidID(invitationID) {
		return router.Errorf(http.StatusNotFound, "invitation not found")
	}
	n, err := q.RevokeInvitation(ctx, queries.RevokeInvitationParams{WorkspaceID: id, ID: invitationID})
	if err != nil {
		return err
	}
	if n == 0 {
		return router.Errorf(http.StatusNotFound, "pending invitation not found")
	}
	return tx.Commit(ctx)
}

func RequireManager(ctx context.Context, id string) error {
	if err := RequireMember(ctx, id); err != nil {
		return err
	}
	r, err := role(ctx, queries.New(db.From(ctx)), id, auth.CurrentUser(ctx).ID)
	if err != nil {
		return err
	}
	if r != "owner" && r != "admin" {
		return router.Errorf(http.StatusForbidden, "workspace administrator required")
	}
	return nil
}
