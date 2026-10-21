-- Claim only exact account addresses with current membership. Never guess an
-- owner from a display name or transfer an existing owner's retained inbox.
UPDATE mailbox b SET private=true,owner_subject=u.subject
FROM auth_user u
WHERE b.owner_subject IS NULL AND NOT b.private
AND lower(b.address)=lower(u.email)
AND EXISTS(SELECT 1 FROM auth_member m WHERE m.scope=b.workspace_id::text
AND m.subject=u.subject AND m.role IN ('owner','admin','member'));
