-- name: ListWorkspaceLabels :many
SELECT l.* FROM mail_label l LEFT JOIN mailbox b ON b.id=l.mailbox_id WHERE COALESCE(l.workspace_id,b.workspace_id)=$1 ORDER BY lower(l.name),l.id;
-- name: GetWorkspaceLabel :one
SELECT l.* FROM mail_label l LEFT JOIN mailbox b ON b.id=l.mailbox_id WHERE l.id=sqlc.arg(id) AND COALESCE(l.workspace_id,b.workspace_id)=sqlc.arg(workspace_id);
-- name: CreateWorkspaceLabel :one
INSERT INTO mail_label(workspace_id,name) VALUES($1,$2) RETURNING *;
-- name: DeleteResourceLabelAssignments :exec
DELETE FROM resource_label WHERE label_id=$1;
-- name: ListResourceLabels :many
SELECT l.* FROM mail_label l JOIN resource_label r ON r.label_id=l.id LEFT JOIN mailbox b ON b.id=l.mailbox_id
WHERE COALESCE(l.workspace_id,b.workspace_id)=sqlc.arg(workspace_id) AND r.source=sqlc.arg(source) AND r.source_id=sqlc.arg(source_id) ORDER BY l.name,l.id;
-- name: AddResourceLabel :exec
INSERT INTO resource_label(label_id,source,source_id) VALUES($1,$2,$3) ON CONFLICT(label_id,source,source_id) DO NOTHING;
-- name: DeleteResourceLabelsOf :exec
DELETE FROM resource_label WHERE source=$1 AND source_id=$2;
-- name: RemoveResourceLabel :exec
DELETE FROM resource_label WHERE label_id=$1 AND source=$2 AND source_id=$3;
-- name: LabelResourceContainer :one
SELECT calendar_id AS container_id FROM calendar_event WHERE id=sqlc.arg(id);
-- name: LabelContactExists :one
SELECT id FROM contact WHERE id=$1 AND workspace_id=$2;
-- name: LabelMailContainer :one
SELECT mailbox_id AS container_id FROM mail_item WHERE id=$1;
-- name: DeleteWorkspaceLabel :exec
DELETE FROM mail_label l WHERE l.id=sqlc.arg(id) AND COALESCE(l.workspace_id,(SELECT b.workspace_id FROM mailbox b WHERE b.id=l.mailbox_id))=sqlc.arg(workspace_id);

-- name: ListDriveFileLabels :many
SELECT r.source_id AS file_id,l.id AS label_id,l.name FROM resource_label r
JOIN drive_file f ON f.id=r.source_id AND r.source='document'
JOIN mail_label l ON l.id=r.label_id LEFT JOIN mailbox b ON b.id=l.mailbox_id
WHERE f.workspace_id=sqlc.arg(workspace_id) AND COALESCE(l.workspace_id,b.workspace_id)=sqlc.arg(workspace_id)
ORDER BY r.source_id,lower(l.name),l.id;
