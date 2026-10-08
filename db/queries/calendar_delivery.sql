-- name: GetCalendarDispatch :one
SELECT * FROM calendar_dispatch WHERE event_id=$1 AND sequence=$2 AND instance_key=$3;
-- name: DeleteCalendarDispatch :exec
DELETE FROM calendar_dispatch WHERE id=$1;
-- name: CreateCalendarDispatch :exec
INSERT INTO calendar_dispatch(event_id,sequence,instance_key) VALUES($1,$2,$3);
-- name: AddCalendarReplyGrant :exec
INSERT INTO calendar_reply_grant(attendee_id,token_hash,sequence,instance_key,expires_at) VALUES($1,$2,$3,$4,$5);
-- name: DeleteAttendeeGrants :exec
DELETE FROM calendar_reply_grant WHERE attendee_id=$1;
-- name: GetCalendarReplyGrant :one
SELECT g.*,a.event_id,a.email FROM calendar_reply_grant g JOIN event_attendee a ON a.id=g.attendee_id WHERE g.token_hash=$1;
-- name: GetReplyEvent :one
SELECT e.* FROM calendar_event e JOIN event_attendee a ON a.event_id=e.id WHERE a.id=$1;
-- name: GetCalendarReminder :one
SELECT * FROM calendar_reminder WHERE event_id=$1 AND subject=$2;
-- name: SetCalendarReminder :exec
INSERT INTO calendar_reminder(event_id,subject,minutes_before) VALUES($1,$2,$3) ON CONFLICT(event_id,subject) DO UPDATE SET minutes_before=excluded.minutes_before;
-- name: DeleteCalendarReminder :exec
DELETE FROM calendar_reminder WHERE event_id=$1 AND subject=$2;
-- name: CountSubjectReminders :one
SELECT COUNT(*) FROM calendar_reminder WHERE subject=$1;
-- name: ListReminderSubscriptions :many
SELECT r.*,e.calendar_id,c.workspace_id,c.owner_subject FROM calendar_reminder r JOIN calendar_event e ON e.id=r.event_id JOIN calendar c ON c.id=e.calendar_id WHERE r.id::text > $1 ORDER BY r.id LIMIT 500;
-- name: AddReminderNotice :execrows
INSERT INTO reminder_notice(event_id,subject,sequence,instance_key) VALUES($1,$2,$3,$4) ON CONFLICT(event_id,subject,sequence,instance_key) DO NOTHING;
-- name: LockReminderSubject :one
SELECT subject FROM auth_user WHERE subject=$1 FOR UPDATE;
