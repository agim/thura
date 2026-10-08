-- name: DriveWorkspaceUsage :one
SELECT (SELECT COALESCE(SUM(v.size),0)::bigint FROM file_version v JOIN drive_file f ON f.id=v.file_id WHERE f.workspace_id=$1)::bigint AS retained,
(SELECT COALESCE(SUM(expected_size),0)::bigint FROM upload_session WHERE workspace_id=$1 AND completed_at IS NULL AND expires_at>now() AND (sqlc.narg(ignore_upload_id)::uuid IS NULL OR id<>sqlc.narg(ignore_upload_id)::uuid))::bigint AS reserved;
-- name: CountWorkspaceFiles :one
SELECT COUNT(*) FROM drive_file WHERE workspace_id=$1;
-- name: CountWorkspaceFolders :one
SELECT COUNT(*) FROM drive_folder WHERE workspace_id=$1;
-- name: CountPendingNewFiles :one
SELECT COUNT(*) FROM upload_session WHERE workspace_id=$1 AND file_id IS NULL AND completed_at IS NULL AND expires_at>now();
-- name: CountWorkspaceUploads :one
SELECT COUNT(*) FROM upload_session WHERE workspace_id=$1 AND completed_at IS NULL AND expires_at>now();
-- name: GetUploadChunk :one
SELECT * FROM upload_chunk WHERE session_id=$1 AND number=$2;
-- name: ListExpiredUploads :many
SELECT * FROM upload_session WHERE expires_at < $1 OR completed_at < $2 ORDER BY expires_at LIMIT 100;
-- name: LockUploadForCleanup :one
SELECT * FROM upload_session WHERE id=$1 FOR UPDATE;
-- name: DeleteUploadChunks :exec
DELETE FROM upload_chunk WHERE session_id=$1;
-- name: DeleteUploadSession :exec
DELETE FROM upload_session WHERE id=$1;
-- name: ListFileUploads :many
SELECT * FROM upload_session WHERE file_id=$1;
-- name: DeleteFileShares :exec
DELETE FROM share_grant WHERE file_id=$1;
-- name: DeleteFileOfficeSessions :exec
DELETE FROM office_session WHERE file_id=$1;
-- name: DeleteFileVersions :exec
DELETE FROM file_version WHERE file_id=$1;
-- name: DeleteDriveFile :exec
DELETE FROM drive_file WHERE workspace_id=$1 AND id=$2;
-- name: DriveObjectReferenced :one
SELECT (EXISTS(SELECT 1 FROM file_version v WHERE v.object_key=$1) OR EXISTS(SELECT 1 FROM upload_chunk c WHERE c.object_key=$1))::boolean AS referenced;
