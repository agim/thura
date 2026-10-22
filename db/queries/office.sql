-- name: CreateOfficeSession :one
INSERT INTO office_session(file_id,subject,source_version,document_key,expires_at) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: GetOfficeSession :one
SELECT * FROM office_session WHERE id=$1;
-- name: DeleteExpiredOfficeSessions :execrows
DELETE FROM office_session WHERE id IN (SELECT s.id FROM office_session s WHERE s.expires_at<$1 ORDER BY s.expires_at LIMIT 1000);
