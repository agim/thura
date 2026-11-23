// Package mailfilter files new mail by a mailbox's filters, as Gmail does:
// each filter is a set of "contains" criteria and what to do with a match
// (apply a label, skip the Inbox, mark as read, star, move to Trash).
// Filters run when mail reaches the Inbox, never on mail held as Spam, and
// only ever move mail out of the Inbox. internal/mailbox owns the settings.
package mailfilter

import (
	"context"
	"strings"

	q "thura/db/queries/gen"
)

// terms are the alternatives of one criterion: a comma or the word OR
// separates them ("billing@a.example, news@b.example"), any one may match.
func terms(criterion string) []string {
	out := []string{}
	for _, part := range strings.Split(strings.ReplaceAll(criterion, " OR ", ","), ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// contains reports whether a criterion is empty (it does not narrow) or one
// of its alternatives is in the text, whatever the case.
func contains(text, criterion string) bool {
	alternatives := terms(criterion)
	if len(alternatives) == 0 {
		return true
	}
	text = strings.ToLower(text)
	for _, term := range alternatives {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

// Empty reports whether a filter has no criterion: it would match all mail,
// so it is never saved and never matches.
func Empty(f q.MailFilter) bool {
	return len(terms(f.FromContains))+len(terms(f.ToContains))+len(terms(f.SubjectContains))+len(terms(f.HasWords)) == 0
}

// Match reports whether a message meets every filled criterion of a filter:
// From (name and address), To (To and Cc), Subject, and words anywhere in
// the subject or the body.
func Match(f q.MailFilter, item q.MailItem) bool {
	if Empty(f) {
		return false
	}
	body := item.TextBody
	if strings.TrimSpace(body) == "" {
		body = item.HTMLBody
	}
	return contains(item.FromAddress, f.FromContains) &&
		contains(item.ToAddress+" "+item.Cc, f.ToContains) &&
		contains(item.Subject, f.SubjectContains) &&
		contains(item.Subject+" "+body, f.HasWords)
}

// File applies the given filters to one Inbox message, together: every
// matching filter's label, and the strongest move (Trash over skipping the
// Inbox). It answers the message as stored afterwards and whether any
// filter matched. A message outside the Inbox is left alone.
func File(ctx context.Context, queries *q.Queries, filters []q.MailFilter, item q.MailItem) (q.MailItem, bool, error) {
	if item.Folder != q.MailFolderInbox {
		return item, false, nil
	}
	folder, read, star, matched := q.MailFolderInbox, false, false, false
	for _, f := range filters {
		if !f.Enabled || !Match(f, item) {
			continue
		}
		matched = true
		if f.LabelID != nil {
			if err := queries.AddMailTag(ctx, q.AddMailTagParams{ItemID: item.ID, LabelID: *f.LabelID}); err != nil {
				return item, matched, err
			}
		}
		if f.Trash {
			folder = q.MailFolderTrash
		} else if f.SkipInbox && folder == q.MailFolderInbox {
			folder = q.MailFolderArchive
		}
		read = read || f.MarkRead
		star = star || f.Star
	}
	if !matched || (folder == q.MailFolderInbox && !read && !star) {
		return item, matched, nil
	}
	filed, err := queries.FileFilteredMail(ctx, q.FileFilteredMailParams{ID: item.ID, Folder: folder, MarkRead: read, Star: star})
	if err != nil {
		return item, matched, err
	}
	return filed, matched, nil
}

// Arrive files a message that just reached the Inbox by its mailbox's
// filters, in the transaction that put it there.
func Arrive(ctx context.Context, queries *q.Queries, item q.MailItem) (q.MailItem, error) {
	filters, err := queries.ListActiveMailFilters(ctx, item.MailboxID)
	if err != nil || len(filters) == 0 {
		return item, err
	}
	filed, _, err := File(ctx, queries, filters, item)
	return filed, err
}
