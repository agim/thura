# Real Deploy stack acceptance

This harness drives an owned Linux Deploy agent against the actual Thura Git
branch, rather than substituting a toy app or seeding setup's test-only marker.
It uses a separate Colima VM named `thura-deploy-acceptance`, with 2 CPUs and
4 GiB RAM. It never targets the production Deploy console or existing demo
Docker context. Credentials/state remain under `.lidza/deploy-stack-acceptance`.

Prerequisites: build a Deploy v0.2.98+ agent into
`.lidza/deploy-stack-acceptance/agent-linux` (cross-compile for the VM
architecture if necessary); ensure Docker's buildx plugin is available inside the VM; create the
owned VM (`colima start thura-deploy-acceptance --activate=false --cpu 2 --memory 4`).
The agent runs inside that same VM, so its memory is included in the host
budget. The host driver uses a private Docker config and the exact owned VM
socket. Do not modify another installation's Docker configuration.

```
python3 scripts/stack-acceptance/run.py initialize
python3 scripts/stack-acceptance/run.py provision --branch main
python3 scripts/stack-acceptance/run.py deploy
python3 scripts/stack-acceptance/run.py bootstrap --core-config .lidza/deploy-stack-acceptance/core-services.json
python3 scripts/stack-acceptance/run.py smoke
python3 scripts/stack-acceptance/run.py mail-flow
```

`initialize` refuses an existing configuration. `start` resumes that owned agent.
`provision` creates a private managed PostgreSQL database and selects `minimum`.
`deploy` uses the current GitHub CLI credential privately for the source checkout.
`provision` leaves mail/AI provider settings absent to exercise genuine first-run
preflight. Current setup requires all core services. Supply a private chmod-600
JSON file with `ai`, `integrations` and `mail` objects whose values are strings:
AI provider/model/key and `THURA_AI_ENABLED=true`; Matrix URL/identity/token and
`SMIP_ENABLED=true` plus paired `SMIP_CONFIG`; and `SMIP_MAIL_ENABLED=true` with
`SMIP_MAIL_CONFIG`. Deployment-managed Collabora/LiveKit fields are preserved
unless explicitly overridden. Never commit this file or invent peer identities
to bypass core configuration. Matrix/model services must be supplied separately.
`bootstrap` claims a real operator account, persists the wizard's settings, runs
real outgoing SMTP and storage probes, publishes, and performs a coordinated
agent reload. Outgoing mail goes only to a synthetic SMTP sink inside the mail
container; it adds no service container. No external recipient or LLM is called
by bootstrap itself. Mail-flow fixtures explicitly turn off incoming AI screening
to avoid paid inference, while retaining the required AI assistant configuration.
Do not rerun bootstrap after successful publication; its one-time token is spent.

`smoke` uses the existing compact smoke source for clean/EICAR antivirus scans,
Rspamd checks, reversible/replayed learning and signature reload; it also queries
the actual managed database and checks memory/CPU caps and OOM counters. `mail-flow` delivers real reserved-domain SMTP into the mailbox, checks public
relay refusal, reports Junk and verifies durable spam learning, then restores
Inbox and verifies ham correction. Its private sink capture survives the
coordinated reload in an owned persistent test directory. Run the
real Office browser suite separately using this fixture's synthetic operator and
`BASE_URL=http://thura-stack.localhost:19881`; it uploads, edits, saves, downloads
and inspects DOCX/XLSX bytes. Loopback HTTP is specific to this isolated fixture;
production ingress needs trusted HTTPS/WSS, DNS/MX, firewall rules and externally
reachable SFU media addresses.

A successful run is functional evidence, not a simultaneous-user capacity or
mail-detection accuracy benchmark. Preserve the report and screenshots, stop the
owned agent (`python3 scripts/stack-acceptance/run.py stop`) and VM after review, and retain backed-up installation keys. When
removing test resources, verify exact names and ownership labels; never run a
broad Docker prune or remove another app's volumes.

Client safety tests:

```
python3 -m unittest discover -s scripts/stack-acceptance -v
```

Rollback retention and screenshots, after a successful setup/Office/mail run:

```
python3 scripts/stack-acceptance/rollback.py
node scripts/stack-acceptance/screens.mjs
```

Rollback compares exact object SHA-256 values, immutable version metadata,
learned feedback and canonical durable Bayes hashes, retaining the managed
database binding. Per-instance Rspamd history keys and unordered SCAN output are
not used as data-retention assertions. The initial retention fixture expects the
one DOCX and one XLSX created by the first Office run (four stored versions).
