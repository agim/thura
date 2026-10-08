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
