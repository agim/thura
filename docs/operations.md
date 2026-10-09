# Deployment and recovery

Thura is a generated application on published Līdza v0.1.90. Build with
`lidza build`; deploy the binary, production assets, and operator configuration
using the generated Dockerfile/systemd deployment. Use a separate Postgres
database and private local or S3-compatible storage. Valkey supplies the cache.
All instances need the same sealed credential configuration and master key.
The default mail service is [Debian Postfix](postfix.md); its host, mappings and
public domains are chosen per deployment.
Set a public HTTPS `APP_URL`, trusted proxy settings, production auth cookies,
and working mail credentials before inviting real users. Disable development
outbox capture in production. Initial owners are provisioned through the
operator MCP tools described in [implementation status](implementation-status.md).

The six live apps are available at `/app`. Optional integrations need separate
services: [ONLYOFFICE](office.md), [Matrix](chat.md), and [LiveKit](meet.md).
Use their exact configuration requirements, including private document storage,
Matrix application-service namespaces, and LiveKit `room.auto_create: false`.
The tested service versions are compatibility pins, not a claim that these are
the newest secure releases. Review vendor advisories before public deployment.
Keep `/workspace` labelled as the browser-local sample.

## Debian local-storage deployment

The generated `deploy/thura.service` treats `/opt/thura` as read-only. When
using private local object storage, select `deploy/thura-local.service` instead.
It gives the application one mode-0700 state directory at `/var/lib/thura`;
set `STORAGE_PROVIDER=local` and `STORAGE_DIR=/var/lib/thura/storage` in the
operator's private `/opt/thura/.env`. Environment-file values override unit
defaults, so verify that path instead of leaving a relative `storage` value.
Create the `thura` system user/group and install the binary/assets as in the
generated unit. Install and enable only one of the app units.

For editable sealed operator settings, store the private config directory at
`/var/lib/thura/config` owned by `thura`, with directory mode 0700 and files
0600, and link `/opt/thura/config` to it before starting. Supply the master key
privately; it does not belong in an image or a Git checkout. Keep code/assets
read-only. Back up both storage and sealed configuration/master-key sets.
S3 deployments can retain the generated app unit and use the selected
provider's backup/access design. Installation on an actual operator host is a
deployment operation; the systemd unit has not been started on a public host
by this implementation.

## Limits and jobs

Drive defaults to a 1 GiB logical quota per workspace, configurable with
`DRIVE_QUOTA_BYTES` between 1 MiB and 1 TiB. Retained versions and trash count;
unexpired uploads reserve their full expected size. Workspace locks serialize
reservations, version saves, folder creation, and permanent deletion. Each
workspace permits 500 retained files, 500 folders, 100 active uploads and 100
versions per file. Mail attachments and Matrix copies are outside this Drive
quota, as are bounded derived Drive previews. Budget their storage separately; physical Drive bytes can temporarily
exceed logical usage while chunks and deletion jobs await cleanup.

Hourly upload cleanup processes at most 100 expired sessions per run. Completed
sessions support idempotent finalization for up to 24 hours. File/chunk deletion
jobs commit in the same database transaction as removal of their references;
the worker rechecks references before touching storage. Daily orphan sweeps
subdivide full 500-object listings into bounded prefix jobs, preserving objects
newer than 48 hours and anything still referenced. They only touch generated
`drive/files/`, `drive/uploads/` and `drive/previews/` keys. Retries tolerate already-missing objects.
Storage failure leaves a retryable job rather than silently forgetting deletion.

Preview jobs use one decoder worker across nodes and the ordinary durable retry budget. Failed previews can be explicitly retried; unsupported files remain downloadable.

Workspace owners/admins can browse scoped audit history from Members, filter by action/outcome and page through records. Workspace creation, invitations, membership/role changes, file-share creation/revocation and permanent deletion record successful events in the business transaction. Refused access/sharing changes record denied/failed outcomes with static action/resource names and HTTP status, without request bodies, addresses or tokens. Actors come from the authenticated context; provisioning uses a named system actor. The official audit pack also records operator admin actions. `AUDIT_RETENTION` defaults to `8760h` and prunes daily; `0` keeps all events. Choose retention and access controls per deployment, back up the audit table, and monitor failed audit writes: a required audit failure rolls back the associated change. This is an operational history, not an immutable external audit archive.

Watch Līdza job status/failures and structured request logs. Include queue age,
mail delivery failures, storage capacity, Postgres backups, document callbacks,
Matrix federation health, and LiveKit webhook/reconciliation failures in operator
monitoring. Log capture/outbox delivery is not evidence of internet delivery.
Calendar, Matrix membership, and meeting reconciliation jobs are bounded; large
backlogs require repeated runs. Membership removal blocks app APIs immediately,
while ongoing provider sessions have the windows documented in each connector.

## Offline backup and clean restore

`scripts/backup.py` backs up Postgres and **local storage** as one offline bundle.
It uses Postgres client tools matching the server major version, Python 3, and
`DATABASE_URL` exported privately in the process environment. Credentials stay
off command arguments. The optional `--pg-container` runs client tools inside
an existing container; the configured database host must be reachable there.

