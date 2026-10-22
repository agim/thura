-- name: ListSmipPeers :many
SELECT * FROM smip_peer WHERE owner=$1 ORDER BY domain,state,key_id LIMIT 1000;
-- name: ListTrustedSmipPeers :many
SELECT * FROM smip_peer WHERE owner=$1 AND state='trusted' AND confirmed ORDER BY domain,key_id LIMIT 1000;
-- name: ListSmipPeerDomain :many
SELECT * FROM smip_peer WHERE owner=$1 AND domain=$2 AND state=$3 ORDER BY key_id;
-- name: CountSmipPeerDomains :one
SELECT COUNT(DISTINCT domain) FROM smip_peer WHERE owner=$1;
-- name: UpsertSmipPeer :exec
INSERT INTO smip_peer(owner,domain,origin,key_id,public_key,not_before,not_after,revoked,state,confirmed,paired_by,paired_at,fetched_at,notes)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT(owner,domain,key_id) DO UPDATE SET origin=EXCLUDED.origin,public_key=EXCLUDED.public_key,not_before=EXCLUDED.not_before,not_after=EXCLUDED.not_after,
revoked=EXCLUDED.revoked,state=EXCLUDED.state,confirmed=EXCLUDED.confirmed,paired_by=EXCLUDED.paired_by,paired_at=EXCLUDED.paired_at,fetched_at=EXCLUDED.fetched_at,notes=EXCLUDED.notes;
-- name: DeleteSmipPeerState :execrows
DELETE FROM smip_peer WHERE owner=$1 AND domain=$2 AND state=$3;
-- name: RevokeSmipPeer :execrows
UPDATE smip_peer SET revoked=true,state='revoked',notes=$3,fetched_at=$4 WHERE owner=$1 AND domain=$2 AND state<>'pending';
-- name: LockSmipPeers :exec
SELECT pg_advisory_xact_lock(hashtextextended('smip-peer:'||$1::text,0));
