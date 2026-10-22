-- name: ListMeetings :many
SELECT * FROM meeting WHERE workspace_id=$1 ORDER BY (ended_at IS NOT NULL),created_at DESC LIMIT 100;
-- name: GetMeeting :one
SELECT * FROM meeting WHERE workspace_id=$1 AND id=$2;
-- name: FindMeetingRequest :one
SELECT * FROM meeting WHERE workspace_id=$1 AND request_id=$2;
-- name: LockMeeting :one
SELECT * FROM meeting WHERE workspace_id=$1 AND id=$2 FOR UPDATE;
-- name: FindMeeting :one
SELECT * FROM meeting WHERE id=$1;
-- name: CountActiveMeetings :one
SELECT COUNT(*) FROM meeting WHERE workspace_id=$1 AND ended_at IS NULL;
-- name: AddMeeting :one
INSERT INTO meeting(workspace_id,request_id,name,created_by) VALUES($1,$2,$3,$4) RETURNING *;
-- name: EndMeeting :one
UPDATE meeting SET ended_at=now() WHERE id=$1 RETURNING *;
-- name: SaveMeetingToken :exec
INSERT INTO meeting_participant(meeting_id,subject,token_expires_at) VALUES($1,$2,$3) ON CONFLICT(meeting_id,subject) DO UPDATE SET token_expires_at=excluded.token_expires_at,updated_at=now();
-- name: JoinMeetingSession :exec
UPDATE meeting_participant SET joined=true,session_id=$3,updated_at=now() WHERE meeting_id=$1 AND subject=$2;
-- name: LeaveMeetingSession :exec
UPDATE meeting_participant SET joined=false,updated_at=now() WHERE meeting_id=$1 AND subject=$2 AND session_id=$3;
-- name: ListRevokedMeetingParticipants :many
SELECT p.*,m.workspace_id FROM meeting_participant p JOIN meeting m ON m.id=p.meeting_id
WHERE (p.joined OR p.token_expires_at > now()) AND (m.ended_at IS NOT NULL OR
(p.guest_invite_id IS NULL AND NOT EXISTS(SELECT 1 FROM auth_member a WHERE a.subject=p.subject AND a.scope=m.workspace_id::text AND a.role IN ('owner','admin','member'))) OR
(p.guest_invite_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM meeting_invite i JOIN calendar_event e ON e.id=i.event_id WHERE i.id=p.guest_invite_id AND i.meeting_id=p.meeting_id AND e.meeting_id=i.meeting_id AND NOT e.cancelled AND i.expires_at>now()))) ORDER BY p.updated_at LIMIT 100;
-- name: ClearMeetingParticipant :exec
UPDATE meeting_participant SET joined=false,updated_at=now() WHERE id=$1;
-- name: FindMeetingWebhook :one
SELECT * FROM meeting_webhook WHERE event_id=$1;
-- name: AddMeetingWebhook :exec
INSERT INTO meeting_webhook(event_id,checksum) VALUES($1,$2);
-- name: ExpireMeetingWebhooks :exec
DELETE FROM meeting_webhook WHERE created_at < now()-interval '30 days';

-- name: CreateMeetingInvite :one
INSERT INTO meeting_invite(meeting_id,event_id,code,pin_hash,pin_sealed,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(code) DO NOTHING RETURNING *;
-- name: MeetingInviteByEvent :one
SELECT * FROM meeting_invite WHERE event_id=$1;
-- name: MeetingInviteByCode :one
SELECT * FROM meeting_invite WHERE code=$1;
-- name: UpdateMeetingInviteExpiry :exec
UPDATE meeting_invite SET expires_at=$2 WHERE id=$1;
-- name: SetEventMeetingDetails :one
UPDATE calendar_event SET meeting_id=$2,location=$3,description=$4 WHERE id=$1 RETURNING *;
-- name: SaveGuestMeetingToken :exec
INSERT INTO meeting_participant(meeting_id,subject,guest_invite_id,token_expires_at) VALUES($1,$2,$3,$4);
-- name: MeetingParticipantAllowed :one
SELECT EXISTS(SELECT 1 FROM meeting_participant p JOIN meeting m ON m.id=p.meeting_id
WHERE p.meeting_id=$1 AND p.subject=$2 AND m.ended_at IS NULL AND (
(p.guest_invite_id IS NULL AND EXISTS(SELECT 1 FROM auth_member a WHERE a.subject=p.subject AND a.scope=m.workspace_id::text AND a.role IN ('owner','admin','member'))) OR
(p.guest_invite_id IS NOT NULL AND EXISTS(SELECT 1 FROM meeting_invite i JOIN calendar_event e ON e.id=i.event_id WHERE i.id=p.guest_invite_id AND i.meeting_id=p.meeting_id AND e.meeting_id=i.meeting_id AND NOT e.cancelled AND i.expires_at>now()))));
-- name: CountMeetingGuestTokens :one
SELECT COUNT(*) FROM meeting_participant WHERE meeting_id=$1 AND guest_invite_id IS NOT NULL AND (joined OR token_expires_at>now());
-- name: ExpireMeetingGuestTokens :exec
DELETE FROM meeting_participant WHERE guest_invite_id IS NOT NULL AND NOT joined AND token_expires_at<now()-interval '1 day';
-- name: FindGuestCalendarEvent :one
SELECT * FROM calendar_event WHERE id=$1;
