-- name: ListSmipPinnedOrigins :many
SELECT * FROM smip_pinned_origin ORDER BY domain LIMIT 1000;
-- name: CountSmipPinnedOrigins :one
SELECT COUNT(*) FROM smip_pinned_origin;
-- name: UpsertSmipPinnedOrigin :exec
INSERT INTO smip_pinned_origin(domain,origin,added_by,added_at) VALUES($1,$2,$3,$4)
ON CONFLICT(domain) DO UPDATE SET origin=EXCLUDED.origin,added_by=EXCLUDED.added_by,added_at=EXCLUDED.added_at;
-- name: DeleteSmipPinnedOrigin :execrows
DELETE FROM smip_pinned_origin WHERE domain=$1;
-- name: GetSmipCaBundle :one
SELECT * FROM smip_ca_bundle WHERE id='default';
-- name: UpsertSmipCaBundle :exec
INSERT INTO smip_ca_bundle(id,pem,updated_by,updated_at) VALUES('default',$1,$2,$3)
ON CONFLICT(id) DO UPDATE SET pem=EXCLUDED.pem,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at;
-- name: DeleteSmipCaBundle :execrows
DELETE FROM smip_ca_bundle WHERE id='default';
-- name: LockSmipTrust :exec
SELECT pg_advisory_xact_lock(hashtextextended('smip-trust',0));
