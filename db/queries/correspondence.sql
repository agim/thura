-- name: GetCorrespondenceContact :one
SELECT * FROM contact WHERE workspace_id=$1 AND id=$2;

-- name: CorrespondencePeople :many
SELECT * FROM contact WHERE workspace_id=sqlc.arg(workspace_id)
AND (id=sqlc.arg(contact_id) OR (sqlc.arg(organization)::boolean AND company<>'' AND lower(trim(company))=lower(trim(sqlc.arg(company)::text))));

-- name: CorrespondenceCandidates :many
SELECT m.* FROM mail_item m JOIN mailbox b ON b.id=m.mailbox_id
WHERE b.workspace_id=sqlc.arg(workspace_id)
AND m.status IN ('received','sent','captured') AND m.folder NOT IN ('trash','spam')
AND EXISTS (SELECT 1 FROM unnest(sqlc.arg(emails)::text[]) AS address
 WHERE strpos(lower(m.from_address || ' ' || m.to_address || ' ' || m.cc || ' ' || m.bcc),address)>0)
AND (sqlc.arg(search)::text='' OR strpos(lower(m.subject || ' ' || m.text_body || ' ' || m.from_address || ' ' || m.to_address),lower(sqlc.arg(search)))>0)
AND (sqlc.narg(before_time)::timestamptz IS NULL OR (m.created_at,m.id)<(sqlc.narg(before_time)::timestamptz,sqlc.arg(before_id)::uuid))
ORDER BY m.created_at DESC,m.id DESC LIMIT sqlc.arg(page_limit);
