-- name: EnsureWopiLock :exec
INSERT INTO wopi_lock(file_id,lock_id,base_version,expires_at) VALUES($1,'',$2,now()) ON CONFLICT(file_id) DO NOTHING;
-- name: LockWopiLock :one
SELECT * FROM wopi_lock WHERE file_id=$1 FOR UPDATE;
-- name: GetWopiLock :one
SELECT * FROM wopi_lock WHERE file_id=$1;
-- name: SetWopiLock :exec
UPDATE wopi_lock SET lock_id=$2,base_version=$3,expires_at=$4 WHERE file_id=$1;
-- name: DeleteWopiLock :exec
DELETE FROM wopi_lock WHERE file_id=$1;
