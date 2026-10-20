# Compact deployment: four containers on a 4 GiB server

`deploy/compose.yaml` is the default small-team profile. See
[deployment profiles](deployment-profiles.md) for optional 8/16 GiB resource
overrides and their validation status. It keeps all existing
services, with two vCPU in aggregate CPU quotas and **3584 MiB of aggregate
container memory limits**, no swap. Reserve the remaining **512 MiB** of a
4 GiB Linux server for its kernel, Docker and an existing HTTPS reverse proxy.
The host, user files, mail storage and backups are outside the container RAM
limits. Docker Desktop's own VM overhead is not included in this Linux sizing.

| Container | Processes | CPU limit | RAM limit |
|---|---|---:|---:|
| `thura` | Thura + LiveKit | 0.50 | 512 MiB |
| `postgres` | PostgreSQL | 0.25 | 256 MiB |
| `postfix` | Postfix + Rspamd + ClamAV + Freshclam + Valkey | 0.75 | 1792 MiB |
| `collabora` | Collabora CODE | 0.50 | 1024 MiB |

There is no storage-initialization container: the application supervisor
initializes the volume mount before dropping both application processes to UID
65532. Each container's supervisor exits if a supervised service dies so Docker
can restart it. Health checks verify local application/media and mail services.
Postgres has a bounded pool, 64 MiB shared buffers and conservative work memory.

This profile is for light, small-team use: fewer than 1,000 emails/day, roughly
one or two documents open at once, and small calls. The acceptance test uses a two-participant call. It is not a capacity guarantee
for 100 simultaneous members,
large spreadsheets, PDF conversions and video calls at once. Recordings and
local LLM hosting are excluded. Optional AI uses the setup-configured API.
Consolidating services alone does not halve RAM: the limits and concurrency
settings provide the actual budget.

## Mail and learning

All mail protection remains enabled when configured. ClamAV loads the complete
standard signature databases and keeps existing scan/archive limits. It uses
one scan thread and `ConcurrentDatabaseReload no`, avoiding a second scanning
engine during signature updates. The updater also uses `TestDatabases no` to
avoid loading its own duplicate scanning engine; downloaded database signature
verification remains enabled, and clamd still loads/validates the database. Scans pause during reload; required scans that
time out remain held and retry. No signature family or malware check is removed.
Rspamd also scans one message at a time. Postfix retains its durable queue.

One loopback-only Valkey replaces the two former services: learning stays in
**database 0**, app cache in **database 1**, and realtime pub/sub in **database 2**.
It persists AOF data with `noeviction`. Its 96 MiB data cap is deliberately small;
capacity exhaustion is an error, never silent eviction of learned corrections.
Monitor it and increase the budget or choose the separate profile when needed.
User corrections remain durable in Postgres and retry when learning is unavailable.

## New installations

```
python3 scripts/init-compose.py
# Privately edit .env with COLLABORA_IMAGE, THURA_WOPI_ORIGIN,
# LIVEKIT_PUBLIC_URL and your public domains/ports.
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

Choose a tested official Collabora digest for the server architecture, as described
in [Collabora deployment](collabora.md). `THURA_WOPI_ORIGIN` defaults to the private
`http://thura:3000` callback; Thura explicitly recognizes that internal host alias.
Use a public HTTPS origin instead when your domain/proxy configuration requires
it. Collabora's service port and LiveKit signaling port are published only on
loopback, for the operator's existing HTTPS proxy. Set `LIVEKIT_PUBLIC_URL` to its
browser-reachable `wss://` origin. Leaving it empty keeps meeting joins unavailable
until configured; it does not generate insecure public meeting links.

Expose TCP 7881 and UDP 7882 directly for media. On a NAT host, set
`THURA_RTC_NODE_IP` to its reachable public IP when automatic discovery is unsuitable.
TURN for restrictive client networks needs separate operator configuration; this
profile does not promise universal connectivity or include a recording service.
The existing setup wizard still chooses receiving, API sending and optional AI.

## Existing installations

Back up the database, deployment secrets, object storage, mail queue and learned
statistics first. Keep the **same Compose project name** and existing named
volumes. Add only missing credentials:

```
python3 scripts/init-compose.py --upgrade-compact
# Privately configure the public origins and tested Collabora digest.
docker compose --env-file .env -f deploy/compose.yaml down
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

Do **not** use `down -v`. The old `mail-filter-learning` AOF volume is mounted in
the combined mail service; startup adjusts its filesystem ownership for Debian's
Valkey user without deleting data. Learning database 0 and team prefixes remain
unchanged. Old app-cache data is disposable and is not migrated. The old
`valkey` and `mail-filter-cache` containers must be stopped before the new writer
starts. Keep their old volumes until the upgrade and backup restoration have
been verified. Do not mount one AOF or signature volume into two active writers.

The prior arrangement remains available as `deploy/compose.separate.yaml` for
operators who want independent failure/restart boundaries or larger workloads;
add the existing separate Collabora/LiveKit deployments as needed. Consolidated
services restart together, so an application update also restarts its local SFU.
Apply updates outside active calls.

## Verification

Use an isolated `thura-compact-*` Compose project and synthetic accounts from
`e2e/seed.sql`; never seed those accounts into production. The compact acceptance
checks are:

```
python3 -m unittest discover -s deploy/compact
python3 scripts/compact-smoke.py --project thura-compact-acceptance
BASE_URL=http://127.0.0.1:18080 npx playwright test -c integration/playwright.config.ts
BASE_URL=http://127.0.0.1:18080 npx playwright test -c integration/compact.playwright.config.ts
```

The smoke test checks the real Rspamd controller's reversible/replayed learning,
clean and EICAR antivirus scans, a full signature reload, service health, memory
limits, CPU quotas and OOM counters. Browser checks edit/save and inspect actual
DOCX/XLSX output, and exchange real media between desktop and a phone-sized
Chromium browser. Phone viewport emulation is not a new physical Android test.
See [recorded acceptance evidence](benchmarks/compact-deployment.json).
