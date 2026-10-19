-- name: ListTeamDomains :many
SELECT * FROM team_domain WHERE workspace_id=$1 ORDER BY name;
-- name: GetTeamDomain :one
SELECT * FROM team_domain WHERE workspace_id=$1 AND id=$2;
-- name: LockTeamDomain :one
SELECT * FROM team_domain WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: AddTeamDomain :one
INSERT INTO team_domain(workspace_id,name,challenge,web,mail) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: VerifyTeamDomain :one
UPDATE team_domain SET status='verified',verified_at=COALESCE(verified_at,$3),checked_at=$3,valid_until=$4 WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: SuspendTeamDomain :exec
UPDATE team_domain SET status='suspended',checked_at=$3 WHERE workspace_id=$1 AND id=$2;
-- name: BindTeamDomain :execrows
INSERT INTO team_domain_binding(name,workspace_id,domain_id) VALUES($1,$2,$3) ON CONFLICT(name) DO UPDATE SET domain_id=EXCLUDED.domain_id WHERE team_domain_binding.workspace_id=EXCLUDED.workspace_id;
-- name: UnbindTeamDomain :exec
DELETE FROM team_domain_binding WHERE workspace_id=$1 AND domain_id=$2;
-- name: DeleteTeamMailConfig :exec
DELETE FROM team_mail_config WHERE workspace_id=$1 AND domain_id=$2;
-- name: DeleteTeamDomain :exec
DELETE FROM team_domain WHERE workspace_id=$1 AND id=$2;
-- name: BoundTeamDomain :one
SELECT d.* FROM team_domain d JOIN team_domain_binding b ON b.domain_id=d.id WHERE b.name=$1;
-- name: GetTeamSetup :one
SELECT * FROM team_setup WHERE workspace_id=$1;
-- name: SaveTeamSetup :one
INSERT INTO team_setup(workspace_id,primary_domain,status) VALUES($1,$2,$3) ON CONFLICT(workspace_id) DO UPDATE SET primary_domain=EXCLUDED.primary_domain,status=EXCLUDED.status,updated_at=now() RETURNING *;
-- name: ListTeamMailConfigs :many
SELECT * FROM team_mail_config WHERE workspace_id=$1 ORDER BY domain_id;
-- name: GetTeamMailConfig :one
SELECT * FROM team_mail_config WHERE workspace_id=$1 AND domain_id=$2;
-- name: SaveTeamMailConfig :one
INSERT INTO team_mail_config(domain_id,workspace_id,provider,sealed,enabled,smip,postfix) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(domain_id) DO UPDATE SET provider=EXCLUDED.provider,sealed=EXCLUDED.sealed,enabled=EXCLUDED.enabled,smip=EXCLUDED.smip,postfix=EXCLUDED.postfix,updated_at=now() WHERE team_mail_config.workspace_id=EXCLUDED.workspace_id RETURNING *;
-- name: DueTeamDomains :many
SELECT * FROM team_domain WHERE status='verified' AND verification='dns' AND (checked_at IS NULL OR checked_at < $1) ORDER BY checked_at NULLS FIRST LIMIT 100;
-- name: AllTeamMailConfigs :many
SELECT c.* FROM team_mail_config c JOIN team_domain d ON c.domain_id=d.id WHERE d.status='verified' AND c.enabled=true AND c.smip=true;
-- name: TeamMailboxAddressExists :one
SELECT EXISTS(SELECT 1 FROM mailbox WHERE lower(address)=lower($1));
-- name: DueAllMailTransmissions :many
SELECT id FROM mail_transmission WHERE state IN ('planning','pending') ORDER BY updated_at,id LIMIT 50;
-- name: ConflictingTeamDomainMailboxes :one
SELECT COUNT(*) FROM mailbox WHERE workspace_id<>$1 AND lower(split_part(address,'@',2))=$2;

-- name: ApproveTeamDomain :one
UPDATE team_domain SET status='verified',verification='administrator',verified_at=now(),checked_at=now(),valid_until=NULL WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: ListManagedTeams :many
SELECT * FROM workspace ORDER BY name,id;
-- name: EnsureTeamProviderPolicy :exec
INSERT INTO smip_mail_domain(origin,domain,state,ever_verified,endpoint,document,reason,checked_at,expires_at) VALUES($1,$2,'error',false,'','','administrator_provider_only',now(),now()) ON CONFLICT(origin,domain) DO NOTHING;

-- name: CalendarWorkspaceForGrant :one
SELECT workspace_id FROM calendar WHERE id=$1;
-- name: EnsureTeamSetup :exec
INSERT INTO team_setup(workspace_id) VALUES($1) ON CONFLICT(workspace_id) DO NOTHING;
-- name: ListAllSmipMailDomains :many
SELECT * FROM smip_mail_domain ORDER BY origin,domain LIMIT 1000;
