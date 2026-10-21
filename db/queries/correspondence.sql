-- name: GetCorrespondenceContact :one
SELECT * FROM contact WHERE workspace_id=$1 AND id=$2;

-- name: CorrespondencePeople :many
SELECT p.* FROM contact p WHERE p.workspace_id=sqlc.arg(workspace_id)
AND (id=sqlc.arg(contact_id) OR (sqlc.arg(organization)::boolean AND company<>'' AND lower(trim(p.company))=lower(trim(sqlc.arg(company)::text))));

-- name: CorrespondenceCandidates :many
WITH people AS (
 SELECT p.* FROM contact p WHERE p.workspace_id=sqlc.arg(workspace_id)
 AND (p.id=sqlc.arg(contact_id) OR (sqlc.arg(organization)::boolean AND p.company<>'' AND lower(trim(p.company))=lower(trim(sqlc.arg(company)::text))))
), links AS (
 SELECT l.* FROM correspondence_link l JOIN people p ON p.id=l.contact_id
), sources AS (
 SELECT m.id, 'mail'::text AS source, m.mailbox_id::text AS container_id, m.subject,
 left(m.text_body,1000)::text AS preview, m.from_address,m.to_address,m.cc,m.bcc,m.status::text AS status,
 m.created_at AS at, 'Email participant'::text AS reason
 FROM mail_item m JOIN mailbox b ON b.id=m.mailbox_id
 WHERE b.workspace_id=sqlc.arg(workspace_id) AND (NOT b.private OR b.owner_subject=sqlc.arg(viewer)::text) AND NOT sqlc.arg(browse)::boolean
 AND m.status IN ('received','sent','captured') AND m.folder NOT IN ('trash','spam')
 AND EXISTS (SELECT 1 FROM people p WHERE strpos(lower(m.from_address || ' ' || m.to_address || ' ' || m.cc || ' ' || m.bcc),lower(p.email))>0)
 AND (sqlc.arg(search)::text='' OR strpos(lower(m.subject || ' ' || m.text_body || ' ' || m.from_address || ' ' || m.to_address),lower(sqlc.arg(search)))>0)
 UNION ALL
 SELECT f.id,'document', ''::text,f.name,f.content_type,''::text,''::text,''::text,''::text,'Drive file',f.created_at,
 CASE WHEN EXISTS(SELECT 1 FROM links l WHERE l.source='document' AND l.source_id=f.id) THEN 'Linked to contact' ELSE 'Shared with contact' END
 FROM drive_file f WHERE f.workspace_id=sqlc.arg(workspace_id) AND NOT f.trashed
 AND (sqlc.arg(browse)::boolean OR EXISTS(SELECT 1 FROM links l WHERE l.source='document' AND l.source_id=f.id)
 OR EXISTS(SELECT 1 FROM share_grant g JOIN people p ON lower(trim(g.target_email))=lower(p.email) WHERE g.file_id=f.id AND g.revoked_at IS NULL AND g.expires_at>now()))
 UNION ALL
 SELECT r.id,'chat',''::text,r.name,CASE WHEN r.direct THEN 'Private conversation' ELSE 'Shared channel' END,''::text,''::text,''::text,''::text,'Conversation',r.created_at,
 CASE WHEN EXISTS(SELECT 1 FROM links l WHERE l.source='chat' AND l.source_id=r.id) THEN 'Linked to contact' ELSE 'Conversation participant' END
 FROM chat_room r WHERE r.workspace_id=sqlc.arg(workspace_id)
 AND (NOT r.direct OR EXISTS(SELECT 1 FROM chat_participant cp WHERE cp.room_id=r.id AND cp.subject=sqlc.arg(viewer)))
 AND (sqlc.arg(browse)::boolean OR EXISTS(SELECT 1 FROM links l WHERE l.source='chat' AND l.source_id=r.id)
 OR EXISTS(SELECT 1 FROM chat_participant cp JOIN auth_user u ON u.subject=cp.subject JOIN people p ON lower(u.email)=lower(p.email) WHERE cp.room_id=r.id))
 UNION ALL
 SELECT m.id,'meeting',''::text,m.name,'Meeting record'::text,''::text,''::text,''::text,''::text,
 CASE WHEN m.ended_at IS NULL THEN 'Open meeting' ELSE 'Ended meeting' END,m.created_at,
 CASE WHEN EXISTS(SELECT 1 FROM links l WHERE l.source='meeting' AND l.source_id=m.id) THEN 'Linked to contact' ELSE 'Meeting participant record' END
 FROM meeting m WHERE m.workspace_id=sqlc.arg(workspace_id)
 AND (sqlc.arg(browse)::boolean OR EXISTS(SELECT 1 FROM links l WHERE l.source='meeting' AND l.source_id=m.id)
 OR EXISTS(SELECT 1 FROM meeting_participant mp JOIN auth_user u ON u.subject=mp.subject JOIN people p ON lower(u.email)=lower(p.email) WHERE mp.meeting_id=m.id))
 UNION ALL
 SELECT e.id,'calendar',e.calendar_id::text,e.title,
 e.description || CASE WHEN e.all_day THEN ' · All day: ' || e.start_date ELSE '' END,
 ''::text,''::text,''::text,''::text,CASE WHEN e.cancelled THEN 'Cancelled event' ELSE 'Calendar event' END,
 coalesce(e.starts_at,e.created_at),
 CASE WHEN EXISTS(SELECT 1 FROM links l WHERE l.source='calendar' AND l.source_id=e.id) THEN 'Linked to contact' ELSE 'Event organizer or invitee' END
 FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id
 WHERE c.workspace_id=sqlc.arg(workspace_id) AND (c.owner_subject IS NULL OR c.owner_subject=sqlc.arg(viewer))
 AND (sqlc.arg(browse)::boolean OR EXISTS(SELECT 1 FROM links l WHERE l.source='calendar' AND l.source_id=e.id)
 OR EXISTS(SELECT 1 FROM people p WHERE lower(e.organizer)=lower(p.email) OR EXISTS(SELECT 1 FROM event_attendee a WHERE a.event_id=e.id AND lower(a.email)=lower(p.email))))
)
SELECT s.*, EXISTS(SELECT 1 FROM correspondence_link l WHERE l.contact_id=sqlc.arg(contact_id) AND l.source=s.source AND l.source_id=s.id)::boolean AS linked
FROM sources s
WHERE (sqlc.arg(target_id)::text='' OR s.id::text=sqlc.arg(target_id))
AND (sqlc.arg(source_filter)::text='' OR s.source=sqlc.arg(source_filter))
AND (sqlc.arg(search)::text='' OR s.source='mail' OR (NOT sqlc.arg(browse)::boolean AND s.source IN ('chat','document')) OR strpos(lower(s.subject || ' ' || s.preview),lower(sqlc.arg(search)))>0)
AND (sqlc.narg(before_time)::timestamptz IS NULL OR (s.at,s.source,s.id)<(sqlc.narg(before_time)::timestamptz,sqlc.arg(before_source)::text,sqlc.arg(before_id)::uuid))
ORDER BY s.at DESC,s.source DESC,s.id DESC LIMIT sqlc.arg(page_limit);

-- name: AddCorrespondenceLink :exec
INSERT INTO correspondence_link(contact_id,source,source_id,created_by) VALUES($1,$2,$3,$4)
ON CONFLICT(contact_id,source,source_id) DO NOTHING;

-- name: RemoveCorrespondenceLink :exec
DELETE FROM correspondence_link WHERE contact_id=$1 AND source=$2 AND source_id=$3;
