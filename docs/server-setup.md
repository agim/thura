# Resumable server setup

For installation, DNS/MX, Mailgun/Postfix, storage and user onboarding, start with [Install and configure a Thura server](install-server.md).

A deployment with no users automatically redirects page requests to `/setup` to create its first server administrator. Until published configuration has been activated successfully on this server, signed-out visitors stay on `/setup` for sign-in, and the signed-in server administrator automatically opens `/admin/setup`. Other application and built-in admin pages redirect to the appropriate setup page; ordinary app APIs return HTTP 503 with a setup-required response. Health checks, setup resources and essential sign-in endpoints remain available. Redirects are not cached, and saving an unfinished draft does not unlock the app. Successful startup activation unlocks normal routes; restarting a draft later preserves availability through the existing published settings. The wizard is part of the official Līdza admin frame. Workspace owners/admins do not automatically become server administrators.

## Before the first account

The operator installs Thura, initializes its master key, configures Postgres and any optional Valkey service, private persistent disk and HTTPS, and supplies `THURA_SETUP_TOKEN` through sealed credentials or a private environment binding. Use at least 32 unpredictable characters from a cryptographically secure generator. Do not put it in a URL, image, Git, shell history or chat. The setup form accepts this token in a password field over the deployment's HTTPS connection. Keep the server behind operator access until this bootstrap is complete.

A row lock and account-count check permit exactly one successful account creation. The token does not grant access after any account exists. The password uses the official authentication policy and Argon2id hash; the operator token is never persisted in the wizard. Account creation, administrator binding, initial encrypted draft and audit commit together. Sign in after creation; if its response was lost, try the same credentials rather than creating a second account. Remove the setup-token binding afterward.

An existing deployment cannot reopen first-account creation. Its operator explicitly lists an existing account ID or email in `ADMIN_USERS` to adopt the wizard. An email entry admits only an account whose email is verified, so an invited account activated through a separate delivery address must be listed by ID. A workspace role alone is insufficient. Public account registration stays disabled; new members join by invitation.

## Steps

Fresh installations prefill the storage prefix with `thura/<installation-id>/`,
using a new random installation UUID rather than the singleton setup row ID or a shared app name. Independent
databases can share a private S3 bucket without using the same object namespace.
Explicit `STORAGE_PREFIX` environment overrides are preserved. The prefix survives
wizard draft resets and is frozen with the storage location after publication.
Existing installations, including those with an empty prefix, are not relocated.
Restrict each installation's S3 credentials to its prefix; a prefix alone is not
an access-control boundary. Restoring a database preserves its namespace; a
separate clone needs an explicit storage migration rather than a renamed prefix.

The wizard covers server, mail, storage, access, AI and all core service settings
below. Host infrastructure is provisioned by Deploy. Publication requires chat,
document editing, meetings, AI, SMIP mail and paired SMIP chat/files to be
configured and enabled; partial drafts can still be saved and resumed. Existing
published installations continue to boot on upgrade, but their next publication
must complete the core-service configuration. AI can use an existing local Ollama
service or a hosted API; setup does not silently provision a model, purchase API
access, or invent a trusted SMIP peer. The four-container compact deployment
supplies Collabora, LiveKit, mail/filtering and storage; a Matrix homeserver and
an AI model service must also be reachable. Their resources are additional to
the compact stack if hosted on the same machine.

1. **Server and workspace:** operator-selected public HTTPS origin, initial workspace name, operator contact, IANA timezone, backup and retention plan.
2. **Email:** SMTP (including Debian Postfix), Mailgun, SendGrid, Postmark or Resend; verified sender, region/domain, API key or SMTP host/port/encryption and credentials. Receiving and sending are independent: select the included managed Postfix service for receiving and either Postfix or a hosted/SMTP provider for sending. The Compose sidecar receives exact mailbox mappings automatically after publication and activation. External raw-MIME relays still require their own signed configuration; hosted-provider-native ingress webhooks are not provisioned. See [Postfix Compose deployment](postfix.md).
3. **Storage:** private persistent local directory or S3-compatible endpoint, region, private bucket/prefix and access/secret keys. Pre-create the bucket and give its key only the required bucket/prefix permissions. The framework selects virtual-host requests for AWS and path-style requests for other endpoints; this wizard does not expose the unsupported pointer-valued environment override in Līdza v0.1.90.
4. **Quotas and access:** retained/reserved Drive bytes per workspace (1 MiB–1 TiB), maximum members plus live pending invitations (1–1000), owner-only versus admin invitations, external/anonymous sharing restrictions, email recipient limit (1–50). Owner/admin/member permissions remain the implemented role model; no arbitrary permission editor is implied. Uploads stay bounded at 10 MiB, and shares at 30 days. Anonymous links default off for wizard-published deployments; existing deployments retain their prior behavior until configured.
5. **AI service:** Ollama, OpenAI, Anthropic, Gemini or an OpenAI-compatible service; model name, service URL, API credentials and output-token limit. All are saved encrypted with the wizard. No inference is performed merely by saving configuration. Subsequent AI Settings changes are retained across restarts.
6. **Chat, documents, meetings and SMIP:** Matrix homeserver/application-service credentials, Collabora service/app URLs and access-token signing secret, LiveKit API/browser endpoints and credentials, and SMIP's encrypted pairing configuration. SMIP mail identity is configured in the email step. All are required for publication. Provider-side service setup, TURN, trusted peer agreement and device acceptance remain necessary.
7. **Review:** core-service validation, stored secret names, explicit email/storage checks and deployment checklist; publish confirmation. Configuration validation does not prove external service connectivity, public DNS or end-to-end call/peer interoperability.

