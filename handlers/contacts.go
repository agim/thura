package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

func ListWorkspaces(ctx context.Context, req *router.Request[router.None]) (schema.WorkspaceList, error) {
	rows, err := queries.New(db.From(ctx)).ListWorkspaces(ctx, auth.CurrentUser(ctx).ID)
	result := schema.WorkspaceList{Items: []schema.Workspace{}}
	for _, w := range rows {
		result.Items = append(result.Items, schema.Workspace{ID: w.ID, Name: w.Name, CreatedAt: w.CreatedAt})
	}
	return result, err
}

func ListContacts(ctx context.Context, req *router.Request[router.None]) (schema.ContactList, error) {
	id := req.Param("workspaceId")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return schema.ContactList{}, err
	}
	rows, err := queries.New(db.From(ctx)).ListContacts(ctx, id)
	result := schema.ContactList{Items: []schema.Contact{}}
	for _, c := range rows {
		result.Items = append(result.Items, contact(c))
	}
	return result, err
}

func clean(in schema.ContactInput) (schema.ContactInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Company != nil {
		s := strings.TrimSpace(*in.Company)
		in.Company = &s
	}
	if in.Phone != nil {
		s := strings.TrimSpace(*in.Phone)
		in.Phone = &s
	}
	return in, in.Validate()
}

func CreateContact(ctx context.Context, req *router.Request[schema.ContactInput]) (schema.Contact, error) {
	id := req.Param("workspaceId")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return schema.Contact{}, err
	}
	in, err := clean(req.Body)
	if err != nil {
		return schema.Contact{}, err
	}
	c, err := queries.New(db.From(ctx)).CreateContact(ctx, queries.CreateContactParams{WorkspaceID: id, Name: in.Name, Email: in.Email, Company: optionalText(in.Company), Phone: optionalText(in.Phone), Favorite: in.Favorite})
	if err == nil {
		req.Status(http.StatusCreated)
	}
	return contact(c), err
}

func UpdateContact(ctx context.Context, req *router.Request[schema.ContactInput]) (schema.Contact, error) {
	id, cid := req.Param("workspaceId"), req.Param("id")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return schema.Contact{}, err
	}
	if !workspace.ValidID(cid) {
		return schema.Contact{}, router.Errorf(http.StatusNotFound, "contact not found")
	}
	in, err := clean(req.Body)
	if err != nil {
		return schema.Contact{}, err
	}
	c, err := queries.New(db.From(ctx)).UpdateContact(ctx, queries.UpdateContactParams{WorkspaceID: id, ID: cid, Name: in.Name, Email: in.Email, Company: optionalText(in.Company), Phone: optionalText(in.Phone), Favorite: in.Favorite})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.Contact{}, router.Errorf(http.StatusNotFound, "contact not found")
	}
	return contact(c), err
}

func DeleteContact(ctx context.Context, req *router.Request[router.None]) (router.None, error) {
	id, cid := req.Param("workspaceId"), req.Param("id")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return router.None{}, err
	}
	if !workspace.ValidID(cid) {
		return router.None{}, router.Errorf(http.StatusNotFound, "contact not found")
	}
	count, err := queries.New(db.From(ctx)).DeleteContact(ctx, queries.DeleteContactParams{WorkspaceID: id, ID: cid})
	if err == nil && count == 0 {
		err = router.Errorf(http.StatusNotFound, "contact not found")
	}
	return router.None{}, err
}

func contact(c queries.Contact) schema.Contact {
	return schema.Contact{ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name, Email: c.Email, Company: c.Company, Phone: c.Phone, Favorite: c.Favorite, CreatedAt: c.CreatedAt}
}

func optionalText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
