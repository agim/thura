package meet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/livekit"
	"thura/schema"
)

func pinHash(pin string) string { h := sha256.Sum256([]byte(pin)); return hex.EncodeToString(h[:]) }
func digits(n int) (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", n, v), nil
}

// Invitation secrets are never exposed by the meeting list or guest endpoint.
func NewInvite(ctx context.Context, queries *q.Queries, meetingID, eventID string, expires time.Time) (q.MeetingInvite, error) {
	pin, err := digits(6)
	if err != nil {
		return q.MeetingInvite{}, err
	}
	key, err := credentials.Key(".")
	if err != nil {
		return q.MeetingInvite{}, err
	}
	sealed, err := credentials.Encrypt(key, []byte(pin))
	if err != nil {
		return q.MeetingInvite{}, err
	}
	for n := 0; n < 8; n++ {
		code, xerr := digits(9)
		if xerr != nil {
			return q.MeetingInvite{}, xerr
		}
		invite, xerr := queries.CreateMeetingInvite(ctx, q.CreateMeetingInviteParams{MeetingID: meetingID, EventID: eventID, Code: code, PinHash: pinHash(pin), PinSealed: sealed, ExpiresAt: expires})
		if !errors.Is(xerr, pgx.ErrNoRows) {
			return invite, xerr
		}
	}
	return q.MeetingInvite{}, router.Errorf(503, "could not allocate a meeting ID; try again")
}
func InviteLink(ctx context.Context, invite q.MeetingInvite) (string, string, error) {
	key, err := credentials.Key(".")
	if err != nil {
		return "", "", err
	}
	pin, err := credentials.Decrypt(key, invite.PinSealed)
	if err != nil {
		return "", "", err
	}
	// Fragments keep the PIN out of HTTP request URLs and Referer headers.
	return mail.From(ctx).Link("/meet?room=" + invite.Code + "#pin=" + string(pin)), string(pin), nil
}
func InviteInstructions(ctx context.Context, invite q.MeetingInvite) (string, error) {
	link, pin, err := InviteLink(ctx, invite)
	if err != nil {
		return "", err
	}
	return "[Thura Meet]\nJoin meeting: " + link + "\n\nIf the link does not work, open " + mail.From(ctx).Link("/meet") + " and enter:\nMeeting ID: " + invite.Code + "\nPIN: " + pin + "\nEnter your name, then choose Join meeting. Your microphone and camera start off.\nAnyone with this invitation can join this meeting only. Keep the link and PIN private.\n[/Thura Meet]", nil
}
func JoinGuest(ctx context.Context, in schema.GuestMeetingInput) (schema.GuestMeetingAccess, error) {
	out := schema.GuestMeetingAccess{}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return out, err
	}
	for _, v := range []string{in.Code, in.Pin} {
		for _, c := range v {
			if c < '0' || c > '9' {
				return out, router.Errorf(404, "meeting ID or PIN is incorrect")
			}
		}
	}
	queries := q.New(db.From(ctx))
	invite, err := queries.MeetingInviteByCode(ctx, in.Code)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, router.Errorf(404, "meeting ID or PIN is incorrect")
	}
	if err != nil {
		return out, err
	}
	if subtle.ConstantTimeCompare([]byte(invite.PinHash), []byte(pinHash(in.Pin))) != 1 {
		return out, router.Errorf(404, "meeting ID or PIN is incorrect")
	}
	cfg, err := provider.Load()
	if err != nil {
		return out, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	m, err := queries.FindMeeting(ctx, invite.MeetingID)
	if err != nil {
		return out, err
	}
	m, err = queries.LockMeeting(ctx, q.LockMeetingParams{WorkspaceID: m.WorkspaceID, ID: m.ID})
	if err != nil {
		return out, err
	}
	invite, err = queries.MeetingInviteByCode(ctx, in.Code)
	if err != nil {
		return out, err
	}
	e, err := queries.FindGuestCalendarEvent(ctx, invite.EventID)
	if err != nil {
		return out, err
	}
	if m.EndedAt != nil || e.Cancelled || e.MeetingID == nil || *e.MeetingID != m.ID || !time.Now().Before(invite.ExpiresAt) {
		return out, router.Errorf(410, "this meeting invitation is no longer available")
	}
	if err = queries.ExpireMeetingGuestTokens(ctx); err != nil {
		return out, err
	}
	count, err := queries.CountMeetingGuestTokens(ctx, m.ID)
	if err != nil {
		return out, err
	}
	if count >= 100 {
		return out, router.Errorf(409, "meeting is full; try again later")
	}
	if err = cfg.Call(ctx, "CreateRoom", Room(m.ID), map[string]any{"name": Room(m.ID), "empty_timeout": 300, "max_participants": 100}, nil); err != nil {
		return out, err
	}
	identity := "guest:" + uuid.NewString()
	token, err := cfg.Token(identity, in.Name+" (guest)", Room(m.ID))
	if err != nil {
		return out, err
	}
	expires := time.Now().Add(2 * time.Minute)
	err = queries.SaveGuestMeetingToken(ctx, q.SaveGuestMeetingTokenParams{MeetingID: m.ID, Subject: identity, GuestInviteID: &invite.ID, TokenExpiresAt: expires})
	if err != nil {
		return out, err
	}
	return schema.GuestMeetingAccess{Token: token, ServerURL: cfg.PublicURL, ExpiresAt: expires, Name: m.Name}, tx.Commit(ctx)
}
