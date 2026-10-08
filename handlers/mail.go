package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	queries "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/internal/platform/paging"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func ListMailboxes(ctx context.Context, r *router.Request[router.None]) (schema.MailboxList, error) {
	id := r.Param("workspaceId")
	if err := workspace.RequireMember(ctx, id); err != nil {
		return schema.MailboxList{}, err
	}
	rows, err := queries.New(db.From(ctx)).ListMailboxes(ctx, id)
	out := schema.MailboxList{Items: []schema.Mailbox{}}
	for _, m := range rows {
		out.Items = append(out.Items, mailbox.ViewMailbox(m))
	}
	return out, err
}

func ReceiveMail(ctx context.Context, r *router.Request[router.File]) (schema.MailItem, error) {
	id := r.Param("mailboxId")
	if !workspace.ValidID(id) {
		return schema.MailItem{}, router.Errorf(404, "mailbox not found")
	}
	raw, err := io.ReadAll(r.Body.Body)
	if err != nil {
		return schema.MailItem{}, err
	}
	item, err := mailbox.Receive(ctx, id, r.Raw.Header.Get("X-Thura-Timestamp"), r.Raw.Header.Get("X-Thura-Delivery"), r.Raw.Header.Get("X-Thura-Signature"), raw)
	return mailbox.View(item), err
}
func ListMail(ctx context.Context, r *router.Request[router.None]) (schema.MailItemList, error) {
	if _, err := mailbox.Access(ctx, r.Param("workspaceId"), r.Param("mailboxId")); err != nil {
		return schema.MailItemList{}, err
	}
	folder := queries.MailFolder(r.Raw.URL.Query().Get("folder"))
	if folder == "" {
		folder = queries.MailFolderInbox
	}
	switch folder {
	case "inbox", "drafts", "sent", "archive", "trash", "spam":
	default:
		return schema.MailItemList{}, router.Errorf(422, "invalid folder")
	}
	page, err := paging.Read(r.Raw, 50)
	if err != nil {
		return schema.MailItemList{}, err
	}
	var before *time.Time
	beforeID := "00000000-0000-0000-0000-000000000000"
	if page.Cursor != "" {
		var cursor mailCursor
		if err := paging.Decode(page.Cursor, &cursor); err != nil {
			return schema.MailItemList{}, err
		}
		if !workspace.ValidID(cursor.ID) || cursor.Time.IsZero() {
			return schema.MailItemList{}, router.Errorf(422, "invalid cursor")
		}
		before, beforeID = &cursor.Time, cursor.ID
	}
	rows, err := queries.New(db.From(ctx)).ListMailItemsPage(ctx, queries.ListMailItemsPageParams{MailboxID: r.Param("mailboxId"), Folder: folder, Search: page.Search, BeforeTime: before, BeforeID: beforeID, PageLimit: page.Limit + 1})
	out := schema.MailItemList{Items: []schema.MailItem{}}
	if len(rows) > int(page.Limit) {
		last := rows[page.Limit-1]
		out.NextCursor = paging.Encode(mailCursor{ID: last.ID, Time: last.UpdatedAt})
		rows = rows[:page.Limit]
	}
	for _, i := range rows {
		out.Items = append(out.Items, mailbox.View(i))
	}
	return out, err
}

type mailCursor struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
}

