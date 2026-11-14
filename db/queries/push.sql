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
  created_at=CASE WHEN push_subscription.subject=EXCLUDED.subject THEN push_subscription.created_at ELSE now() END
RETURNING *;
-- name: DeletePushDevice :execrows
DELETE FROM push_subscription WHERE workspace_id=$1 AND subject=$2 AND id=$3;
-- name: DeletePushSubscription :exec
DELETE FROM push_subscription WHERE id=$1;
-- name: TouchPushSubscription :exec
UPDATE push_subscription SET last_used_at=$2 WHERE id=$1;
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