On first publication, `postmaster`, `abuse`, `hostmaster`, `webmaster`, `security`,
`noc`, `admin`, `administrator`, `root` and `mailer-daemon` on the initial inbox
domain become exact aliases of the initial workspace mailbox. The first server
administrator owns that workspace and can read these notices. Existing workspace
mailbox permissions still apply; this does not create a separate private inbox.
Aliases do not create external forwarding or a catch-all. Postfix and SMIP use
the same target mailbox, retain the original message recipient and run normal
abuse screening. Conflicting addresses prevent publication rather than taking
another mailbox's address. External relay operators must direct the same role
addresses to that mailbox's signed ingress endpoint.

Save a step before leaving it. Returning to any step restores non-secret fields and shows whether each secret is saved. Blank secret input preserves its value; the separate clear checkbox removes it. No secret is rendered into HTML, returned by the bootstrap API or stored in browser persistence. Draft values are not used by running providers. Saving an old revision returns a conflict; reload instead of overwriting another tab's work.

## Encryption and publication

Draft and published configuration are separate AES-256-GCM snapshots in `server_setup`, encrypted with Līdza's master key. The password hash lives in the official auth table, not these snapshots. The single published snapshot avoids partial activation of separately saved provider fields. Business audits contain actions/revisions and account IDs, never configuration bodies or secret values.

Publishing persists settings without activating a running node. Publishing validates required fields, provider prerequisites, HTTPS settings, limits and successful checks bound to the saved provider settings, and commits the snapshot, initial owner workspace/shared mailbox and audit in one transaction. An audit failure leaves all of them unpublished. Repeating the same publication does not duplicate its workspace or audit. Changes to the default sender do not rewrite addresses of existing mailboxes; those are separate workspace resources.

**Existing deployments must apply all pending database migrations before restarting any app or worker node with this release.** From a checkout containing the new release's migration files, run `LIDZA_MODE=production lidza db migrate --production` with the deployment's database credentials supplied privately. Confirm migration success before restarting. This includes the additive owner-claim migration and both setup-probe migrations when upgrading from before these changes. Take a recoverable backup first; do not select only one migration or skip a failed migration.

**Restart every app/worker node to activate a published revision.** Startup loads the whole snapshot through Līdza's runtime credential overrides and reconfigures the official packs before app jobs/listeners run. Setup status distinguishes published configuration from the revision actually loaded by this node. The app and `/readyz` remain unavailable on a first installation until activation succeeds; `/healthz` stays available. Already initialized nodes keep serving their previous valid configuration during an update and report that a restart is required. A missing or unreadable published snapshot fails startup instead of opening the app. Draft reset does not change the active snapshot. The wizard refuses conflicting process environment overrides when publishing; deployment environment variables retain normal higher precedence at startup. Avoid later adding conflicting overrides. Use the wizard for settings it manages: advanced built-in credential edits to those names can be superseded by its published snapshot at the next startup.

Changing storage credentials for the same location is supported. Changing its backend, directory, bucket, endpoint, region or prefix after the first publication is refused even while empty, because other nodes could still write to the old location. Existing app files/mail/upload sessions also prevent relocation during initial adoption; use a separately planned data migration. Resetting the draft does not erase active credentials, accounts, transfers or files. There is no implicit secret-key rotation, recall or deletion of prior backups.

Publishing config does not deploy a public host, create DNS/buckets, test a provider credential against its service, send a test email, restart processes or migrate storage. Complete verified sending domains/SPF/DKIM/DMARC, inbound relay, DNS/TLS, private persistence, protected master-key backups and a tested restore. Provider-specific acceptance fixtures remain documented separately. The backup plan records operator responsibilities; it does not schedule backups or promise automatic retention. Test delivery and storage on the actual deployment before inviting users.

Postgres credentials, Valkey connectivity, operating-system service configuration and master-key injection are prerequisites to reaching this wizard, not settings it can safely replace while running. Never paste these infrastructure credentials into unrelated wizard fields.

## Provider checks

