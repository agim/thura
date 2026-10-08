-- name: ListCalendars :many
SELECT * FROM calendar WHERE workspace_id=$1 AND (owner_subject IS NULL OR owner_subject=$2) ORDER BY name LIMIT 100;
-- name: GetCalendar :one
SELECT * FROM calendar WHERE workspace_id=$1 AND id=$2 AND (owner_subject IS NULL OR owner_subject=$3);
-- name: CreateCalendar :one
INSERT INTO calendar(workspace_id,name,color,owner_subject) VALUES($1,$2,$3,$4) RETURNING *;
-- name: ListCalendarEvents :many
SELECT * FROM calendar_event WHERE calendar_id=$1 ORDER BY created_at DESC LIMIT 1000;
-- name: GetCalendarEvent :one
SELECT * FROM calendar_event WHERE calendar_id=$1 AND id=$2;
-- name: LockCalendarEvent :one
SELECT * FROM calendar_event WHERE calendar_id=$1 AND id=$2 FOR UPDATE;
-- name: FindCalendarEventUID :one
SELECT * FROM calendar_event WHERE calendar_id=$1 AND uid=$2;
-- name: CreateCalendarEvent :one
INSERT INTO calendar_event(calendar_id,uid,organizer,title,description,location,all_day,start_date,end_date,starts_at,ends_at,time_zone,rrule)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;
-- name: UpdateCalendarEvent :one
UPDATE calendar_event SET title=$3,description=$4,location=$5,all_day=$6,start_date=$7,end_date=$8,starts_at=$9,ends_at=$10,time_zone=$11,rrule=$12,sequence=sequence+1,updated_at=now()
WHERE calendar_id=$1 AND id=$2 RETURNING *;
-- name: BumpEventSequence :one
UPDATE calendar_event SET sequence=sequence+1,updated_at=now() WHERE id=$1 RETURNING *;
-- name: CancelCalendarEvent :one
UPDATE calendar_event SET cancelled=true,sequence=sequence+1,updated_at=now() WHERE id=$1 RETURNING *;
-- name: AddEventAttendee :exec
INSERT INTO event_attendee(event_id,email) VALUES($1,$2) ON CONFLICT(event_id,email) DO NOTHING;
-- name: DeleteEventAttendee :exec
DELETE FROM event_attendee WHERE event_id=$1 AND email=$2;
-- name: ListEventAttendees :many
SELECT * FROM event_attendee WHERE event_id=$1 ORDER BY email;
-- name: ReplyEventAttendee :one
UPDATE event_attendee SET response=$3,response_sequence=$4 WHERE event_id=$1 AND email=$2 RETURNING *;
-- name: ListEventExceptions :many
SELECT * FROM event_exception WHERE event_id=$1 ORDER BY instance_key;
-- name: SaveEventException :one
INSERT INTO event_exception(event_id,instance_key,cancelled,title,starts_at,ends_at,start_date,end_date) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(event_id,instance_key) DO UPDATE SET cancelled=excluded.cancelled,title=excluded.title,starts_at=excluded.starts_at,ends_at=excluded.ends_at,start_date=excluded.start_date,end_date=excluded.end_date RETURNING *;
-- name: ClearEventExceptions :exec
DELETE FROM event_exception WHERE event_id=$1;
-- name: CalendarAccountEmail :one
SELECT COALESCE(email,'')::text FROM auth_user WHERE subject=$1;
-- name: CountWorkspaceCalendars :one
SELECT COUNT(*) FROM calendar WHERE workspace_id=$1;
-- name: CountWorkspaceEvents :one
SELECT COUNT(*) FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id WHERE c.workspace_id=$1;
-- name: ImportCalendarEvent :one
UPDATE calendar_event SET organizer=$3,title=$4,description=$5,location=$6,all_day=$7,start_date=$8,end_date=$9,starts_at=$10,ends_at=$11,time_zone=$12,rrule=$13,sequence=$14,cancelled=$15,updated_at=now() WHERE calendar_id=$1 AND id=$2 RETURNING *;
-- name: SetImportedEventSequence :one
UPDATE calendar_event SET sequence=$2,cancelled=$3 WHERE id=$1 RETURNING *;
-- name: ListCalendarAttendees :many
SELECT a.* FROM event_attendee a JOIN calendar_event e ON e.id=a.event_id WHERE e.calendar_id=$1 ORDER BY a.email;
-- name: ListCalendarExceptions :many
SELECT x.* FROM event_exception x JOIN calendar_event e ON e.id=x.event_id WHERE e.calendar_id=$1 ORDER BY x.instance_key;

-- name: BindCalendarMeeting :one
UPDATE calendar_event SET meeting_id=$2, sequence=sequence+1, updated_at=now() WHERE id=$1 RETURNING *;