func GetMail(ctx context.Context, r *router.Request[router.None]) (schema.MailDetail, error) {
	i, err := mailbox.ItemAccess(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"))
	if err != nil {
		return schema.MailDetail{}, err
	}
	rows, err := queries.New(db.From(ctx)).ListMailAttachments(ctx, i.ID)
	out := schema.MailDetail{Item: mailbox.View(i), Attachments: []schema.AttachmentView{}}
	for _, a := range rows {
		out.Attachments = append(out.Attachments, mailbox.Attachment(a))
	}
	return out, err
}
func CreateMailDraft(ctx context.Context, r *router.Request[schema.DraftInput]) (schema.MailItem, error) {
	m, err := mailbox.Access(ctx, r.Param("workspaceId"), r.Param("mailboxId"))
	if err != nil {
		return schema.MailItem{}, err
	}
	in := r.Body
	i, err := queries.New(db.From(ctx)).CreateDraft(ctx, queries.CreateDraftParams{MailboxID: m.ID, AuthorID: auth.CurrentUser(ctx).ID, FromAddress: m.Address, ToAddress: mailbox.Text(in.To), Cc: mailbox.Text(in.Cc), Bcc: mailbox.Text(in.Bcc), Subject: mailbox.Text(in.Subject), TextBody: mailbox.Text(in.Text), HTMLBody: mailbox.Text(in.HTML), ThreadID: mailbox.Text(in.ThreadID)})
	if err == nil {
		r.Status(http.StatusCreated)
	}
	return mailbox.View(i), err
}
func UpdateMailDraft(ctx context.Context, r *router.Request[schema.DraftInput]) (schema.MailItem, error) {
	i, err := mailbox.ItemAccess(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"))
	if err != nil {
		return schema.MailItem{}, err
	}
	in := r.Body
	if err = mailbox.CheckEditable(i); err != nil {
		return schema.MailItem{}, err
	}
	i, err = queries.New(db.From(ctx)).UpdateDraft(ctx, queries.UpdateDraftParams{ID: i.ID, ToAddress: mailbox.Text(in.To), Cc: mailbox.Text(in.Cc), Bcc: mailbox.Text(in.Bcc), Subject: mailbox.Text(in.Subject), TextBody: mailbox.Text(in.Text), HTMLBody: mailbox.Text(in.HTML), ThreadID: mailbox.Text(in.ThreadID)})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(409, "draft changed while saving")
	}
	return mailbox.View(i), err
}
func UpdateMailFlags(ctx context.Context, r *router.Request[schema.MailFlags]) (schema.MailItem, error) {
	tx, q, i, err := mailbox.Lock(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"))
	if err != nil {
		return schema.MailItem{}, err
	}
	defer tx.Rollback(ctx)
	var folder *queries.MailFolder
	if r.Body.Folder != nil {
		value := queries.MailFolder(*r.Body.Folder)
		folder = &value
		if i.Status == "queued" || i.Status == "failed" {
			return schema.MailItem{}, router.Errorf(409, "cancel queued mail before moving it")
		}
		if value == "drafts" && i.Status != "draft" || value == "sent" && i.Status != "sent" && i.Status != "captured" {
			return schema.MailItem{}, router.Errorf(422, "message cannot be moved to that folder")
		}
	}
	i, err = q.UpdateMailFlags(ctx, queries.UpdateMailFlagsParams{ID: i.ID, Folder: folder, Starred: r.Body.Starred, Unread: r.Body.Unread})
	if err != nil {
		return schema.MailItem{}, err
	}
	return mailbox.View(i), tx.Commit(ctx)
}
func SendMail(ctx context.Context, r *router.Request[schema.SendMailInput]) (schema.MailItem, error) {
	return mailbox.Queue(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"), r.Body)
}
func UndoMail(ctx context.Context, r *router.Request[router.None]) (schema.MailItem, error) {
	return mailbox.Cancel(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"))
}
func UploadMailAttachment(ctx context.Context, r *router.Request[router.File]) (schema.AttachmentView, error) {
	return mailbox.SaveAttachment(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id"), r.Body)
}
func DownloadMailAttachment(ctx context.Context, r *router.Request[router.None]) (schema.FileContent, error) {
	if _, err := mailbox.ItemAccess(ctx, r.Param("workspaceId"), r.Param("mailboxId"), r.Param("id")); err != nil {
		return schema.FileContent{}, err
	}
	if !workspace.ValidID(r.Param("attachmentId")) {
		return schema.FileContent{}, router.Errorf(404, "attachment not found")
	}
	a, err := queries.New(db.From(ctx)).GetMailAttachment(ctx, r.Param("attachmentId"))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && a.ItemID != r.Param("id") {
		return schema.FileContent{}, router.Errorf(404, "attachment not found")
	}
	if err != nil {
		return schema.FileContent{}, err
	}
	stream, _, err := storage.From(ctx).Get(ctx, a.ObjectKey)
	if err != nil {
		return schema.FileContent{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stream, (10<<20)+1))
	err = errors.Join(readErr, stream.Close())
	if len(data) > 10<<20 {
		return schema.FileContent{}, router.Errorf(413, "attachment exceeds download limit")
	}
	return schema.FileContent{Name: a.Name, ContentType: a.ContentType, Data: base64.StdEncoding.EncodeToString(data)}, err
}
