# Debian Postfix transport

Start with [the server installation guide](install-server.md) for DNS/MX, Mailgun sending, administrator setup and user invitations. This document covers the receiving/SMTP transport.

Thura defaults to the maintained Postfix package available in Debian-family
distributions. Deployment supplies every hostname, domain and mailbox; the app
has no fixed public origin. Postfix is a separate SMTP service with a durable
queue. Thura remains a Līdza Go application using the official mail pack.

The supplied container uses a pinned official Rspamd image on Debian 13 and installs the
distribution's current Postfix/Python/CA packages. Rebuild for security updates;
record the resulting image digest and package versions for a release. The
integration fixture tested Postfix 3.10.13. The same adapter can run beside
Debian's native Postfix package on a dedicated mail host.

## Included Compose deployment and wizard

From the repository root, run:

```sh
python3 scripts/init-compose.py
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

The initializer creates private `.env` bootstrap values without overwriting an
existing file. If `.env` already exists, privately add `POSTGRES_PASSWORD`,
`AUTH_SECRET`, `LIDZA_MASTER_KEY` (64 hex characters), `THURA_SETUP_TOKEN` and
`THURA_POSTFIX_TOKEN` (each at least 32 random characters). Use a URL-safe
Postgres password. Keep the setup token and master key backed up separately.
Never commit this file. The Compose stack includes Thura, Postgres, Valkey,
a private storage volume and a separate Postfix container with a persistent
queue. The default [compact deployment](compact-deployment.md) includes document and
media services in four containers, with antivirus and shared Valkey in the
Postfix container. Configure the public origins and Collabora image as described
there before starting the default stack.

Put your HTTPS reverse proxy in front of `127.0.0.1:8080` and open `/setup`.
Use the setup token from your private `.env` to create the administrator. On
**Email service**, choose these independently:

- **Receive email through:** `postfix` for the included service, or `relay` for
  an independently managed signed raw-MIME relay.
- **Send email through:** `postfix` for direct SMTP delivery, or `provider` to
  select SMTP, Mailgun, SendGrid, Postmark or Resend and enter its credentials.

New and reset wizard drafts recommend Mailgun, with Mailgun/SendGrid/Postmark/Resend listed before SMTP. Existing published choices are preserved.

For example, `Receive: postfix`, `Send: provider`, `Outgoing provider: mailgun`
receives directly on your server and sends through Mailgun. Other outgoing
providers work the same way. Selecting Mailgun does not change receiving.
Set the mail server hostname, initial inbox address and outgoing sender; the
inbox may differ from the sender. A blank inbox uses the sender on first
publication. Changing this field later does not rename an existing mailbox.
Mailbox messages send as their mailbox address, so authorize the inbox address/domain with your outgoing provider too. The sender field supplies system notices and the wizard test. Postfix receiving generates a private signing secret when none has been saved.

The containers share a **network namespace**, keeping SMTP submission on
`127.0.0.1:2525` and the authenticated configuration/ingestion requests on
loopback. They remain separate containers with separate filesystems; no Docker
socket or database credentials go to Postfix. The sidecar never receives
Mailgun or other outgoing API keys. Only public receiving port 25 is published;
`THURA_SMTP_BIND` and `THURA_SMTP_PORT` can override the host binding/port for a
private test. Public internet MX delivery needs the real port 25.

During first setup the service starts in outgoing-only bootstrap mode. This
allows the wizard's explicitly requested test email before a mailbox UUID
exists; port 25 does not listen and no recipients are mapped yet. Save the mail
step, complete the mail/storage checks, publish and restart Thura:

```sh
docker compose --env-file .env -f deploy/compose.yaml restart thura
```

The sidecar polls the activated configuration every two seconds, installs exact
mailbox mappings and opens receiving after activation. Draft edits on an active
installation never change its mail configuration. Newly provisioned mailboxes
are discovered automatically; mailbox-specific credential prefixes must have a
valid inbound secret. Duplicate addresses, unsupported address syntax and more
than 1,000 mailboxes are refused instead of publishing ambiguous routes.
Configuration read failures retain the last configuration and queue. Removed
mapping deliveries already in the queue are deferred for inspection, rather
than silently discarded. Disabling managed Postfix stops the service and
preserves its queue volume. Do not remove that volume during upgrades.

Set your inbox domain's MX to the mail server hostname and its A record to this
server; allow inbound TCP 25. Set `SMTP_TLS_DIR` in `.env` to a private directory
containing `smtp-cert.pem` and `smtp-key.pem` for that hostname (default
`deploy/private/smtp`). The managed service detects certificate changes and
reloads Postfix without restarting or losing its queue. Without a usable pair,
STARTTLS is disabled instead of advertising a command that returns 454.
Lidza-deploy v0.2.105 or newer provisions the primary hostname certificate through
the mail service's `public_tls` declaration. Thura loads its immutable bundle,
validates the public hostname/key/expiry, and notices renewal automatically.
The `DEPLOY_PUBLIC_TLS_*` paths are private to that service; neither Caddy's
certificate store nor the Docker socket is mounted into the container.
With a hosted outgoing provider, retain its sending-domain records and your
Postfix MX. DNS, PTR and external reachability still require operator setup.
A wizard test confirms outgoing acceptance, not incoming internet reachability;
send an external test to the inbox after activation and check the received item.

For production upgrades that recreate Thura, run `up -d` for the **whole stack**
so the sidecar follows the recreated network namespace. Keep the app listener
on container port 3000. Scale-out mail gateways need a separate network/TLS
plan; do not scale this shared-namespace Compose recipe blindly.

## Configure a mailbox

1. Provision a mailbox through `app_provision_mailbox`, recording its UUID,
   canonical address and optional credential prefix. See [mail connectors](mail-connectors.md).
2. Privately copy [relay.example.json](../deploy/postfix/relay.example.json) to
   an operator-owned file outside the checkout. Replace its address, UUID and
   secret. Generate a random secret of at least 32 characters and seal the same
   value as the prefix's `MAIL_INBOUND_SECRET` in Thura. Never commit this file.
3. Set `baseUrl` to the origin the mail host uses to reach Thura. Loopback HTTP
   is allowed on the same host; other hosts require HTTPS. Redirects are refused.
   This internal relay origin is independent of the public `APP_URL` used for
   invitation and recovery links.
4. Add an exact mapping for every accepted address. Multiple domains and
   workspaces can coexist in one map. Domains are derived from these mappings;
   unknown recipients are refused during SMTP. There is no catch-all or automatic
   plus-address expansion. Restart after changing mappings or rotating secrets.
5. Set Thura's official `MAIL_PROVIDER=smtp`, `MAIL_SMTP_HOST=127.0.0.1`,
   `MAIL_SMTP_PORT=2525`, `MAIL_SMTP_SECURITY=none` for the same-host private
   listener. Prefix these names for a per-mailbox connector. Set the global
   `MAIL_FROM` for invitations/recovery separately. Plain SMTP is appropriate
   here only because submission stays on loopback.

The non-secret [production mail profile](../deploy/postfix/thura-mail.env.example)
provides these defaults; replace its system address and merge it into the app's
operator settings. Development remains captured/outbox mail until configured.

Public `APP_URL`, Thura TLS/proxy settings and all external service hostnames
are configured by each operator. A multi-host deployment must configure
authenticated TLS submission on its chosen MTA and use Līdza's SMTP credentials
and `starttls`/`tls` settings. The supplied local listener does not implement a
public authenticated submission service; never broaden `mynetworks` to expose
unauthenticated relay access.

## Container deployment on Linux

Build `deploy/postfix` as `thura-postfix:release`. Protect the private relay
configuration (mode 0600), mount it read-only, and persist the entire queue:

```sh
docker build -t thura-postfix:release deploy/postfix
docker volume create thura-postfix-queue
docker run -d --name thura-postfix --restart unless-stopped --network host \
  -e MAIL_HOSTNAME=mail.example.org \
  -v /srv/private/thura-relay.json:/run/secrets/thura-relay.json:ro \
  -v thura-postfix-queue:/var/spool/postfix \
  thura-postfix:release
