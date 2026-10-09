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

INSERT INTO mailbox(id,workspace_id,name,address,config_prefix)
VALUES ('a0000000-0000-4000-8000-000000000010','a0000000-0000-4000-8000-000000000001','Test mailbox','mail-e2e@example.com','')
ON CONFLICT (id) DO NOTHING;
INSERT INTO mail_item(id,mailbox_id,author_id,folder,from_address,to_address,subject,html_body,status,unread)
VALUES ('a0000000-0000-4000-8000-000000000011','a0000000-0000-4000-8000-000000000010','','inbox','sender@example.com','mail-e2e@example.com','HTML safety fixture','<p>Safe fixture body</p><script>parent.document.body.dataset.injected="yes"</script><img src="https://tracking.invalid/pixel"><a href="https://tracking.invalid/">Unsafe navigation</a>','received',true)
ON CONFLICT (id) DO UPDATE SET unread=true;

-- Public synthetic RSVP fixture; no production credentials or data.
INSERT INTO calendar(id,workspace_id,name,color) VALUES ('a0000000-0000-4000-8000-000000000020','a0000000-0000-4000-8000-000000000001','RSVP browser calendar','#15756b') ON CONFLICT(id) DO NOTHING;
INSERT INTO calendar_event(id,calendar_id,uid,organizer,title,all_day,starts_at,ends_at,time_zone) VALUES ('a0000000-0000-4000-8000-000000000021','a0000000-0000-4000-8000-000000000020','browser-rsvp@thura.test','owner-e2e@example.com','Browser invitation planning',false,now()+interval '1 day',now()+interval '1 day 1 hour','America/New_York') ON CONFLICT(id) DO UPDATE SET sequence=0,cancelled=false,starts_at=now()+interval '1 day',ends_at=now()+interval '1 day 1 hour';
INSERT INTO event_attendee(id,event_id,email) VALUES ('a0000000-0000-4000-8000-000000000022','a0000000-0000-4000-8000-000000000021','rsvp-guest@example.com') ON CONFLICT(id) DO UPDATE SET response='needsaction',response_sequence=0;
INSERT INTO calendar_reply_grant(id,attendee_id,token_hash,sequence,expires_at) VALUES ('a0000000-0000-4000-8000-000000000023','a0000000-0000-4000-8000-000000000022','5b0865f115e4fd7f4d79aa89ac5c615f5d4905bccf3179a7bd4de8813dc3c727',0,now()+interval '30 days') ON CONFLICT(id) DO UPDATE SET expires_at=now()+interval '30 days',sequence=0;

-- Synthetic peer for the dedicated real-media meeting suite.
INSERT INTO auth_user(subject,email,name,password_hash) VALUES ('thura-meeting-peer-fixture','meeting-peer-e2e@example.com','Test Meeting Peer','$argon2id$v=19$m=65536,t=3,p=2$wwjJYHkBhjBqsUkagwIJLA$96ZgNf8PIpnIC6wkfHuoh39j5n77Hs1ufezVtQrRIV4') ON CONFLICT(subject) DO NOTHING;
INSERT INTO auth_member(subject,scope,role) VALUES ('thura-meeting-peer-fixture','a0000000-0000-4000-8000-000000000001','member') ON CONFLICT(subject,scope,role) DO NOTHING;

-- Browser fixtures use explicit .env.test providers rather than real wizard
-- credentials. Mark this synthetic installation configured; bootstrap tests
-- exercise fresh state in isolated databases and mocked browser responses.
INSERT INTO server_setup(id,subject,published_revision)
VALUES ('00000000-0000-4000-8000-000000000001','thura-fixture:externally-configured',1)
ON CONFLICT(id) DO UPDATE SET subject='thura-fixture:externally-configured',published_revision=1;