Save every step, then open Review. The page checks the saved draft automatically,
sequentially: email, storage, Matrix, Collabora, LiveKit, AI and secure delivery.
It reuses current successful checks and never automatically retries failed or
uncertain attempts. Email sends one test message to the signed-in administrator;
AI makes one small configured-model request. Provider acceptance does not prove
inbox delivery. Storage tests a unique private object; success does not prove
bucket privacy, durability or a backup policy.

All seven successful checks must be less than 24 hours old before publishing.
Results are bound to relevant settings and credentials with a master-key HMAC.
Changing a connection requires a new check. Retrying supersedes earlier success;
a later failure cannot be hidden by it. Email and AI checks are limited to three
per administrator in fifteen minutes because they can send messages or incur
charges. Other service checks allow thirty attempts in that interval to support
troubleshooting. The limits apply across nodes. The compact review shows current
results, with failure details and expandable history.

Intent and audit commit before provider contact. Resubmitting the same ID never
repeats its operation, even after restart or a lost response. Retry restores its
button and reads the recorded outcome even when the new result is another
failure. An uncertain request is followed by reads rather than an automatic
resend. Inspect the provider/inbox before explicitly retrying an uncertain mail
attempt. Integration checks are bounded to sixty seconds; individual storage and
mail operations use fifteen-second deadlines, with independent five-second
completion/cleanup deadlines. Browser failures remain generic to avoid exposing
credentials from provider responses.

A storage check always attempts to delete only its own `_thura_setup_checks/<check ID>` object beneath the configured prefix, including after an ambiguous write. If deletion cannot be confirmed, the wizard records **cleanup_required**, refuses to treat it as a successful check, and shows the ID for manual cleanup. A crash during a check can also leave that object; remove only the exact recorded check object after confirming the process is no longer running it. App orphan cleanup does not scan this reserved prefix. The `setup_probe` ledger is durable and retained for replay protection; include it in database backups.

The Email service step also offers the separate opt-in SMIP mail identity. [Provision DNS, signing keys and direct TLS](smip-mail.md) before enabling it. It prefers verified SMIP for Mail messages, keeps learned secure domains from downgrading, and leaves ordinary provider configuration available for unsupported recipient domains.

## Cloud storage profiles

Choose cloud object storage, then select a profile. Existing installations default
to **Custom S3-compatible**, retaining their saved endpoint, region, credentials
and namespace until the administrator explicitly changes them. Provider selection
never creates a bucket, changes its privacy, moves objects or rewrites the unique
installation prefix. Existing published storage-location migration guards remain.

| Profile | Endpoint and signing | Credentials |
| --- | --- | --- |
| AWS S3 | Regional S3 endpoint from the selected AWS bucket region | IAM access key ID and secret with bucket/prefix permissions |
| DigitalOcean Spaces | Selected Spaces region endpoint; `us-east-1` signing region | Spaces access key pair, not a DigitalOcean personal API token |
| Google Cloud Storage | `https://storage.googleapis.com`, signing region `auto` | Cloud Storage HMAC access ID and secret, not service-account JSON or an API key |
| Custom S3-compatible | Operator-supplied HTTPS service origin and signing region | Keys issued by that storage service |

Endpoints for named profiles are derived again on the server, not trusted from
browser fields. The official storage pack selects bucket addressing from the
endpoint. Changing providers/hosts does not reuse another vendor’s saved keys;
enter the new provider’s key pair. A region change within the same known provider
retains keys. Choose a private existing bucket with access to upload, read,
inspect, list and delete objects beneath this installation’s prefix. Every actual
bucket must pass Review’s live storage probe before publication.

Profile defaults follow the [AWS S3 endpoint reference](https://docs.aws.amazon.com/general/latest/gr/s3.html),
[DigitalOcean Spaces setup](https://docs.digitalocean.com/products/spaces/reference/aws-sdks/)
and [Google XML API interoperability guide](https://docs.cloud.google.com/storage/docs/aws-simple-migration).
Protocol tests use isolated TLS/XML fixtures; they are not claims of live
acceptance against accounts at all three providers.

## SMIP transport behind Lidza-deploy

Thura's manifest requires Lidza-deploy agent **and** control panel v0.2.103 or later. Updating only the control panel does not configure transport. Prepare the host gateway with the released helper, merge its private agent configuration, and install the reviewed Caddy mTLS snippet into the existing HTTPS site without changing HTTP domain verification or ACME. See the [released gateway rollout](https://github.com/agim/lidza-deploy/blob/v0.2.103/docs/gateway-transport.md). Deployment supplies Thura's private listener credentials automatically. No private listener port is opened publicly.

A review error `public SMIP route does not prove TLS 1.3; HTTP 400` means the public challenge failed its transport gate. Check that the agent is updated, host mTLS ingress is active, and the current Thura release declares the protected manifest paths. A valid browser HTTPS certificate alone does not prove transport to Thura. Retry Secure delivery after fixing the route; never bypass it with forwarded HTTPS headers or mark the result passed manually.
