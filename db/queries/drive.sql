-- name: ListDriveFolders :many
SELECT * FROM drive_folder WHERE workspace_id=$1 ORDER BY name LIMIT 500;
-- name: GetDriveFolder :one
SELECT * FROM drive_folder WHERE workspace_id=$1 AND id=$2;
-- name: CreateDriveFolder :one
INSERT INTO drive_folder(workspace_id,parent_id,name) VALUES($1,$2,$3) RETURNING *;
-- name: ListDriveFiles :many
SELECT * FROM drive_file WHERE workspace_id=$1 ORDER BY name LIMIT 500;
-- name: GetDriveFile :one
SELECT * FROM drive_file WHERE workspace_id=$1 AND id=$2;
-- name: LockDriveFile :one
SELECT * FROM drive_file WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: CreateDriveFile :one
INSERT INTO drive_file(workspace_id,folder_id,name,content_type,size) VALUES($1,$2,$3,$4,$5) RETURNING *;
-- name: UpdateDriveVersion :one
UPDATE drive_file SET size=$3,current_version=current_version+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: UpdateDriveFlags :one
UPDATE drive_file SET name=$3,folder_id=$4,trashed=$5,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING *;
-- name: AddFileVersion :one
INSERT INTO file_version(file_id,number,object_key,checksum,size,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetFileVersion :one
SELECT * FROM file_version WHERE file_id=$1 AND number=$2;
-- name: ListFileVersions :many
SELECT * FROM file_version WHERE file_id=$1 ORDER BY number DESC LIMIT 100;
-- name: CreateUpload :one
INSERT INTO upload_session(workspace_id,subject,file_id,folder_id,name,content_type,expected_size,base_version,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;
-- name: GetUpload :one
SELECT * FROM upload_session WHERE workspace_id=$1 AND id=$2 AND subject=$3;
-- name: LockUpload :one
SELECT * FROM upload_session WHERE workspace_id=$1 AND id=$2 AND subject=$3 FOR UPDATE;
-- name: CompleteUpload :exec
UPDATE upload_session SET completed_at=now(),file_id=$2 WHERE id=$1;
-- name: ListUploadChunks :many
SELECT * FROM upload_chunk WHERE session_id=$1 ORDER BY number;
-- name: PutUploadChunk :one
INSERT INTO upload_chunk(session_id,number,size,checksum,object_key) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(session_id,number) DO UPDATE SET size=excluded.size,checksum=excluded.checksum,object_key=excluded.object_key RETURNING *;
-- name: CreateShareGrant :one
INSERT INTO share_grant(file_id,token_hash,target_email,expires_at) VALUES($1,$2,$3,$4) RETURNING *;
-- name: GetShareGrant :one
SELECT * FROM share_grant WHERE token_hash=$1;
-- name: ListShareGrants :many
SELECT * FROM share_grant WHERE file_id=$1 ORDER BY created_at DESC LIMIT 100;
-- name: RevokeShareGrant :execrows
UPDATE share_grant SET revoked_at=now() WHERE id=$1 AND file_id=$2;
-- name: GetSharedFile :one
SELECT * FROM drive_file WHERE id=$1;
