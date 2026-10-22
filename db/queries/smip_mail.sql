-- name: GetSmipMailDomain :one
SELECT * FROM smip_mail_domain WHERE origin=$1 AND domain=$2;
-- name: ListSmipMailDomains :many
SELECT * FROM smip_mail_domain WHERE origin=$1 ORDER BY domain LIMIT 1000;
-- name: CountSmipMailDomains :one
SELECT COUNT(*) AS total,COUNT(*) FILTER (WHERE ever_verified OR expires_at>$2) AS live FROM smip_mail_domain WHERE origin=$1;
-- name: EvictSmipMailDomains :execrows
DELETE FROM smip_mail_domain WHERE id IN (SELECT d.id FROM smip_mail_domain d WHERE d.origin=$1 AND NOT d.ever_verified AND d.expires_at<=$2 ORDER BY d.expires_at,d.id LIMIT $3);
-- name: WriteSmipMailDomain :exec
INSERT INTO smip_mail_domain(origin,domain,state,ever_verified,endpoint,document,reason,checked_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(origin,domain) DO UPDATE SET state=EXCLUDED.state,ever_verified=smip_mail_domain.ever_verified OR EXCLUDED.ever_verified,
endpoint=CASE WHEN EXCLUDED.ever_verified THEN EXCLUDED.endpoint ELSE smip_mail_domain.endpoint END,
document=CASE WHEN EXCLUDED.ever_verified THEN EXCLUDED.document ELSE smip_mail_domain.document END,
reason=EXCLUDED.reason,checked_at=EXCLUDED.checked_at,expires_at=EXCLUDED.expires_at;
-- name: GetMailTransmission :one
SELECT * FROM mail_transmission WHERE item_id=$1;
-- name: FindMailTransmission :one
SELECT * FROM mail_transmission WHERE id=$1;
-- name: LockMailTransmission :one
SELECT * FROM mail_transmission WHERE id=$1 FOR UPDATE;
-- name: AddMailTransmission :one
INSERT INTO mail_transmission(origin,workspace_id,mailbox_id,item_id,author_id,payload,provider_config,origin_key,created_at,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING *;
-- name: SaveMailTransmission :exec
UPDATE mail_transmission SET state=$2,api_state=$3,api_attempt_id=$4,api_result=$5,updated_at=$6 WHERE id=$1;
-- name: AddMailDelivery :exec
INSERT INTO mail_delivery(transmission_id,recipient,domain,next_attempt,updated_at) VALUES($1,$2,$3,$4,$4);
-- name: ListMailDeliveries :many
SELECT * FROM mail_delivery WHERE transmission_id=$1 ORDER BY id;
-- name: SaveMailDelivery :exec
UPDATE mail_delivery SET route=$2,state=$3,reason=$4,packet=$5,peer_document=$6,receipt=$7,attempts=$8,attempt_id=$9,next_attempt=$10,updated_at=$11 WHERE id=$1;
-- name: DueMailTransmissions :many
SELECT id FROM mail_transmission WHERE origin=$1 AND state IN ('planning','pending') ORDER BY updated_at,id LIMIT 50;
-- name: MailTransmissionUsage :one
SELECT COUNT(*) AS records,COALESCE(SUM(octet_length(payload)),0)::bigint AS bytes FROM mail_transmission WHERE workspace_id=$1;
-- name: GetSmipMailReceipt :one
SELECT * FROM smip_mail_receipt WHERE domain=$1 AND origin=$2 AND message_id=$3;
-- name: AddSmipMailReceipt :exec
INSERT INTO smip_mail_receipt(domain,origin,message_id,recipient,digest,receipt,item_id,accepted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: FindSmipReceivingMailbox :many
SELECT b.* FROM mailbox b WHERE lower(b.address)=lower($1)
OR EXISTS(SELECT 1 FROM mailbox_alias a WHERE a.mailbox_id=b.id AND lower(a.address)=lower($1)) ORDER BY b.id LIMIT 2;

-- name: SetMailMessageIdentity :exec
UPDATE mail_item SET message_id=$2,thread_id=CASE WHEN thread_id='' THEN $2 ELSE thread_id END WHERE id=$1;
-- name: LockSmipMailDomain :one
SELECT * FROM smip_mail_domain WHERE origin=$1 AND domain=$2 FOR UPDATE;
