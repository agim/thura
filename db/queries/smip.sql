-- name: GetSmipBinding :one
SELECT * FROM smip_binding WHERE workspace_id=$1 AND id=$2;
-- name: FindSmipBinding :one
SELECT * FROM smip_binding WHERE peer=$1 AND stream=$2 AND recipient=$3;
-- name: ListSmipBindings :many
SELECT * FROM smip_binding WHERE workspace_id=$1 ORDER BY created_at,id LIMIT 50;
-- name: CountSmipBindings :one
SELECT COUNT(*) FROM smip_binding WHERE workspace_id=$1;
-- name: AddSmipBinding :one
INSERT INTO smip_binding(id,workspace_id,peer,stream,sender,recipient,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING *;
-- name: DisableSmipBinding :exec
UPDATE smip_binding SET enabled=false WHERE workspace_id=$1 AND id=$2;
-- name: FindSmipReceipt :one
SELECT * FROM smip_inbox WHERE origin=$1 AND message_id=$2;
-- name: GetSmipInbox :one
SELECT * FROM smip_inbox WHERE workspace_id=$1 AND id=$2;
-- name: LockSmipInbox :one
SELECT * FROM smip_inbox WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: SmipInboxUsage :one
SELECT COUNT(*) AS records,COALESCE(SUM(size),0)::bigint AS bytes FROM smip_inbox WHERE workspace_id=$1;
-- name: AcceptSmipPacket :one
INSERT INTO smip_inbox(workspace_id,binding_id,origin,message_id,digest,record,origin_key,receipt_key,kind,size,accepted_at,sender,recipient,stream,body,name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *;
-- name: ListSmipInbox :many
SELECT id,binding_id,origin,message_id,sender,recipient,stream,kind,body,name,size,digest,accepted_at,imported_file_id,imported_at FROM smip_inbox WHERE workspace_id=$1 AND id::text > $2 ORDER BY id LIMIT 51;
-- name: ImportSmipFile :exec
UPDATE smip_inbox SET imported_file_id=$3,imported_at=$4 WHERE workspace_id=$1 AND id=$2;
-- name: GetSmipOutbound :one
SELECT * FROM smip_outbox WHERE workspace_id=$1 AND id=$2;
-- name: LockSmipOutbound :one
SELECT * FROM smip_outbox WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: FindSmipOutbound :one
SELECT * FROM smip_outbox WHERE id=$1;
-- name: SmipOutboxUsage :one
SELECT COUNT(*) AS records,COALESCE(SUM(size),0)::bigint AS bytes FROM smip_outbox WHERE workspace_id=$1;
-- name: AddSmipOutbound :one
INSERT INTO smip_outbox(id,workspace_id,binding_id,subject,packet,origin_key,kind,body,name,size,source_file_id,next_attempt,created_at,updated_at,origin) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,$12,$13) RETURNING *;
-- name: ListSmipOutbox :many
SELECT id,binding_id,kind,body,name,size,source_file_id,state,reason,attempts,last_status,next_attempt,created_at,updated_at FROM smip_outbox WHERE workspace_id=$1 AND id::text > $2 ORDER BY id LIMIT 51;
-- name: UpdateSmipOutbound :exec
UPDATE smip_outbox SET state=$3,reason=$4,attempts=$5,attempt_id=$6,last_status=$7,next_attempt=$8,receipt=$9,receipt_key=$10,updated_at=$11 WHERE workspace_id=$1 AND id=$2;
-- name: DueSmipOutbound :many
SELECT id FROM smip_outbox WHERE origin=$1 AND state IN ('pending','uncertain') AND next_attempt<=$2 ORDER BY next_attempt,id LIMIT 50;
