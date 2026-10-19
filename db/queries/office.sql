-- name: CreateOfficeSession :one
INSERT INTO office_session(file_id,subject,source_version,base_version,document_key,expires_at) VALUES($1,$2,$3,$3,$4,$5) RETURNING *;
-- name: GetOfficeSession :one
SELECT * FROM office_session WHERE id=$1;
-- name: LockOfficeSession :one
SELECT * FROM office_session WHERE id=$1 FOR UPDATE;
-- name: SaveOfficeSession :exec
UPDATE office_session SET base_version=$2,last_checksum=$3 WHERE id=$1;
-- name: FindOfficeSourceSession :one
SELECT * FROM office_session WHERE file_id=$1 AND subject=$2 AND source_version=$3 AND expires_at>now() ORDER BY created_at DESC LIMIT 1;
