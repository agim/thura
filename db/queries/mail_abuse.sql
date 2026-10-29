-- name: GetMailProtection :one
SELECT * FROM mail_protection WHERE workspace_id=$1;
-- name: EnsureMailProtection :exec
INSERT INTO mail_protection(workspace_id,ai_enabled) VALUES($1,$2) ON CONFLICT(workspace_id) DO NOTHING;
-- name: SaveMailProtection :one
UPDATE mail_protection SET ai_enabled=$2,approve_unknown=$3,sender_per_hour=$4,domain_per_hour=$5,spam_threshold=$6,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND revision=$7 RETURNING *;
-- name: MailRate :one
INSERT INTO mail_intake_rate(workspace_id,key,bucket) VALUES($1,$2,$3) ON CONFLICT(workspace_id,key,bucket) DO UPDATE SET count=mail_intake_rate.count+1 RETURNING count;
-- name: PruneMailRates :exec
DELETE FROM mail_intake_rate WHERE bucket < $1;
-- name: GetMailSenderPolicy :one
SELECT * FROM mail_sender_policy WHERE workspace_id=$1 AND identity=$2;
-- name: SaveMailSenderPolicy :exec
INSERT INTO mail_sender_policy(workspace_id,identity,decision,updated_by,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(workspace_id,identity) DO UPDATE SET decision=EXCLUDED.decision,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at;
-- name: MailComplaintCount :one
SELECT COUNT(DISTINCT item_id) FROM mail_complaint WHERE workspace_id=$1 AND identity=$2 AND active=true;
-- name: SaveMailComplaint :exec
INSERT INTO mail_complaint(workspace_id,item_id,identity,subject,active,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(item_id,subject) DO UPDATE SET active=EXCLUDED.active,updated_at=EXCLUDED.updated_at;
-- name: GetMailScreening :one
SELECT * FROM mail_screening WHERE item_id=$1;
-- name: LockMailScreening :one
SELECT * FROM mail_screening WHERE item_id=$1 FOR UPDATE;
-- name: AddMailScreening :one
INSERT INTO mail_screening(item_id,workspace_id,sender,domain,identity,authenticated,state,score,findings,ai_state,av_state,approval_required,policy_revision,next_attempt,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14) RETURNING *;
-- name: SaveMailScreening :exec
UPDATE mail_screening SET state=$2,score=$3,findings=$4,ai_state=$5,av_state=$6,approval_required=$7,released=$8,attempts=$9,attempt_id=$10,policy_revision=$11,next_attempt=$12,updated_at=$13,sender_approved=$14 WHERE item_id=$1;
-- name: MailScreeningDue :many
SELECT item_id FROM mail_screening WHERE state IN ('pending','scanning','error') AND attempts<5 AND next_attempt<=$1 ORDER BY next_attempt,item_id LIMIT 50;
-- name: MoveScreenedMail :exec
UPDATE mail_item SET folder=$2,updated_at=now() WHERE id=$1;
-- name: MailKnownContacts :many
SELECT name,email FROM contact WHERE workspace_id=$1 ORDER BY id LIMIT 500;
-- name: MailKnownAccounts :many
SELECT coalesce(u.name,'')::text AS name,coalesce(u.email,'')::text AS email FROM auth_member m JOIN auth_user u ON u.subject=m.subject WHERE m.scope=$1 AND m.role IN ('owner','admin','member') LIMIT 1000;
-- name: MailItemComplaintCount :one
SELECT COUNT(*) FROM mail_complaint WHERE item_id=$1 AND active=true;
-- name: QuarantinedMailObject :one
SELECT EXISTS(SELECT 1 FROM mail_attachment a JOIN mail_screening s ON s.item_id=a.item_id WHERE a.object_key=$1 AND ((s.state<>'clean' AND NOT s.released) OR s.av_state='infected'))::boolean;
-- name: MailDomainComplaintCount :one
SELECT COUNT(DISTINCT c.item_id) FROM mail_complaint c JOIN mail_screening s ON s.item_id=c.item_id WHERE c.workspace_id=$1 AND s.domain=$2 AND s.authenticated=$3 AND c.active=true;

-- name: MailActorComplaint :one
SELECT EXISTS(SELECT 1 FROM mail_complaint WHERE item_id=$1 AND subject=$2 AND active=true)::boolean;

-- name: SaveMailFilterFeedback :one
INSERT INTO mail_filter_feedback(workspace_id,item_id,classification,actor,next_attempt,updated_at)
VALUES($1,$2,$3,$4,$5,$5)
ON CONFLICT(item_id) DO UPDATE SET classification=EXCLUDED.classification,actor=EXCLUDED.actor,
revision=CASE WHEN mail_filter_feedback.classification<>EXCLUDED.classification THEN mail_filter_feedback.revision+1 ELSE mail_filter_feedback.revision END,
state=CASE WHEN mail_filter_feedback.classification=EXCLUDED.classification AND mail_filter_feedback.state IN ('learned','recorded') THEN mail_filter_feedback.state ELSE 'pending' END,
attempts=0,next_attempt=EXCLUDED.next_attempt,updated_at=EXCLUDED.updated_at
WHERE mail_filter_feedback.workspace_id=EXCLUDED.workspace_id RETURNING *;

-- name: LockMailFilterFeedback :one
SELECT * FROM mail_filter_feedback WHERE item_id=$1 FOR UPDATE;

-- name: GetMailFilterFeedback :one
SELECT * FROM mail_filter_feedback WHERE item_id=$1;

-- name: MailFilterFeedbackDue :many
SELECT item_id FROM mail_filter_feedback WHERE state IN ('pending','error') AND next_attempt<=$1 ORDER BY next_attempt,item_id LIMIT 50;

-- name: FinishMailFilterFeedback :exec
UPDATE mail_filter_feedback SET state=$2,learned_revision=$3,attempts=$4,next_attempt=$5,updated_at=$6
WHERE item_id=$1 AND revision=$7;
-- name: SetMailScreeningEnvelope :exec
UPDATE mail_screening SET client_address=$2,client_helo=$3,envelope_sender=$4 WHERE item_id=$1;
-- name: SaveMailScreeningAuth :exec
UPDATE mail_screening SET spf=$2,dkim=$3,dkim_domain=$4,dkim_aligned=$5,dmarc=$6 WHERE item_id=$1;
-- name: SaveMailPerson :exec
UPDATE mail_screening SET person_state=$2,person_source=$3,person_detail=$4 WHERE item_id=$1;
-- name: MailSentToCandidates :many
SELECT i.to_address,i.cc,i.bcc FROM mail_item i JOIN mailbox b ON b.id=i.mailbox_id
WHERE b.workspace_id=sqlc.arg(workspace_id) AND i.status NOT IN ('draft','received')
AND position(lower(sqlc.arg(address)::text) IN lower(i.to_address||','||i.cc||','||i.bcc))>0 LIMIT 20;
-- name: MailContactAddress :one
SELECT EXISTS(SELECT 1 FROM contact WHERE workspace_id=$1 AND lower(email)=lower(sqlc.arg(address)::text))::bool;