Stop every app instance, worker, document save, and inbound relay before taking
the snapshot, and keep them stopped until it completes. The required
`--maintenance-ack` records this operator precondition; the script cannot verify
that all distributed writers are stopped. Both metadata and objects must be
captured from the same maintenance window.

```sh
python3 scripts/backup.py backup --storage /srv/thura/storage \
  --bundle /srv/private-backups/thura-2026-10-08 --maintenance-ack

# Export DATABASE_URL for a newly created, empty recovery database first.
python3 scripts/backup.py restore --storage /srv/thura-recovery/storage \
  --bundle /srv/private-backups/thura-2026-10-08 --maintenance-ack
```

The bundle contains a custom-format dump, every local object and its metadata
sidecar, and a SHA-256 manifest with the application commit. New bundles refuse
existing destinations. Restore verifies the complete inventory before writing,
refuses a database containing user tables/views/sequences and nonempty storage,
and restores metadata in one transaction. It rechecks all copied file hashes.
If copying fails after database restore, keep the app stopped and retry into
another clean target. The bundle checksum detects corruption; it does not
authenticate a maliciously replaced manifest. Bundles contain private account
and workspace data: keep them on encrypted, access-controlled storage with an
independent immutable copy and a retention policy. Never commit a bundle.

For S3 storage, use provider versioning/snapshots and a complete inventory from
the same stopped-writer window; this script does not back up remote buckets.
Back up the sealed credential file, master key, environment configuration,
service definitions, and TLS/DNS configuration separately through the operator's
secret backup process. Loss of a master key can make restored credentials
unusable. Restore those secrets before starting the recovered application.

Integration state is an additional backup set:

- Matrix: homeserver database, media store, signing keys, application-service
  registration/token and homeserver configuration. Restore the same server
  identity and domain; app metadata alone cannot recover room history or copies.
- ONLYOFFICE: JWT configuration and any unsaved recovery/cache state required by
  its operation. Finish saves before the snapshot. Committed versions live in
  Thura storage; an unsaved editor buffer is not a durable Thura version.
- LiveKit: server/API keys, webhook and TURN configuration. Active media rooms
  are ephemeral and are not restored as connected sessions. Recording and
  transcription are disabled; no recording backup is implied.
- Mail: connector credentials plus upstream provider/MTA mailbox and queue state.
  Reconcile ambiguous outbound attempts before replaying restored delivery jobs.
  Restoring a snapshot may replay work already completed after that snapshot.

On a clean instance verify account sign-in/recovery, membership, private file
bytes/checksums and history, revoked shares, calendar UID/sequence/RSVP state,
provider connectivity and job idempotency before routing traffic. A local
metadata/object restore checks all table counts, checksummed files, retained versions and derived-preview metadata, populated-target refusal and corrupted-backup refusal. See the latest measured result in [implementation status](implementation-status.md). The reproducible synthetic test is
`python3 scripts/recovery-fixture.py --storage .lidza/test-storage --output .lidza/recovery-check --maintenance-ack`
with a privately exported `DATABASE_URL` ending in `_test` and all writers stopped.
The fixture seeds and then removes a tiny preview in the synthetic source database/storage, and drops its uniquely named recovery database; it retains its
local output for inspection. It deliberately corrupts its test snapshot after
verifying recovery, so that snapshot is not a usable backup. Production
integration-state recovery, real mail-domain delivery and physical-device/TURN
acceptance remain deployment gates.

## Release checks and upgrades

Run `lidza gen`, `lidza check --json`, `lidza test`, `lidza test --e2e`, and
`lidza build`. Dedicated real-service suites and their fixture commands are in
the connector documents; run them with distinct Playwright output directories.
Run runtime dependency audits and review the layout/performance diagnostics.
The pre-commit hook runs `lidza verify`. Completed slices are pushed without
waiting for GitHub Actions results.

Upgrade published Līdza with `lidza update`, inspect generated migrations,
re-run the suites, and exercise restore with the new schema. Upgrade each
optional service independently through its documented migration path and
compatibility tests. Preserve a recoverable snapshot and the matching app
revision before applying irreversible schema/provider changes.

## Opt-in SMIP chat and file transport

SMIP is default-disabled and separate from Matrix conversations. [The SMIP operator guide](protocols/smip-thura.md) covers sealed pairing keys/origins, direct TLS 1.3, consistent identity across worker nodes, transactional workspace consent, durable sending/reconciliation, quotas/retention and explicit Drive import. Keep workers enabled in deployed apps and browser/provider checks; `JOBS_WORKERS=0` is scoped to deterministic full Go verification. Operators choose every deployment origin/domain and must configure both peers; no automatic discovery, SMTP fallback or public deployment is implied.

## Initial server configuration

Use the [resumable setup wizard](server-setup.md) for a new server administrator, provider credentials, quotas and restrictions. Supply a private unpredictable `THURA_SETUP_TOKEN` before first-account creation and remove it afterward. Existing deployments require an explicitly listed `ADMIN_USERS` account. Publishing writes an encrypted configuration snapshot and initial workspace/mailbox atomically; restart every node to activate it. Draft restart preserves active settings. Back up the `server_setup` snapshots with Postgres and protect the master key separately. Ordinary app pushes do not change cloud environment settings.
