package correspondence

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "thura/db/queries/gen"
	"thura/internal/platform/paging"
	"thura/internal/workspace"
	"thura/schema"
)

type correspondenceCursor struct {
	At     time.Time `json:"at"`
	ID     string    `json:"id"`
	Source string    `json:"source"`
}

func Timeline(ctx context.Context, w, id, scope, source string, page paging.Request, browse bool) (schema.CorrespondenceTimeline, error) {
	result := schema.CorrespondenceTimeline{Items: []schema.CorrespondenceEntry{}, Notices: []string{}}
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
	result.Contact = schema.Contact{ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name, Email: c.Email, Company: c.Company, Phone: c.Phone, Favorite: c.Favorite, CreatedAt: c.CreatedAt}
	if !correspondenceSource(source, !browse) || (browse && source == "") {
		return result, router.Errorf(422, "invalid correspondence source")
	}
	if browse && scope == "organization" {
		return result, router.Errorf(422, "choose a person to link items")
	}
	if scope != "" && scope != "person" && scope != "organization" {
		return result, router.Errorf(422, "invalid correspondence scope")
	}
	if scope == "organization" && strings.TrimSpace(c.Company) == "" {
		return result, router.Errorf(422, "this person has no organization")
	}
	var before *time.Time
	beforeSource := ""
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
		beforeSource = cursor.Source
		if beforeSource == "" {
			beforeSource = "mail"
		}
		if !correspondenceSource(beforeSource, true) {
			return result, router.Errorf(422, "invalid cursor")
		}
	}
	people, err := q.CorrespondencePeople(ctx, queries.CorrespondencePeopleParams{WorkspaceID: w, ContactID: id, Organization: scope == "organization", Company: c.Company})
	if err != nil {
		return result, err
	}
	identities := map[string]bool{}
	for _, p := range people {
		email := strings.ToLower(strings.TrimSpace(p.Email))
		if email != "" && !identities[email] {
			identities[email] = true
		}
	}
	// Bound each scan. A continuation can follow an empty page of false candidates.
	rows, err := q.CorrespondenceCandidates(ctx, queries.CorrespondenceCandidatesParams{WorkspaceID: w, ContactID: id, Organization: scope == "organization", Company: c.Company, Browse: browse, Viewer: auth.CurrentUser(ctx).ID, SourceFilter: source, BeforeSource: beforeSource, Search: page.Search, BeforeTime: before, BeforeID: beforeID, PageLimit: 201})
	if err != nil {
		return result, err
	}
	scanLimit := 200
	if page.Search != "" && !browse {
		scanLimit = 10
	}
	notices := map[string]bool{}
	started := time.Now()
	for i, m := range rows {
		if i > 0 && page.Search != "" && !browse && time.Since(started) > 6*time.Second {
			previous := rows[i-1]
			result.NextCursor = paging.Encode(correspondenceCursor{At: previous.At, ID: previous.ID, Source: previous.Source})
			break
		}
		if i == scanLimit {
			break
		}
		include := m.Source != "mail" || Matches(identities, m.FromAddress, m.ToAddress, m.Cc, m.Bcc)
		if page.Search != "" && !browse && (m.Source == "chat" || m.Source == "document") {
			metadata := strings.Contains(strings.ToLower(m.Subject+" "+m.Preview), strings.ToLower(page.Search))
			excerpt, matched, notice := contentMatch(ctx, w, m.ID, m.Source, page.Search)
			if notice != "" && !notices[notice] {
				result.Notices = append(result.Notices, notice)
				notices[notice] = true
			}
			include = metadata || matched
			if matched {
				m.Preview = excerpt
			}
			// Recheck both the person's association and source access after external I/O.
			visible, err := q.CorrespondenceCandidates(ctx, queries.CorrespondenceCandidatesParams{WorkspaceID: w, ContactID: id, Organization: scope == "organization", Company: c.Company, Viewer: auth.CurrentUser(ctx).ID, SourceFilter: m.Source, TargetID: m.ID, BeforeID: "00000000-0000-0000-0000-000000000000", PageLimit: 1})
			if err != nil {
				return result, err
			}
			if len(visible) == 0 {
				include = false
			}
		}
		if include {
			preview := []rune(strings.Join(strings.Fields(m.Preview), " "))
			if len(preview) > 220 {
				preview = append(preview[:220], '…')
			}
			mailboxID := ""
			if m.Source == "mail" {
				mailboxID = m.ContainerID
			}
			result.Items = append(result.Items, schema.CorrespondenceEntry{ID: m.ID, Source: m.Source, ContainerID: m.ContainerID, Reason: m.Reason, Linked: m.Linked, MailboxID: mailboxID, Subject: m.Subject, Preview: string(preview), FromAddress: m.FromAddress, ToAddress: m.ToAddress, Status: m.Status, At: m.At})
		}
		if len(result.Items) == int(page.Limit) || i == scanLimit-1 {
			if i+1 < len(rows) {
				result.NextCursor = paging.Encode(correspondenceCursor{At: m.At, ID: m.ID, Source: m.Source})
			}
			break
		}
	}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.CorrespondenceTimeline{}, err
	}
	return result, nil
}

func correspondenceSource(source string, mail bool) bool {
	switch source {
	case "", "mail":
		return mail
	case "chat", "document", "meeting", "calendar":
		return true
	}
	return false
}
