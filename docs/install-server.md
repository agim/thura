# Install and configure a Thura server

Use this guide for a new deployment, then [operations](operations.md) for upgrades, monitoring and recovery. Every operator chooses their own hosts and domains. All names and addresses below are examples; do not publish them literally.

Thura defaults to **Debian Postfix for sending and receiving**. This guide also covers **Mailgun for sending with Postfix for receiving**, plus SendGrid/Postmark/Resend. Choose the arrangement that fits the deployment. Selecting an outbound provider does not configure incoming mail.

## 1. Choose hosts, domains and services

| Purpose | Example | Required service |
| --- | --- | --- |
| Web application | `https://thura.example.org` | Thura binary, HTTPS and background workers |
| Workspace email addresses | `team@example.org` | Exact mailbox mappings and an inbound transport |
| Incoming SMTP host | `mail.example.org` | Postfix on public TCP 25, when receiving directly |
| Application database | Private database endpoint | Postgres; migrations applied before startup |
| File and attachment storage | Persistent private disk or private S3 bucket | Official storage pack |
| Cache/realtime bus | Private endpoint | Valkey when configured; follow deployment settings |
| Optional editing/chat/meet | Operator-selected endpoints | ONLYOFFICE, Matrix and LiveKit/TURN |

Provision the application database with a dedicated role and restrict network access to authorized app/migration/backup processes. Give the migration runner the required schema permissions. Keep Postgres and Valkey off public interfaces, or protect remote connections with authentication, TLS and firewall rules. The cloud development containers' trust authentication is not a production configuration.

For a fresh dedicated Debian PostgreSQL installation, install the distribution's supported Postgres package (Postgres 17 is the tested baseline), then create an unprivileged database owner using an interactive password prompt:

```sh
sudo -u postgres createuser --pwprompt thura
sudo -u postgres createdb --owner=thura thura
```

Configure client authentication, private listeners and TLS for remote clients before use. Do not run these creation commands over an unrelated existing installation without selecting its intended cluster. For Valkey, use the distribution/provider's supported installation instructions (version 8 is the tested baseline); configure private authenticated `CACHE_URL` and `REALTIME_BUS_URL` bindings as needed. Keep their credentials private.

Choose storage before publishing the wizard. Local storage needs persistent disk shared by every node that accesses the same objects; separate local disks on multiple app nodes are insufficient. S3 needs a pre-created private bucket, region/HTTPS endpoint and credentials limited to the app's bucket/prefix. Enable the provider's public-access blocking and choose backup/versioning policies. Thura's storage check does not prove bucket privacy or durability.

Optional services have separate installation guides: [ONLYOFFICE](office.md), [Matrix](chat.md), [LiveKit/TURN](meet.md) and [experimental paired-server SMIP](protocols/smip-thura.md). Leave these disabled until their services and credentials are ready.

## 2. Build and install Thura

Use the `agim/thura` application repository, not a checkout of the Līdza framework. The current application requires published Līdza v0.1.90 and Go 1.27.2 or a later patched release in the supported series. A build host also needs Node/npm; the ordinary production binary embeds its frontend and needs no Node runtime.

On a build host:

```sh
git clone https://github.com/agim/thura.git
cd thura
go install github.com/agim/lidza/cmd/lidza@$(go list -m -f '{{.Version}}' github.com/agim/lidza)
npm ci --no-fund --no-audit
lidza build
```

Put Go's executable directory on PATH. If running verification on this toolchain, install the compatible analyzer with `bash scripts/install-analysis-tools.sh`. Build output is `bin/thura`. Deploy the binary plus this release's `db/`, `mail/`, `admin/` and applicable operator configuration under `/opt/thura`; keep release files read-only and private configuration outside the checkout/image. Do not copy `.env.test`, fixture credentials or development storage into production.

For a Debian-family host, create the dedicated `thura` system user/group. Select exactly one unit:

- Private local storage: [thura-local.service](../deploy/thura-local.service), writable state under `/var/lib/thura`, with storage at `/var/lib/thura/storage`.
- S3 storage: [thura.service](../deploy/thura.service), with the app directory read-only.

On a **new** local-storage host, one installation layout is:

