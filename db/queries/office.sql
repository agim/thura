-- name: CreateOfficeSession :one
INSERT INTO office_session(file_id,subject,source_version,document_key,expires_at) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: GetOfficeSession :one
SELECT * FROM office_session WHERE id=$1;
