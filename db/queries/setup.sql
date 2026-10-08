-- name: EnsureServerSetup :exec
INSERT INTO server_setup(id) VALUES('00000000-0000-4000-8000-000000000001') ON CONFLICT DO NOTHING;
-- name: GetServerSetup :one
SELECT * FROM server_setup WHERE id='00000000-0000-4000-8000-000000000001';
-- name: LockServerSetup :one
SELECT * FROM server_setup WHERE id='00000000-0000-4000-8000-000000000001' FOR UPDATE;
-- name: CountSetupAccounts :one
SELECT COUNT(*) FROM auth_user;
-- name: ClaimServerSetup :exec
UPDATE server_setup SET subject=$1,draft=$2,revision=revision+1 WHERE id='00000000-0000-4000-8000-000000000001';
-- name: SaveServerSetup :exec
UPDATE server_setup SET draft=$1,step=$2,revision=revision+1 WHERE id='00000000-0000-4000-8000-000000000001';
-- name: PublishServerSetup :exec
UPDATE server_setup SET published=draft,published_revision=revision,published_at=$1,workspace_id=$2 WHERE id='00000000-0000-4000-8000-000000000001';
-- name: SetupHasStoredContent :one
SELECT EXISTS(SELECT 1 FROM drive_file UNION ALL SELECT 1 FROM mail_item UNION ALL SELECT 1 FROM upload_session) AS populated;
-- name: UpdateSetupWorkspace :exec
UPDATE workspace SET name=$2 WHERE id=$1;
-- name: SetupWorkspaceSeats :one
SELECT (SELECT COUNT(*) FROM auth_member WHERE scope=$1 AND role IN ('owner','admin','member')) + (SELECT COUNT(*) FROM invitation WHERE workspace_id=$1::uuid AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>$2) AS seats;
-- name: SetupWorkspaceMembers :one
SELECT COUNT(*) FROM auth_member WHERE scope=$1 AND role IN ('owner','admin','member');
