-- name: GetDrivePreview :one
SELECT * FROM drive_preview WHERE file_id=$1;
-- name: LockDrivePreview :one
SELECT * FROM drive_preview WHERE file_id=$1 FOR UPDATE;
-- name: ResetDrivePreview :one
INSERT INTO drive_preview(file_id,version) VALUES($1,$2)
ON CONFLICT(file_id) DO UPDATE SET version=excluded.version,status='pending',object_key='',checksum='',content_type='',width=0,height=0,truncated=false,job_id='',updated_at=now()
RETURNING *;
-- name: CompleteDrivePreview :exec
UPDATE drive_preview SET status=$3,object_key=$4,checksum=$5,content_type=$6,width=$7,height=$8,truncated=$9,updated_at=now() WHERE file_id=$1 AND version=$2;
-- name: DeleteDrivePreview :exec
DELETE FROM drive_preview WHERE file_id=$1;

-- name: SetDrivePreviewJob :exec
UPDATE drive_preview SET job_id=$2 WHERE file_id=$1;