```sh
sudo useradd --system --home /opt/thura --shell /usr/sbin/nologin thura
sudo install -d /opt/thura/bin /opt/thura/admin
sudo install -m 0755 bin/thura /opt/thura/bin/thura
sudo cp -a db mail /opt/thura/
sudo install -d -o thura -g thura -m 0700 /var/lib/thura/config /var/lib/thura/storage
sudo ln -s /var/lib/thura/config /opt/thura/config
sudo install -m 0644 deploy/thura-local.service /etc/systemd/system/thura.service
sudo systemctl daemon-reload
```

These paths must not replace an existing user's account, configuration link or unrelated unit; adapt an existing installation deliberately. The app's admin templates are embedded; install any separate operator overrides if used. Create `/opt/thura/.env`, owned by `thura` and mode 0600, for this unit's deployment bindings; the app must also be able to read it. Copy/mount the protected sealed config and key into the private config directory with the correct ownership, or supply the key through the secret manager. Keep code/assets root-owned and read-only.

Install the selected unit, reload systemd and enable it only after configuring infrastructure and applying migrations. The local variant can also serve S3 if a writable private configuration directory is needed. Follow [the state/config directory ownership instructions](operations.md#debian-local-storage-deployment); directories are mode 0700 and secret files 0600. Link `/opt/thura/config` to the private state directory when appropriate.

Container deployments use the repository Dockerfile. Supply database/auth/master-key settings privately, persist required state, and mount private configuration rather than baking deployment secrets into an image. The runtime uses a non-root account; mounted directories must be writable by that account where writes are required. Publish only the chosen application listener behind HTTPS. Do not expose internal database/cache or unauthenticated SMTP submission ports.

## 3. Prepare secrets, HTTPS and the database

Initialize a master key on the operator's trusted configuration host:

```sh
lidza credentials init
lidza credentials edit
```

The first command creates `config/master.key` and the sealed credentials file without printing key values. The editor lets you enter secrets without putting their values in command arguments or shell history; use a trusted private editor/session. Supply the same master key to each runtime and migration runner through a secret manager/private `LIDZA_MASTER_KEY` binding or the protected key file. Never store the master key inside the encrypted file it must unlock.

Configure these infrastructure values privately before starting:

| Setting | Purpose |
| --- | --- |
| `DATABASE_URL` | This deployment's database, with production credentials |
| `AUTH_SECRET` | Independent unpredictable signing secret, at least 32 characters, identical across app nodes |
| `THURA_SETUP_TOKEN` | Independent unpredictable bootstrap token, at least 32 characters; remove after first-account creation |
| `LIDZA_MASTER_KEY` or protected key file | The generated 32-byte encryption key; keep a separate protected backup |
| Cache/realtime connection settings | Private service credentials/endpoints, if enabled |

Use non-secret deployment settings for `LIDZA_MODE=production`, `LIDZA_LOG=json`, `AUTH_COOKIE_SECURE=true` and a positive `JOBS_WORKERS` value, for example 4. Set `APP_URL=https://thura.example.org` to your actual public origin and enter that exact same value in the wizard. File/process environment settings can override wizard values: avoid conflicting overrides for provider/policy fields the wizard will manage. Do not use log/outbox capture for production email delivery.

For built-in HTTPS, set `LIDZA_TLS_DOMAINS` to your application hostname and optionally `LIDZA_TLS_EMAIL` to the operator contact. Point its A record, and AAAA only if IPv6 is working, at the server. Permit public TCP 80 and 443 for ACME/HTTPS; the supplied systemd units permit binding these ports. Certificates are stored through Postgres. Alternatively terminate HTTPS at your chosen reverse proxy and keep the app listener private; configure its forwarding and WebSocket support, use the real public `APP_URL`, and keep secure cookies enabled.

From a release checkout containing the matching migrations, with production credentials loaded privately:

```sh
LIDZA_MODE=production lidza db migrate --production
```

`--production` permits the CLI to migrate a remote database; it does not select credentials. `LIDZA_MODE=production` selects production-mode settings, while process environment values still take precedence. Check the intended database target privately before running it. This applies the entire pending sequence, including the owner-claim and setup-probe migrations. Existing installations need a recoverable backup first and must migrate successfully **before restarting any new app or worker binary**. Stop on migration failure.

For the unit installed as `thura.service`, start it after configuration and migration success:

```sh
sudo systemctl enable --now thura
sudo systemctl status thura
```

Inspect service logs privately if startup fails. For an existing deployment, use its actual unit/container names rather than enabling a second process. Start the configured Thura unit or container. Restrict initial access to operators while bootstrapping. `/healthz` should return 200; `/readyz` intentionally returns 503 until initial configuration is activated. A setup-required 503 is different from a database/startup failure.

## 4. Create the first administrator and configure the workspace

Open your public application origin over HTTPS. With no users it redirects to `/setup`. Enter the private setup token, name, email and a strong password to create the first server administrator, then sign in. Use an administrator email that already receives mail independently of this new server so the initial delivery check can reach it. First-account creation is serialized and cannot be reopened once an account exists. Remove the bootstrap-token binding afterward.

The administrator enters `/admin/setup` inside the official admin frame. Complete and save each step:

1. Public HTTPS origin, initial workspace name, operator contact, timezone and backup/retention plan.
2. Chosen outbound email service, sender address and separate inbound signing secret.
3. Persistent private local directory or S3 connection settings.
4. Storage quota, member/pending-invitation limit, invitation authority and sharing restrictions.
5. Optional service endpoints/credentials, or leave them disabled.
6. Review, explicitly run mail/storage checks, and publish.

Saved drafts can be resumed or restarted. Blank secret fields preserve saved values; use the clear checkbox to remove a secret. Confirm the test email actually reaches your administrator inbox: provider acceptance alone is insufficient. Storage checks must finish successfully, including deletion. Both results must match the saved provider settings and be less than 24 hours old.

Publish, then restart **every app and worker node** and confirm the intended active revision and `/readyz` success on each node. Publication alone does not activate the first installation. The initial workspace, its owner membership and shared mailbox are created atomically. Its mailbox address comes from the configured `MAIL_FROM`. See [wizard details and recovery](server-setup.md).

Existing deployments cannot create a new first administrator. Privately configure `ADMIN_USERS` with an explicitly authorized existing account ID or email to adopt the wizard. A workspace owner/admin role alone does not grant server configuration authority.

## 5. Configure outbound mail

### SMTP / Debian Postfix

Have a functioning SMTP submission relay before running the wizard's mail check. For a same-host Postfix relay, choose `smtp`, host `127.0.0.1`, port `2525`, security `none`, and leave username/password empty only for a trusted loopback listener. Set the actual sender address. For a different host, use an authenticated TLS or STARTTLS submission endpoint and store its username/password privately; plaintext transport is restricted to loopback.

The supplied Thura-specific Postfix configurator requires at least one real mailbox UUID, which the wizard creates at publication. For a fresh SMTP-only deployment, start a regular distribution Postfix **outbound-only loopback relay** first; do not expose public MX receipt yet. Add its private 2525 submission listener, verify outbound routing/DNS and use it for the wizard email check. After publication, install the exact-recipient Thura transport in step 7, keeping submission on 2525. Alternatively bootstrap with an already working authenticated SMTP service, then switch to Postfix through saved checks and publication. Do not invent mailbox IDs or use an empty relay map with the supplied configurator.

Direct internet SMTP sending needs the DNS/DKIM/PTR and port-25 requirements in step 6. A trusted local relay's acceptance only confirms queueing; inspect its queue and confirm final external delivery.

### Mailgun

Use [Mailgun domain verification documentation](https://documentation.mailgun.com/docs/mailgun/user-manual/domains/domains-verify) and [Mailgun routing documentation](https://documentation.mailgun.com/docs/mailgun/user-manual/receive-forward-store) for current account-specific records and receiving options. In your Mailgun account:

1. Add the actual domain used in your sender address, for example `example.org` for `team@example.org`, and select its US or EU region. For a separate sending subdomain, use a sender address belonging to that verified subdomain. Use the production sending domain, not the sandbox, for general users; sandbox sending is restricted to authorized recipients.
2. Publish the SPF and DKIM records Mailgun provides and wait for verification. DNS record names, selectors, values and record types come from that domain's dashboard. Do not invent keys or substitute another account's records.
3. Create a suitably scoped domain sending credential/API key and enter it privately in the wizard. Do not put it in Git, a URL, screenshots or support logs.
4. Select `mailgun`; set the plain sender address (`MAIL_FROM`), sending domain (`MAIL_DOMAIN`), region (`MAIL_REGION`) and API key (`MAIL_API_KEY`). Unused SMTP fields need no credentials. Keep the inbound signing secret separate from the Mailgun API key.
5. Save, run the wizard email check, confirm receipt, then publish/restart as above.

Mailgun's HTTP sending API does not need a public SMTP submission listener on your app. Invitation/recovery mail uses the global provider. Additional mailboxes can use their own operator-managed credential prefixes; see [mail connectors](mail-connectors.md).

For SendGrid, Postmark or Resend, verify the sender/domain in that service, publish its issued authentication records, select the corresponding provider and enter its private API key. SendGrid's US/EU region selection must match the account. Account permissions, quotas, suppressions and abuse restrictions remain provider-side settings.

## 6. Point DNS and MX at the chosen incoming transport

MX controls **incoming** mail for the domain to which the record belongs. It does not select the app's outbound API provider. Do not replace an organization's working MX merely to enable Mailgun sending. If its existing mail must stay with another service, use a dedicated mail subdomain or a deliberately configured upstream relay.

For direct Postfix receipt of `team@example.org`, the example records are:

| DNS name | Type | Example value / source |
| --- | --- | --- |
| `thura.example.org` | A | Application public IPv4 |
| `mail.example.org` | A | Postfix host public IPv4 |
| `example.org` | MX | Priority 10, `mail.example.org.` |
| Sending domain | TXT, SPF | One SPF policy authorizing the actual outbound senders |
| Mailgun-issued DKIM selector | TXT or CNAME | Exact Mailgun dashboard record |
| `_dmarc.example.org` | TXT | Start with an intentional monitoring policy, such as `v=DMARC1; p=none`, then tighten after observing legitimate mail |

An MX target must resolve directly to the mail host; do not use a web/CDN-proxied hostname or a CNAME as its target. Add IPv6/AAAA only when the listener, routing and firewall work. For mailboxes on a subdomain, publish the MX on that subdomain, not automatically on the parent domain. Lower DNS TTL before a planned cutover and verify propagation before changing traffic.

For Mailgun-only sending, the SPF policy commonly includes Mailgun, but use the actual provider instructions. Merge existing authorized senders into one SPF record; multiple `v=spf1` records break evaluation. Ensure DKIM/SPF alignment with the visible From domain for DMARC. Create and monitor any DMARC report destination you publish; stricter policies can reject legitimate mail if alignment is wrong.

If Postfix sends directly to internet MX hosts, configure a matching reverse-DNS/PTR through the IP provider, forward DNS and SMTP hostname, permit outbound TCP 25, and deploy a DKIM signer such as OpenDKIM. The supplied Postfix adapter does not install a DKIM signer. If outbound 25 is blocked, use Mailgun/the selected API provider or an authenticated submission relay. Incoming TCP 25 still must be reachable when you own the MX.

## 7. Install and map incoming Postfix mail

Follow [the complete Postfix installation guide](postfix.md) for a new dedicated native Debian-family MTA or the supplied container. It supplies a queued adapter to Thura and exact recipient checks. Avoid running its configurator over an unrelated existing MTA.

After the initial workspace/mailbox exists, obtain that mailbox's UUID from the authorized mailbox API/Mail interface or the operator provisioning result. Configure a private relay JSON file with:

```json
{
  "baseUrl": "https://thura.example.org",
  "mailboxes": {
    "team@example.org": {
      "id": "REPLACE_WITH_THE_ACTUAL_MAILBOX_UUID",
      "secret": "REPLACE_PRIVATELY_WITH_ITS_INBOUND_SIGNING_SECRET"
    }
  }
}
```

These replacement strings are placeholders, not working credentials. Copy [the relay example](../deploy/postfix/relay.example.json) into a private operator-owned file, mode 0600; never commit the completed file. Use the wizard's `MAIL_INBOUND_SECRET` for a mailbox with an empty configuration prefix. Prefixed mailboxes use that prefix's separate secret. The relay origin can be loopback HTTP only on the same host; other hosts use HTTPS without redirects.

Mount the private JSON and persist the entire Postfix queue. For public receipt, enable `SMTP_LISTEN=25`, open incoming TCP 25 and supply the SMTP certificate/key paths described in the Postfix guide. The private outbound submission listener remains loopback port 2525; do not publish it or broaden `mynetworks`. With Mailgun sending, Thura does not need to submit outbound messages to that listener.

Map every accepted address explicitly. Unknown recipients, unmapped plus addresses and unauthorized relay attempts must be refused. For additional shared mailboxes, an operator uses local MCP `app_provision_mailbox` with workspace UUID, name, address and optional credential prefix, then adds its exact inbound mapping. This assigns a mailbox to a workspace; adding a user does not automatically create a personal mailbox or DNS record.

Start/reload the MTA only with correct mappings and secrets. External senders can now reach Postfix, which signs the complete raw message and sends it to `POST /api/v1/inbound/{mailboxId}`. Thura verifies the HMAC/timestamp/delivery ID and stores private MIME/attachments. During app outages the queue defers delivery and retries. Monitor queue age and failures.

**Hosted Mailgun/SendGrid inbound forwarding is not a turnkey adapter in this release.** Do not point a provider's ordinary webhook directly at the Thura endpoint: its payload/authentication do not match the signed raw-MIME contract. A hosted receiving deployment needs an adapter that authenticates the provider, preserves exact raw MIME and stable delivery IDs, and creates Thura's signatures. Until that adapter is installed and validated, keep MX on the supported Postfix transport or your working existing receiving service. Mailgun MX records apply only when you deliberately choose its receiving service; they are not required to use its outbound API with Postfix receiving.

## 8. Invite and manage users

Public signup is disabled. After activation, the initial owner opens `/app`, chooses the workspace and uses **Members** to invite a person's exact email address with an admin or member role. Owners can invite; admins can do so only if the setup policy permits it. Invitations and current members count toward the configured seat limit.

The recipient follows the email link, creates a password account if needed or signs in with the invited email, then accepts. Invitations expire after 72 hours and can be revoked. Acceptance rechecks permissions, seat limits and the invited email; it does not create a second workspace. If an invitation cannot be delivered, inspect the mail outbox/jobs and provider suppressions, sender verification and `APP_URL` before retrying.

Workspace ownership controls workspace data; server administration controls deployment configuration and credentials. Removing a member blocks app access, while optional external provider sessions have their documented reconciliation windows. Keep at least one owner; ordinary members cannot promote themselves or gain server-admin access. Use each account's password recovery flow with working outbound mail rather than sharing passwords.

Additional operator provisioning is available through local `lidza mcp`: `app_provision_account`, `app_create_workspace` and `app_provision_mailbox`. Supply account passwords privately. These are privileged operations, not public signup APIs. Leave remote `/mcp` disabled unless an operator deliberately configures and protects its token.

## 9. Verify before opening access

- Every node reports the intended active revision and successful readiness; HTTPS and secure cookies work through the actual public origin.
- An external address can receive invitations/recovery mail and reply to a mapped workspace mailbox. Confirm SPF/DKIM/DMARC results in the received headers; provider acceptance is not inbox delivery.
- Incoming mail accepts mapped recipients and refuses unknown recipients/open relay. Queueing survives an app/MTA restart, and signed retries do not duplicate ingestion.
- Users can accept invitations; ordinary members cannot access `/admin/setup`, and removed members lose workspace API access.
- Private files upload/download, unauthorized reads fail, quotas apply, and data survives node/container restarts. A local-disk path or S3 connection check is not a backup.
- Positive worker counts process scheduled mail, reminders and cleanup. Monitor failed jobs, queue age, storage capacity and database/service health.
- Backups cover Postgres, private objects, sealed settings and separately protected master keys, plus optional provider/MTA state. Test a clean restore and configure scheduling, independent copies, retention and alerts.

Use [operations](operations.md) for backup/restore commands and upgrade ordering. Each deployment must verify its own public domain, credentials, network and optional media devices; the repository's synthetic fixtures do not establish those results.
