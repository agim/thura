-- name: GetSmipPolicy :one
SELECT * FROM smip_policy WHERE workspace_id=$1;
-- name: SaveSmipPolicy :exec
INSERT INTO smip_policy(workspace_id,chat,files,updated_by,updated_at) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(workspace_id) DO UPDATE SET chat=EXCLUDED.chat,files=EXCLUDED.files,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at;
-- name: ListSmipPolicyDomains :many
SELECT domain FROM smip_policy_domain WHERE workspace_id=$1 ORDER BY domain LIMIT 200;
-- name: ClearSmipPolicyDomains :exec
DELETE FROM smip_policy_domain WHERE workspace_id=$1;
-- name: AddSmipPolicyDomain :exec
INSERT INTO smip_policy_domain(workspace_id,domain) VALUES($1,$2) ON CONFLICT(workspace_id,domain) DO NOTHING;
-- name: ListSmipBlockedDomains :many
SELECT * FROM smip_blocked_domain WHERE workspace_id=$1 ORDER BY domain LIMIT 500;
-- name: SmipDomainBlocked :one
SELECT EXISTS(SELECT 1 FROM smip_blocked_domain WHERE workspace_id=$1 AND domain=$2);
-- name: BlockSmipDomain :exec
INSERT INTO smip_blocked_domain(workspace_id,domain,blocked_by,blocked_at) VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,domain) DO NOTHING;
-- name: UnblockSmipDomain :execrows
DELETE FROM smip_blocked_domain WHERE workspace_id=$1 AND domain=$2;
-- name: FindSmipConversation :one
SELECT * FROM smip_conversation WHERE workspace_id=$1 AND local_address=$2 AND remote_address=$3;
-- name: AddSmipConversation :one
INSERT INTO smip_conversation(id,workspace_id,mailbox_id,owner_subject,local_address,remote_address,remote_domain,stream,state,started_by,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING *;
-- name: GetSmipConversation :one
SELECT * FROM smip_conversation WHERE workspace_id=$1 AND id=$2;
-- name: LockSmipConversation :one
SELECT * FROM smip_conversation WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: DecideSmipConversation :exec
UPDATE smip_conversation SET state=$3,decided_by=$4,decided_at=$5,updated_at=$5 WHERE workspace_id=$1 AND id=$2;
-- name: TouchSmipConversation :exec
UPDATE smip_conversation SET updated_at=$3 WHERE workspace_id=$1 AND id=$2;
-- name: CountSmipConversations :one
SELECT COUNT(*) FROM smip_conversation WHERE workspace_id=$1;
-- name: CountSmipConversationInbox :one
SELECT COUNT(*) FROM smip_inbox WHERE conversation_id=$1;
-- name: ListSmipConversations :many
SELECT c.*,COALESCE(p.kind,'')::text AS preview_kind,COALESCE(p.body,'')::text AS preview_body,COALESCE(p.name,'')::text AS preview_name,
(SELECT COUNT(*) FROM smip_inbox i WHERE i.conversation_id=c.id)::bigint AS received
FROM smip_conversation c LEFT JOIN LATERAL (SELECT i.kind,i.body,i.name FROM smip_inbox i WHERE i.conversation_id=c.id ORDER BY i.accepted_at DESC,i.id DESC LIMIT 1) p ON true
WHERE c.workspace_id=$1 AND (c.owner_subject=sqlc.arg(subject)::text OR c.owner_subject IS NULL) ORDER BY c.updated_at DESC,c.id LIMIT 200;
-- name: ListSmipConversationOverview :many
SELECT c.id,c.local_address,c.remote_address,c.remote_domain,c.state,c.updated_at,
((SELECT COUNT(*) FROM smip_inbox i WHERE i.conversation_id=c.id)+(SELECT COUNT(*) FROM smip_outbox o WHERE o.conversation_id=c.id))::bigint AS messages
FROM smip_conversation c WHERE c.workspace_id=$1 ORDER BY c.updated_at DESC,c.id LIMIT 200;
-- name: ListSmipConversationInbox :many
SELECT id,kind,body,name,size,accepted_at,imported_file_id,imported_at,declined_at FROM smip_inbox WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY accepted_at,id LIMIT 500;
-- name: ListSmipConversationOutbox :many
SELECT id,kind,body,name,size,state,reason,created_at FROM smip_outbox WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at,id LIMIT 500;
-- name: LockSmipConversationInbox :one
SELECT * FROM smip_inbox WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE;
-- name: DeclineSmipFile :exec
UPDATE smip_inbox SET declined_at=$3,decided_by=$4 WHERE workspace_id=$1 AND id=$2;
