-- name: ListPushDevices :many
SELECT * FROM push_subscription WHERE workspace_id=$1 AND subject=$2 ORDER BY created_at, id;
-- name: GetPushSubscription :one
SELECT * FROM push_subscription WHERE id=$1;
-- name: CountPushDevices :one
SELECT count(*) FROM push_subscription WHERE workspace_id=$1 AND subject=$2;
-- name: SavePushSubscription :one
-- A browser has one endpoint: subscribing again (a new key, another member
-- signed in on the same browser) replaces the row for this workspace.
INSERT INTO push_subscription(workspace_id,subject,endpoint,endpoint_hash,p256dh,auth,label)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(workspace_id,endpoint_hash) DO UPDATE SET subject=EXCLUDED.subject, endpoint=EXCLUDED.endpoint,
  p256dh=EXCLUDED.p256dh, auth=EXCLUDED.auth, label=EXCLUDED.label,
  last_error_at=NULL, last_error_kind='', last_error='',
  created_at=CASE WHEN push_subscription.subject=EXCLUDED.subject THEN push_subscription.created_at ELSE now() END
RETURNING *;
-- name: DeletePushDevice :execrows
DELETE FROM push_subscription WHERE workspace_id=$1 AND subject=$2 AND id=$3;
-- name: DeletePushSubscription :exec
DELETE FROM push_subscription WHERE id=$1;
-- name: ListPushRecipients :many
-- The subscriptions of the given members whose membership is current.
SELECT s.* FROM push_subscription s
WHERE s.workspace_id=$1 AND s.subject = ANY(sqlc.arg(subjects)::text[])
  AND EXISTS(SELECT 1 FROM auth_member m WHERE m.scope=s.workspace_id::text AND m.subject=s.subject AND m.role IN ('owner','admin','member'))
ORDER BY s.subject, s.created_at, s.id;
-- name: GetNotificationPreference :one
SELECT * FROM notification_preference WHERE workspace_id=$1 AND subject=$2;
-- name: ListNotificationPreferences :many
SELECT * FROM notification_preference WHERE workspace_id=$1 AND subject = ANY(sqlc.arg(subjects)::text[]);
-- name: SaveNotificationPreference :one
INSERT INTO notification_preference(workspace_id,subject,mail,mail_people_only,chat,chat_requests,calendar,meet,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now())
ON CONFLICT(workspace_id,subject) DO UPDATE SET mail=EXCLUDED.mail, mail_people_only=EXCLUDED.mail_people_only,
  chat=EXCLUDED.chat, chat_requests=EXCLUDED.chat_requests, calendar=EXCLUDED.calendar, meet=EXCLUDED.meet, updated_at=now()
RETURNING *;
-- name: GetPushVapidKey :one
SELECT * FROM push_vapid_key WHERE name=$1;
-- name: AddPushVapidKey :exec
INSERT INTO push_vapid_key(name,public_key,private_key) VALUES($1,$2,$3) ON CONFLICT(name) DO NOTHING;
-- name: PushMailArrival :one
SELECT i.id, i.mailbox_id, i.folder, i.from_address, i.subject, b.workspace_id, b.private, b.owner_subject
FROM mail_item i JOIN mailbox b ON b.id=i.mailbox_id WHERE i.id=$1;
-- name: ListPushWorkspaceMembers :many
SELECT DISTINCT m.subject FROM auth_member m WHERE m.scope=$1 AND m.role IN ('owner','admin','member') ORDER BY m.subject;
-- name: ListPushChatParticipants :many
SELECT p.subject FROM chat_participant p JOIN chat_room r ON r.id=p.room_id WHERE r.workspace_id=$1 AND r.id=$2 ORDER BY p.subject;
-- name: PushAccountsByEmail :many
SELECT DISTINCT m.subject FROM auth_member m JOIN auth_user u ON u.subject=m.subject
WHERE m.scope=$1 AND m.role IN ('owner','admin','member') AND lower(u.email) = ANY(sqlc.arg(emails)::text[]);
-- name: PushAccountName :one
SELECT coalesce(nullif(trim(name),''), email, '')::text FROM auth_user WHERE subject=$1;
-- name: RecordPushAttempt :exec
-- One delivery attempt: when, for which kind, and its outcome. A delivery
-- that landed counts once; a failure counts once when it is final (a
-- retried attempt records its error without counting).
UPDATE push_subscription SET last_attempt_at=sqlc.arg(at), last_kind=sqlc.arg(kind),
  last_used_at=CASE WHEN sqlc.arg(delivered)::bool THEN sqlc.arg(at) ELSE last_used_at END,
  delivered_count=delivered_count + CASE WHEN sqlc.arg(delivered)::bool THEN 1 ELSE 0 END,
  last_error_at=CASE WHEN sqlc.arg(delivered)::bool THEN last_error_at ELSE sqlc.arg(at) END,
  last_error_kind=CASE WHEN sqlc.arg(delivered)::bool THEN last_error_kind ELSE sqlc.arg(error_kind)::text END,
  last_error=CASE WHEN sqlc.arg(delivered)::bool THEN last_error ELSE sqlc.arg(error)::text END,
  failed_count=failed_count + CASE WHEN sqlc.arg(final_failure)::bool THEN 1 ELSE 0 END
