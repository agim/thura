-- Test-only accounts. lidza test --e2e loads this into .env.test's database.
INSERT INTO auth_user(subject,email,name,password_hash)
VALUES ('thura-e2e-owner','owner-e2e@example.com','Test Owner','$argon2id$v=19$m=65536,t=3,p=2$wwjJYHkBhjBqsUkagwIJLA$96ZgNf8PIpnIC6wkfHuoh39j5n77Hs1ufezVtQrRIV4')
ON CONFLICT (subject) DO NOTHING;
INSERT INTO workspace(id,name)
VALUES ('a0000000-0000-4000-8000-000000000001','Browser test workspace')
ON CONFLICT (id) DO NOTHING;
INSERT INTO auth_member(subject,scope,role)
VALUES ('thura-e2e-owner','a0000000-0000-4000-8000-000000000001','owner')
ON CONFLICT (subject,scope,role) DO NOTHING;

-- Reset only the test-only invited visitor so acceptance is repeatable.
DELETE FROM auth_member WHERE subject IN (SELECT subject FROM auth_user WHERE email='invite-browser@example.com');
DELETE FROM auth_session WHERE subject IN (SELECT subject FROM auth_user WHERE email='invite-browser@example.com');
DELETE FROM auth_account WHERE subject IN (SELECT subject FROM auth_user WHERE email='invite-browser@example.com');
DELETE FROM auth_user WHERE email='invite-browser@example.com';
INSERT INTO invitation(id,workspace_id,email,role,token_hash,invited_by,expires_at)
VALUES ('a0000000-0000-4000-8000-000000000002','a0000000-0000-4000-8000-000000000001','invite-browser@example.com','member','c56ed6c340c8f681c69adf04b19ed1193080f39f78ccfd85dd414247ab12fa9d','thura-e2e-owner',now()+interval '72 hours')
ON CONFLICT (id) DO UPDATE SET accepted_at=NULL,revoked_at=NULL,expires_at=now()+interval '72 hours';
