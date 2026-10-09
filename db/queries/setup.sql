-- name: EnsureServerSetup :exec
INSERT INTO server_setup(id) VALUES('00000000-0000-4000-8000-000000000001') ON CONFLICT DO NOTHING;
-- name: GetServerSetup :one
SELECT * FROM server_setup WHERE id='00000000-0000-4000-8000-000000000001';
-- name: LockServerSetup :one
SELECT * FROM server_setup WHERE id='00000000-0000-4000-8000-000000000001' FOR UPDATE;
-- name: GetServerSetupRouting :one
SELECT subject,published_revision,EXISTS(SELECT 1 FROM auth_user) AS has_accounts
FROM server_setup WHERE id='00000000-0000-4000-8000-000000000001';
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
-- name: GetSetupProbe :one
SELECT * FROM setup_probe WHERE id=$1;
-- name: CreateSetupProbe :exec
INSERT INTO setup_probe(id,kind,subject,revision,fingerprint,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$6);
-- name: FinishSetupProbe :exec
UPDATE setup_probe SET state=$2,updated_at=$3 WHERE id=$1 AND state='started';
-- name: CountRecentSetupProbes :one
SELECT COUNT(*) FROM setup_probe WHERE subject=$1 AND kind=$2 AND created_at>$3;
-- name: ListSetupProbes :many
SELECT * FROM setup_probe ORDER BY created_at DESC,id DESC LIMIT 20;
-- name: HasSuccessfulSetupProbe :one
SELECT EXISTS(SELECT 1 FROM setup_probe WHERE kind=$1 AND fingerprint=$2 AND state='succeeded' AND created_at>$3) AS succeeded;
-- name: SupersedeSetupProbeSuccesses :exec
UPDATE setup_probe SET state='superseded' WHERE kind=$1 AND fingerprint=$2 AND state='succeeded';
