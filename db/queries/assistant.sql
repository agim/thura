-- name: GetAssistantCache :one
SELECT text,model FROM assistant_cache
WHERE workspace_id=$1 AND cache_key=$2 AND lease_token IS NULL AND expires_at>now();

-- name: ClaimAssistantCache :one
INSERT INTO assistant_cache(workspace_id,cache_key,lease_token,text,model,expires_at)
VALUES($1,$2,sqlc.arg(lease_token)::uuid,'','',now()+interval '45 seconds')
ON CONFLICT(workspace_id,cache_key) DO UPDATE
SET lease_token=EXCLUDED.lease_token,text='',model='',created_at=now(),expires_at=EXCLUDED.expires_at
WHERE assistant_cache.expires_at<=now()
RETURNING lease_token;

-- name: StoreAssistantCache :execrows
UPDATE assistant_cache SET text=$3,model=$4,lease_token=NULL,created_at=now(),expires_at=now()+interval '30 days'
WHERE workspace_id=$1 AND cache_key=$2 AND lease_token=sqlc.arg(lease_token)::uuid;

-- name: ReleaseAssistantCache :exec
DELETE FROM assistant_cache WHERE workspace_id=$1 AND cache_key=$2 AND lease_token=sqlc.arg(lease_token)::uuid;

-- name: PruneAssistantCache :exec
DELETE FROM assistant_cache c WHERE c.workspace_id=$1 AND
(c.expires_at<=now() OR c.id IN (SELECT old.id FROM assistant_cache old WHERE old.workspace_id=$1 AND old.lease_token IS NULL ORDER BY old.created_at DESC,old.id DESC OFFSET 1000));