WHERE id=sqlc.arg(id);
-- name: PushJobFinalAttempt :one
-- Whether the running delivery job is on its last attempt (the queue
-- counts an attempt when it claims the job).
SELECT coalesce(bool_or(attempts >= max_attempts), false)::bool FROM job
WHERE kind=$1 AND state='running' AND payload->>'delivery' = sqlc.arg(delivery)::text;
-- name: AddPushRemovedDevice :exec
INSERT INTO push_removed_device(workspace_id,subject,endpoint_hash,label,error_kind,error) VALUES($1,$2,$3,$4,$5,$6);
-- name: PrunePushRemovedDevices :exec
DELETE FROM push_removed_device d WHERE d.workspace_id=$1 AND d.subject=$2 AND d.id NOT IN (
  SELECT k.id FROM push_removed_device k WHERE k.workspace_id=$1 AND k.subject=$2 ORDER BY k.removed_at DESC, k.id DESC LIMIT 10);
-- name: ClearPushRemovedDevice :exec
DELETE FROM push_removed_device WHERE workspace_id=$1 AND subject=$2 AND endpoint_hash=$3;
-- name: ListPushRemovedDevices :many
SELECT * FROM push_removed_device WHERE workspace_id=$1 AND subject=$2 ORDER BY removed_at DESC, id DESC LIMIT 10;
-- name: AddNotificationLog :one
INSERT INTO notification_log(workspace_id,subject,kind,link,status,reason,devices) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id;
-- name: PruneNotificationLog :exec
-- The ring buffer: the newest 50 rows of a member in a workspace.
DELETE FROM notification_log l WHERE l.workspace_id=$1 AND l.subject=$2 AND l.id NOT IN (
  SELECT k.id FROM notification_log k WHERE k.workspace_id=$1 AND k.subject=$2 ORDER BY k.created_at DESC, k.id DESC LIMIT 50);
-- name: ListNotificationLog :many
SELECT * FROM notification_log WHERE workspace_id=$1 AND subject=$2 ORDER BY created_at DESC, id DESC LIMIT $3;
-- name: NotificationLogDelivered :exec
-- A device accepted it: the event is delivered, whatever another device did.
UPDATE notification_log SET delivered=delivered+1, status='delivered', updated_at=now() WHERE id=$1;
-- name: NotificationLogFailed :exec
-- A device failed for good: the event is failed unless another device took it.
UPDATE notification_log SET failed=failed+1, error=sqlc.arg(error)::text,
  status=CASE WHEN status='delivered' THEN status ELSE 'failed' END, updated_at=now() WHERE id=sqlc.arg(id);
-- name: NotificationLogOpen :execrows
-- The service worker showed nothing because a focused tab showed the view.
UPDATE notification_log SET reason='open', updated_at=now() WHERE workspace_id=$1 AND subject=$2 AND id=$3;
