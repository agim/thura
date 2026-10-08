-- name: ListChatRooms :many
SELECT r.* FROM chat_room r WHERE r.workspace_id=$1 AND (NOT r.direct OR EXISTS(SELECT 1 FROM chat_participant p WHERE p.room_id=r.id AND p.subject=$2)) ORDER BY r.created_at LIMIT 100;
-- name: GetChatRoom :one
SELECT r.* FROM chat_room r WHERE r.workspace_id=$1 AND r.id=$2 AND (NOT r.direct OR EXISTS(SELECT 1 FROM chat_participant p WHERE p.room_id=r.id AND p.subject=$3));
-- name: FindChatRequest :one
SELECT * FROM chat_room WHERE workspace_id=$1 AND request_id=$2;
-- name: CountWorkspaceRooms :one
SELECT COUNT(*) FROM chat_room WHERE workspace_id=$1;
-- name: AddChatRoom :one
INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,direct,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: AddChatParticipant :exec
INSERT INTO chat_participant(room_id,subject) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: ListRevokedChatActors :many
SELECT p.id,p.subject,r.workspace_id,r.matrix_room_id FROM chat_participant p JOIN chat_room r ON r.id=p.room_id WHERE p.id::text > $1 AND NOT EXISTS(SELECT 1 FROM auth_member m WHERE m.subject=p.subject AND m.scope=r.workspace_id::text AND m.role IN ('owner','admin','member')) ORDER BY p.id LIMIT 100;
-- name: DeleteChatParticipant :exec
DELETE FROM chat_participant WHERE id=$1;