```

`example.org` is illustrative: use the operator's actual hostname and domain
in the configuration and DNS. The default binds only `127.0.0.1:2525`. To accept
internet SMTP, set `SMTP_LISTEN=25`, mount the certificate and private key at
`/run/secrets/smtp-cert.pem` and `/run/secrets/smtp-key.pem`. Keep Thura's
private submission port 2525. The added MX listener uses the host's IPv4 interfaces; configure the
firewall deliberately. Only 127.0.0.1 may submit to arbitrary outbound domains.
Other clients may send only to mapped local recipients. The private inbound
adapter runs as the dedicated `thura-mail` account.

Configure MX/A records, reverse DNS, SPF and an appropriate DKIM signer/DMARC
policy for the chosen domains. Review abuse limits, filtering and public TLS.
Postfix's queue and Thura's signed relay do not establish sender authenticity
or internet deliverability by themselves. Outbound uses DNS/MX unless the
operator selects `POSTFIX_RELAYHOST=[host]:port`. That optional upstream must be
a trusted private relay; authenticated upstream relays require native Postfix
SASL/TLS configuration. Blocked outbound port 25 needs an approved submission
relay, rather than a claim of successful internet delivery.

## Native Debian-family host

On a **new dedicated MTA instance**, install `postfix python3 ca-certificates`
through apt, create a system account/group named `thura-mail`, and install
`configure.py` and `relay.py` as mode-0644 files in a mode-0755 `/opt/thura-postfix/`
directory so the pipe account can read them. Copy the supplied systemd
unit to `/etc/systemd/system/thura-postfix.service`. Create a root-owned mode-0600
`/etc/thura-postfix/environment` containing operator values for `MAIL_HOSTNAME`,
`SMTP_LISTEN` and `THURA_RELAY_CONFIG` (the private JSON path). Stop the package's
default service, reload systemd, and enable/start `thura-postfix.service`.

The configurator sets Thura's routing/restrictions and preserves Debian's
standard Postfix services. It owns this instance's main/listener configuration;
do not run it against an existing unrelated production MTA. For an existing MTA,
adapt the exact-recipient transport map and pipe command manually, retaining
the operator's submission, filtering and TLS policy. Debian-derived package
versions need the same fixture acceptance check; only the recorded Debian
version has been exercised here.

## Queue semantics and recovery

SMTP acceptance means Postfix queued the message. The pipe exits successfully
only after Thura returns a successful ingestion response. Timeout, HTTP failure,
invalid credentials or an unreachable app produce EX_TEMPFAIL (75), retaining
mail for Postfix's retry policy. Retry IDs bind the canonical envelope recipient
and raw bytes; timestamps/signatures are refreshed. Exact repeated bytes for
one recipient intentionally collapse into one Thura item. New SMTP submissions
usually add different Received headers and are separate deliveries; matching
Message-IDs alone are not a deduplication guarantee. Public inbound SMTP and
raw ingestion are bounded to 12 MiB; Thura applies its additional MIME/body/
attachment bounds. Private submission allows 16 MiB of MIME wire data so a
valid 10 MiB uploaded attachment survives base64 overhead. This is an outbound
submission allowance, not an increased inbound API limit. Remote recipients
can impose smaller limits.

Permanent MIME/policy rejection also remains deferred for operator inspection.
Postfix eventually expires undeliverable messages under its queue lifetime and
bounce policy; this is not indefinite retention. Monitor deferred queue age and
failures (`postqueue -p`/`postqueue -j`), inspect logs privately, and use
`postqueue -f` after correcting the cause. Logs contain addresses and queue IDs.
Back up queue state and secret/configuration files in addition to Thura's
metadata/objects; retain ownership/modes on restore. Review ambiguous outbound
attempts before replaying recovered jobs. External SMTP remains at-least-once.

## Local acceptance fixture

Use the synthetic `*_test` database and normal e2e seed only. Build the image
as `thura-postfix:test`, run `lidza test --e2e` to build/seed the test app, export
the private test `DATABASE_URL` without printing it, then run:

```sh
python3 -m unittest discover -s deploy/postfix -p 'test_*.py'
python3 scripts/postfix-fixture.py --pg-container lidza-dev-postgres
```

The fixture owns temporary loopback ports 3002/3003/2525/2526/2527 and its named container,
so stop other test fixtures first. It verifies actual SMTP ingress, retry deduplication after a lost acknowledgment, unknown-recipient and open-relay refusal, queued delivery while the
app is unavailable, queue survival across MTA restart, and official Līdza SMTP
outbound bytes (including a full 10 MiB attachment) through Postfix to a local sink. It stops its app/container and
removes only its synthetic mailbox metadata. Private raw objects and disposable
queue/log evidence remain in ignored test storage. No internet recipient is used.

## Compose acceptance fixture

This historical transport fixture uses the separate-service profile; the compact
profile has additional [bounded functional checks](compact-deployment.md).
After building the stack images under the isolated project name, run:

```sh
docker compose --env-file .env -f deploy/compose.separate.yaml -p thura-postfix-acceptance build
python3 scripts/postfix-compose-fixture.py --keep
npx playwright test --config scripts/postfix/playwright.config.ts
```

For browser review, pass `--keep` to the Python fixture; otherwise it removes
its isolated containers and volumes. It creates private synthetic configuration
in `.lidza/postfix-compose-fixture`, captures every outgoing message in a private
SMTP sink, configures the actual native wizard, verifies persisted incoming mail
and refuses public-port relay attempts. It changes outgoing delivery to an
independent SMTP provider and confirms Postfix still receives. Use an unused
fixture project, loopback app port 8082 and SMTP port 2528; never point this
fixture at a production installation. The separate browser checks exercise
actual desktop/phone wizard menus and ensure hidden provider fields, secret
values and page width behave correctly. Hosted provider accounts are not used.

## SMTP sender authentication and reports

Public SMTP goes through the bundled Rspamd Milter before queue acceptance.
SPF uses the actual socket peer and envelope sender; DKIM verifies the original
signed content; DMARC requires alignment with the visible From domain.
Published `p=reject` failures are rejected during SMTP, `p=quarantine` failures
are held by Thura even with a low spam score, and monitoring policies retain
the ordinary spam/AI checks. Temporary authentication DNS failures defer mail.
A forged Authentication-Results header cannot supply a passing identity.
Relay HMAC and SMIP authentication remain distinct: the HMAC protects the relay
and its separately stored peer/envelope metadata, not a sender's claimed name.
Existing generic relays without signed SMTP metadata remain unverified.

For direct outgoing SMTP, Rspamd signs locally submitted mail for configured
receiving domains only. Persistent RSA-2048 keys and a unique installation
selector live in `/var/spool/postfix/etc/thura-auth`, on the existing durable
Postfix queue volume. Back up that volume privately. To obtain public DNS TXT
records, read `public-records.json` in that directory inside the mail container.
Publish each selector record before relying on direct DKIM; never copy a private
key into DNS or the setup form. API providers retain their own DKIM signing.
Authorize every legitimate sending service in SPF and publish a DMARC policy;
do not blindly replace a provider's existing DNS records.

Rspamd collects daily aggregate reports when a domain publishes `rua`, including
third-party destination authorization in DNS. Reports contain counts, source IPs
and authentication results, not original message bodies or attachments.
The private loopback report bridge queues them through Thura's normal configured
API/SMTP/SMIP sending pipeline, with immutable submission and duplicate protection.
An ambiguous reporting attempt is recorded before execution and is not silently
repeated after a restart. Inspect `report-attempt.json` in the protected state
directory before an operator-authorized retry; `completed` means the collector
command finished, not proof of external delivery. Inspect the normal outbox for
provider delivery status. Pending daily attempts are bounded to retained Redis
history; this is not an unlimited report archive.

This implementation sends aggregate `rua` reports. Rspamd does not implement
forensic `ruf` failure reports. SMTP rejections return failure to the connected
sender; Thura does not send unsolicited failure messages to a potentially forged
From address. DMARC authenticates a domain, not a human sender or harmless content;
spam, malware and optional AI screening still apply.
