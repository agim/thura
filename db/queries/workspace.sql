-- name: ListWorkspaces :many
SELECT w.* FROM workspace w WHERE EXISTS (
  SELECT 1 FROM auth_member m WHERE m.scope = w.id::text AND m.subject = $1
  AND m.role IN ('owner', 'admin', 'member')
) ORDER BY w.name, w.id;

-- name: CreateWorkspace :one
INSERT INTO workspace(name) VALUES($1) RETURNING *;

-- name: GrantWorkspaceOwner :exec
INSERT INTO auth_member(subject, scope, role) VALUES($1, $2, 'owner');

-- name: IsWorkspaceMember :one
SELECT EXISTS(SELECT 1 FROM auth_member m JOIN workspace w ON w.id::text = m.scope
  WHERE m.scope = $1 AND m.subject = $2 AND m.role IN ('owner', 'admin', 'member'));

-- name: ListContacts :many
SELECT * FROM contact WHERE workspace_id = $1 ORDER BY name, id LIMIT 500;

-- name: CreateContact :one
INSERT INTO contact (workspace_id, name, email, company, phone, favorite)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: UpdateContact :one
UPDATE contact SET name = $3, email = $4, company = $5, phone = $6, favorite = $7
WHERE workspace_id = $1 AND id = $2 RETURNING *;

-- name: DeleteContact :execrows
DELETE FROM contact WHERE workspace_id = $1 AND id = $2;
