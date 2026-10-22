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
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/livekit"
	"thura/internal/tenancy"
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
func InviteLink(ctx context.Context, invite q.MeetingInvite, transactionQueries ...*q.Queries) (string, string, error) {
	key, err := credentials.Key(".")
	if err != nil {
		return "", "", err
	}
	pin, err := credentials.Decrypt(key, invite.PinSealed)
	if err != nil {
		return "", "", err
	}
	queries := q.New(db.From(ctx))
	if len(transactionQueries) > 0 {
		queries = transactionQueries[0]
	}
	meeting, err := queries.FindMeeting(ctx, invite.MeetingID)
	if err != nil {
		return "", "", err
	}
	// Fragments keep the PIN out of HTTP request URLs and Referer headers.
	return tenancy.Link(ctx, meeting.WorkspaceID, "/meet?room="+invite.Code+"#pin="+string(pin)), string(pin), nil
}
func InviteInstructions(ctx context.Context, invite q.MeetingInvite, transactionQueries ...*q.Queries) (string, error) {
	link, pin, err := InviteLink(ctx, invite, transactionQueries...)
	if err != nil {
		return "", err
	}
	return "[Thura Meet]\nJoin meeting: " + link + "\n\nIf the link does not work, open " + strings.SplitN(link, "/meet?", 2)[0] + "/meet" + " and enter:\nMeeting ID: " + invite.Code + "\nPIN: " + pin + "\nEnter your name, then choose Join meeting. Your microphone and camera start off.\nAnyone with this invitation can join this meeting only. Keep the link and PIN private.\n[/Thura Meet]", nil
}
// pinFailureLimit failed PINs lock the invite for pinLockout, doubling with
// each further failure up to pinLockoutMax; a correct PIN clears the count.
const pinFailureLimit = 10
const pinLockout = 15 * time.Minute
const pinLockoutMax = 24 * time.Hour

func recordPinFailure(ctx context.Context, queries *q.Queries, inviteID string, now time.Time) error {
	failures, err := queries.RecordMeetingPinFailure(ctx, inviteID)
	if err != nil || failures < pinFailureLimit {
		return err
	}
	lockout := pinLockout
	for n := failures - pinFailureLimit; n > 0 && lockout < pinLockoutMax; n-- {
		lockout *= 2
	}
	if lockout > pinLockoutMax {
		lockout = pinLockoutMax
	}
	until := now.Add(lockout)
	return queries.LockMeetingInvite(ctx, q.LockMeetingInviteParams{ID: inviteID, LockedUntil: &until})
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
	// A 6-digit PIN needs a durable per-invite counter: the per-IP limiter
	// alone is sidestepped from a large address block. The lock is checked
	// before the compare so a locked invite leaks nothing about the PIN.
	now := time.Now()
	if invite.LockedUntil != nil && now.Before(*invite.LockedUntil) {
		return out, router.Errorf(429, "too many incorrect PINs; try again later or ask the host for a new invitation")
	}
	if subtle.ConstantTimeCompare([]byte(invite.PinHash), []byte(pinHash(in.Pin))) != 1 {
		if err = recordPinFailure(ctx, queries, invite.ID, now); err != nil {
			return out, err
		}
		return out, router.Errorf(404, "meeting ID or PIN is incorrect")
	}
	if err = queries.ResetMeetingPinFailures(ctx, invite.ID); err != nil {
		return out, err
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
	if !tenancy.Allows(ctx, m.WorkspaceID) {
		return out, router.Errorf(404, "meeting ID or PIN is incorrect")
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
