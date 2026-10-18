package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "thura/db/queries/gen"
	"thura/internal/correspondence"
	"thura/internal/platform/paging"
	"thura/internal/workspace"
	"thura/schema"
)

type correspondenceCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func GetCorrespondence(ctx context.Context, req *router.Request[router.None]) (schema.CorrespondenceTimeline, error) {
	result := schema.CorrespondenceTimeline{Items: []schema.CorrespondenceEntry{}}
	w, id := req.Param("workspaceId"), req.Param("id")
	if err := workspace.RequireMember(ctx, w); err != nil {
		return result, err
	}
	if !workspace.ValidID(id) {
		return result, router.Errorf(404, "contact not found")
	}
	q := queries.New(db.From(ctx))
	c, err := q.GetCorrespondenceContact(ctx, queries.GetCorrespondenceContactParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return result, router.Errorf(404, "contact not found")
	}
	if err != nil {
		return result, err
	}
	result.Contact = contact(c)
	scope := req.Query("scope")
	if scope != "" && scope != "person" && scope != "organization" {
		return result, router.Errorf(422, "invalid correspondence scope")
	}
	if scope == "organization" && strings.TrimSpace(c.Company) == "" {
		return result, router.Errorf(422, "this person has no organization")
	}
	page, err := paging.Read(req.Raw, 50)
	if err != nil {
		return result, err
	}
	var before *time.Time
	beforeID := "00000000-0000-0000-0000-000000000000"
	if page.Cursor != "" {
		var cursor correspondenceCursor
		if err := paging.Decode(page.Cursor, &cursor); err != nil {
			return result, err
		}
		if cursor.At.IsZero() || !workspace.ValidID(cursor.ID) {
			return result, router.Errorf(422, "invalid cursor")
		}
		before = &cursor.At
		beforeID = cursor.ID
	}
	people, err := q.CorrespondencePeople(ctx, queries.CorrespondencePeopleParams{WorkspaceID: w, ContactID: id, Organization: scope == "organization", Company: c.Company})
	if err != nil {
		return result, err
	}
	emails := []string{}
	identities := map[string]bool{}
	for _, p := range people {
		email := strings.ToLower(strings.TrimSpace(p.Email))
		if email != "" && !identities[email] {
			emails = append(emails, email)
			identities[email] = true
		}
	}
	// Bound each scan. A continuation can follow an empty page of false candidates.
	rows, err := q.CorrespondenceCandidates(ctx, queries.CorrespondenceCandidatesParams{WorkspaceID: w, Emails: emails, Search: page.Search, BeforeTime: before, BeforeID: beforeID, PageLimit: 201})
	if err != nil {
		return result, err
	}
	for i, m := range rows {
		if i == 200 {
			break
		}
		if correspondence.Matches(identities, m.FromAddress, m.ToAddress, m.Cc, m.Bcc) {
			preview := []rune(strings.Join(strings.Fields(m.TextBody), " "))
			if len(preview) > 220 {
				preview = append(preview[:220], '…')
			}
			result.Items = append(result.Items, schema.CorrespondenceEntry{ID: m.ID, MailboxID: m.MailboxID, Subject: m.Subject, Preview: string(preview), FromAddress: m.FromAddress, ToAddress: m.ToAddress, Status: m.Status, At: m.CreatedAt})
		}
		if len(result.Items) == int(page.Limit) || i == 199 {
			if i+1 < len(rows) {
				result.NextCursor = paging.Encode(correspondenceCursor{At: m.CreatedAt, ID: m.ID})
			}
			break
		}
	}
	return result, nil
}
