-- name: CreateMailbox :one
INSERT INTO mailbox(workspace_id,name,address,config_prefix) VALUES($1,$2,$3,$4) RETURNING *;
-- name: ListMailboxes :many
SELECT * FROM mailbox WHERE workspace_id=$1 ORDER BY name,id;
-- name: GetMailbox :one
SELECT * FROM mailbox WHERE id=$1;
-- name: LockMailbox :one
SELECT * FROM mailbox WHERE id=$1 FOR UPDATE;
-- name: FindInboundMail :one
SELECT * FROM mail_item WHERE mailbox_id=$1 AND external_id=$2;
-- name: CreateDraft :one
INSERT INTO mail_item(mailbox_id,author_id,folder,from_address,to_address,cc,bcc,subject,text_body,html_body,thread_id)
VALUES($1,$2,'drafts',$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;
-- name: ListMailItems :many
SELECT * FROM mail_item WHERE mailbox_id=$1 AND folder=$2 ORDER BY updated_at DESC,id LIMIT 200;
-- name: ListMailItemsPage :many
SELECT * FROM mail_item
WHERE mailbox_id=sqlc.arg(mailbox_id) AND folder=sqlc.arg(folder)
AND (sqlc.arg(search)::text='' OR strpos(lower(from_address || ' ' || to_address || ' ' || subject || ' ' || text_body), lower(sqlc.arg(search))) > 0)
AND (sqlc.narg(before_time)::timestamptz IS NULL OR (updated_at,id) < (sqlc.narg(before_time)::timestamptz,sqlc.arg(before_id)::uuid))
ORDER BY updated_at DESC,id DESC LIMIT sqlc.arg(page_limit);
-- name: GetMailItem :one
SELECT * FROM mail_item WHERE id=$1;
-- name: LockMailItem :one
SELECT * FROM mail_item WHERE id=$1 FOR UPDATE;
-- name: UpdateDraft :one
UPDATE mail_item SET to_address=$2,cc=$3,bcc=$4,subject=$5,text_body=$6,html_body=$7,thread_id=$8,updated_at=now()
WHERE id=$1 AND status='draft' RETURNING *;
-- name: UpdateMailFlags :one
UPDATE mail_item SET folder=coalesce(sqlc.narg(folder),folder),starred=coalesce(sqlc.narg(starred),starred),unread=coalesce(sqlc.narg(unread),unread),updated_at=now()
WHERE id=$1 RETURNING *;
-- name: QueueMailItem :one
UPDATE mail_item SET status='queued',send_at=$2,updated_at=now() WHERE id=$1 AND status='draft' RETURNING *;
-- name: CancelMailItem :one
UPDATE mail_item SET status='draft',send_at=NULL,updated_at=now() WHERE id=$1 AND status='queued' RETURNING *;
-- name: SetMailOutcome :exec
UPDATE mail_item SET status=$2,provider_id=$3,folder=$4,updated_at=now() WHERE id=$1;
-- name: ListMailAttachments :many
SELECT * FROM mail_attachment WHERE item_id=$1 ORDER BY id;
-- name: GetMailAttachment :one
SELECT * FROM mail_attachment WHERE id=$1;
-- name: AddMailAttachment :one
INSERT INTO mail_attachment(item_id,name,content_type,size,object_key,content_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: InsertInboundMail :one
INSERT INTO mail_item(mailbox_id,author_id,folder,from_address,to_address,subject,text_body,html_body,status,unread,raw_key,external_id,thread_id)
VALUES($1,'','inbox',$2,$3,$4,$5,$6,'received',true,$7,$8,$9)
ON CONFLICT (mailbox_id,external_id) DO UPDATE SET external_id=EXCLUDED.external_id RETURNING *;
