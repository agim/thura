package mailaccess

import (
	"context"
	"errors"
	stdmail "net/mail"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

// MaxSenderName bounds the display name outgoing mail carries.
const MaxSenderName = 120

// CleanSenderName trims a display name and reports whether it may be used:
// no control characters (a CR or LF would split the From header) and at most
// MaxSenderName characters.
func CleanSenderName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) > MaxSenderName {
		return name, false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return name, false
		}
	}
	return name, true
}

// SenderDefault is the name a mailbox sends under when it sets none: its
// owner's profile name when private, the workspace name when shared. A name
// that would not be a safe display name is dropped, never repaired.
func SenderDefault(ctx context.Context, queries *q.Queries, box q.Mailbox) (string, error) {
	name, err := queries.MailboxSenderDefault(ctx, box.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if clean, ok := CleanSenderName(name); ok {
		return clean, nil
	}
	return "", nil
}

// SenderName resolves the display name of the mailbox's outgoing mail: its
// own sender name, else SenderDefault. The mailbox name ("Team mail") is a
// label for readers of the app, never a sender name.
func SenderName(ctx context.Context, queries *q.Queries, box q.Mailbox) (string, error) {
	if clean, ok := CleanSenderName(box.SenderName); ok && clean != "" {
		return clean, nil
	}
	return SenderDefault(ctx, queries, box)
}

// From is the RFC 5322 From value of the mailbox: "Name <address>", quoted
// or encoded by net/mail, or the bare address when no name resolves.
func From(ctx context.Context, queries *q.Queries, box q.Mailbox) (string, error) {
	name, err := SenderName(ctx, queries, box)
	if err != nil {
		return "", err
	}
	return FormatFrom(name, box.Address), nil
}

// FormatFrom formats one address with an optional display name.
func FormatFrom(name, address string) string {
	if name == "" {
		return address
	}
	return (&stdmail.Address{Name: name, Address: address}).String()
}
