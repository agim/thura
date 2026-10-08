-- name: LockWorkspace :one
SELECT * FROM workspace WHERE id = $1 FOR UPDATE;

-- name: WorkspaceRole :one
SELECT role FROM auth_member WHERE scope=$1 AND subject=$2
AND role IN ('owner','admin','member')
ORDER BY CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END LIMIT 1;

-- name: ListWorkspaceMembers :many
SELECT m.subject, coalesce(u.email,'')::text AS email, coalesce(u.name,'')::text AS name, m.role
FROM auth_member m JOIN auth_user u ON u.subject=m.subject
WHERE m.scope=$1 AND m.role IN ('owner','admin','member')
ORDER BY m.role,u.email;

-- name: CountWorkspaceOwners :one
SELECT count(DISTINCT subject) FROM auth_member WHERE scope=$1 AND role='owner';

-- name: RemoveWorkspaceMember :exec
DELETE FROM auth_member WHERE scope=$1 AND subject=$2;

-- name: GrantWorkspaceMember :exec
INSERT INTO auth_member(scope,subject,role,granted_by) VALUES($1,$2,$3,$4)
ON CONFLICT (subject,scope,role) DO NOTHING;

-- name: CreateInvitation :one
INSERT INTO invitation(workspace_id,email,role,token_hash,invited_by,expires_at)
VALUES($1,$2,$3,$4,$5,$6) RETURNING *;

-- name: ListInvitations :many
SELECT * FROM invitation WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100;

-- name: LockInvitation :one
SELECT * FROM invitation WHERE token_hash=$1 FOR UPDATE;

-- name: FindInvitation :one
SELECT * FROM invitation WHERE token_hash=$1;

-- name: AcceptInvitation :exec
UPDATE invitation SET accepted_at=now() WHERE id=$1;

-- name: RevokeInvitation :execrows
UPDATE invitation SET revoked_at=now() WHERE workspace_id=$1 AND id=$2 AND accepted_at IS NULL;

-- name: FindInvitedAccount :one
SELECT subject FROM auth_user WHERE email=$1;

-- name: CreateInvitedAccount :one
INSERT INTO auth_user(subject,email,name,password_hash,verified_at)
VALUES($1,$2,$3,$4,now()) RETURNING subject;
