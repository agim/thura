-- The domain registry shared by SMIP/0.2 mail and SMIP/0.1 chat/files,
-- scoped by local identity (origin) and profile.
-- name: GetSmipDomain :one
SELECT * FROM smip_mail_domain WHERE origin=$1 AND profile=$2 AND domain=$3;
-- name: CountSmipDomains :one
SELECT COUNT(*) AS total,COUNT(*) FILTER (WHERE ever_verified OR expires_at>$3) AS live FROM smip_mail_domain WHERE origin=$1 AND profile=$2;
-- name: EvictSmipDomains :execrows
DELETE FROM smip_mail_domain WHERE id IN (SELECT d.id FROM smip_mail_domain d WHERE d.origin=$1 AND d.profile=$2 AND NOT d.ever_verified AND d.expires_at<=$3 ORDER BY d.expires_at,d.id LIMIT $4);
-- name: WriteSmipDomain :exec
INSERT INTO smip_mail_domain(origin,profile,domain,state,ever_verified,endpoint,document,reason,checked_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT(origin,profile,domain) DO UPDATE SET state=EXCLUDED.state,ever_verified=smip_mail_domain.ever_verified OR EXCLUDED.ever_verified,
endpoint=CASE WHEN EXCLUDED.ever_verified THEN EXCLUDED.endpoint ELSE smip_mail_domain.endpoint END,
document=CASE WHEN EXCLUDED.ever_verified THEN EXCLUDED.document ELSE smip_mail_domain.document END,
reason=EXCLUDED.reason,checked_at=EXCLUDED.checked_at,expires_at=EXCLUDED.expires_at;
