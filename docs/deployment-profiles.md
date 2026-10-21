# Thura deployment profiles

The standalone minimum remains `deploy/compose.yaml`. Larger profiles are
optional resource overrides, keeping the same four-container arrangement.
They do not require a different database schema or move existing volumes.

| Profile | Host CPU | Host RAM | Container limits | Host allowance | Validation |
|---|---:|---:|---:|---:|---|
| Minimum | 2 vCPU | 4 GiB | 3.5 GiB | 512 MiB | Small-team functional acceptance passed |
| Standard | 4 vCPU | 8 GiB | 7 GiB | 1 GiB | Sizing proposal; capacity not benchmarked |
| Performance | 8 vCPU | 16 GiB | 14 GiB | 2 GiB | Sizing proposal; capacity not benchmarked |

`deploy/profiles/catalog.json` records these budgets and their validation status.
The service limits are:

| Service | Minimum RAM / CPU | Standard RAM / CPU | Performance RAM / CPU |
|---|---:|---:|---:|
| Thura + LiveKit | 512 MiB / 0.50 | 1024 MiB / 1.00 | 2048 MiB / 2.00 |
| Managed PostgreSQL | 256 MiB / 0.25 | 512 MiB / 0.50 | 1024 MiB / 1.00 |
| Mail, antivirus, Valkey | 1792 MiB / 0.75 | 2560 MiB / 1.00 | 4096 MiB / 2.00 |
| Collabora | 1024 MiB / 0.50 | 3072 MiB / 1.50 | 7168 MiB / 3.00 |

The operator should choose based on simultaneous work, not just account count:
mail volume, open documents and spreadsheet size, simultaneous call participants,
and whether services are external. More RAM provides headroom; these overrides
do not automatically change scan concurrency, database tuning or Go heap limits.
A higher tier is not a claim of a specific supported user count.
Local model hosting and call recording are not included. The host allowance
covers Linux/Docker and an existing HTTPS proxy, not a colocated deployment
control panel and its own database.

## Standalone selection

Keep the same Compose project name and private `.env` used for the installation.
Configure the required public origins and image digest as described in
[compact deployment](compact-deployment.md). Minimum uses the base file alone.
For Standard:

```
docker compose --env-file .env -f deploy/compose.yaml \
  -f deploy/profiles/standard.compose.json up -d --build
```

For Performance:

```
docker compose --env-file .env -f deploy/compose.yaml \
  -f deploy/profiles/performance.compose.json up -d --build
```

Compose accepts the JSON override files directly. Always supply the same override
for future changes. Back up first, use a maintenance window, and never use
`down -v`. Changing a limit can recreate a container and interrupt calls or
editing. Measure actual load and leave headroom before reducing a profile.

## Līdza Deploy integration

`deploy/lidza.stack.json` follows Līdza Deploy's published version-1 stack contract
(`feat/stack-profiles`, bootstrap contract checkpoint `4cc4754`). Select `minimum`, `standard`
or `performance` through Deploy's `runtime_profile` setting. Require the agent's
settings response to advertise `stack_profiles_supported: true`; older agents do
not support this manifest. It is separate from the standalone Compose files.
The committed Collabora pin is the ARM64 image exercised in the demo. Before
using an x86-64 host, select and test the matching official immutable image
and update the office Dockerfile pin; this ARM64 acceptance is not x86-64 evidence.
The manifest deliberately contains three services: Deploy attaches its managed
PostgreSQL as the fourth container. Do not deploy Compose's PostgreSQL alongside it.

A private installation `LIDZA_MASTER_KEY` is required. For a fresh installation,
generate a 32-byte hex key and back it up separately. Upgrades must retain the
existing key. Installation-specific `config/credentials.yml.enc` files stay local;
Thura does not ship the original scaffold ciphertext. Deploy generates the
missing setup, mail-learning, meeting and document secrets in encrypted state.
The manifest's `configuration` declarations require the master key before
deployment, delegate `MAIL_PROVIDER` and `MAIL_FROM` to Thura's setup wizard,
and mark `LLM_PROVIDER` as optional. Operators do not need to configure a mail
provider or AI provider in Deploy merely to start the wizard. AI remains disabled
until configured and enabled in Thura. Use an agent and control panel containing
Lidza Deploy PR #20's configuration contract; older agents reject the new field.
Declared setup ownership never unlocks ordinary application APIs or bypasses
published-configuration validation and genuine readiness after setup.
Thura seeds its encrypted first-run wizard draft from catalogued operator
settings, keeping managed storage and service addresses consistent with the
published setup. Private HTTP service origins are explicitly allowlisted through
`THURA_SERVICE_HTTP_ORIGINS`; public app and meeting URLs still require
HTTPS/WSS. The wizard keeps its real mail and storage probes and environment
conflict checks. The explicit `bootstrap_readiness` declaration exposes only a healthy first-run
installation whose genuine `/readyz` returns the setup-required 503 marker.
Deploy shows **Setup required** while Thura's API gate remains locked. Complete
the wizard, publish, then reload through Deploy; normal `/readyz` must then pass.
Established deployments cannot use this fallback for failures.

The integration must attach the existing managed `DATABASE_URL` and its private
network instead of creating Thura's standalone PostgreSQL service. Profile
budgets include the managed database allocation. Do not silently shrink or tune
an existing/shared database to fit Minimum: report its real allocation and
require an appropriate profile or an explicit database migration.

Stable persistent resources are app storage, Postfix queue, Rspamd state,
ClamAV signatures and Valkey learning. There must never be two active writers
for a learning or mail volume. Failed updates must preserve volumes and recover
the previous application stack. Public mail/media ports need conflict checks;
private Rspamd/controller, antivirus, cache and document ports stay private.

An installation that supplies its own sealed credentials must supply its existing
`LIDZA_MASTER_KEY`. A new random key cannot decrypt an existing
credential file. No original key, provider credential or generated secret belongs
in this catalog or runtime manifest.

## Checks

```
python3 -m unittest discover -s deploy/profiles
```

The checks resolve the actual Compose files, verify the selected resource limits,
CPU totals and host allowance, and ensure the stack still has four services with
the intended private cache/realtime wiring. They do not replace a real deployment
through Līdza Deploy. The actual minimum-profile deployment passed production setup, document saves,
mail learning and rollback in a 2-CPU / 4-GiB Linux VM. See the
[acceptance report](deploy-stack-acceptance.md) for measured resources,
reproduction and the remaining deployment-specific checks.
