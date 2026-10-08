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
AND (sqlc.narg(label_id)::uuid IS NULL OR EXISTS (SELECT 1 FROM mail_tag t WHERE t.item_id=mail_item.id AND t.label_id=sqlc.narg(label_id)::uuid))
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
INSERT INTO mail_item(mailbox_id,author_id,folder,from_address,to_address,cc,subject,text_body,html_body,status,unread,raw_key,external_id,thread_id,message_id,in_reply_to,references_header)
VALUES($1,'','inbox',$2,$3,$4,$5,$6,$7,'received',true,$8,$9,$10,$11,$12,$13)
ON CONFLICT (mailbox_id,external_id) DO UPDATE SET external_id=EXCLUDED.external_id RETURNING *;
-- name: DeleteMailAttachment :execrows
DELETE FROM mail_attachment WHERE id=$1 AND item_id=$2;

-- name: UpdateMailboxSignature :one
UPDATE mailbox SET signature=$2 WHERE id=$1 RETURNING *;
-- name: SetMailThreadHeaders :one
UPDATE mail_item SET in_reply_to=$2,references_header=$3,thread_id=$4 WHERE id=$1 RETURNING *;
-- name: FindMailThreadParent :one
SELECT * FROM mail_item WHERE mailbox_id=$1 AND (message_id=$2 OR provider_id=$2) ORDER BY created_at DESC,id DESC LIMIT 1;
-- name: ListMailThreadPage :many
SELECT * FROM mail_item WHERE mailbox_id=sqlc.arg(mailbox_id) AND (id=sqlc.arg(item_id) OR (thread_id<>'' AND thread_id=sqlc.arg(thread_id)))
AND (sqlc.narg(before_time)::timestamptz IS NULL OR (updated_at,id)<(sqlc.narg(before_time)::timestamptz,sqlc.arg(before_id)::uuid))
ORDER BY updated_at DESC,id DESC LIMIT sqlc.arg(page_limit);
-- name: ListMailboxLabels :many
SELECT * FROM mail_label WHERE mailbox_id=$1 ORDER BY name,id;
-- name: CreateMailboxLabel :one
INSERT INTO mail_label(mailbox_id,name) VALUES($1,$2) RETURNING *;
-- name: GetMailboxLabel :one
SELECT * FROM mail_label WHERE id=$1 AND mailbox_id=$2;
-- name: ListItemLabels :many
SELECT l.* FROM mail_label l JOIN mail_tag t ON t.label_id=l.id WHERE t.item_id=$1 ORDER BY l.name,l.id;
-- name: AddMailTag :exec
INSERT INTO mail_tag(item_id,label_id) VALUES($1,$2) ON CONFLICT(item_id,label_id) DO NOTHING;
-- name: RemoveMailTag :exec
DELETE FROM mail_tag WHERE item_id=$1 AND label_id=$2;
-- name: DeleteLabelTags :exec
DELETE FROM mail_tag WHERE label_id=$1;
-- name: DeleteMailboxLabel :exec
DELETE FROM mail_label WHERE id=$1 AND mailbox_id=$2;
