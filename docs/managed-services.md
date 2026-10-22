# Deployment-managed services and private inboxes

The operator chooses domains, team name, storage, outgoing mail provider, AI
provider/model and policies. Deployment supplies private service addresses,
stable generated credentials and Matrix registration. The wizard groups those
services as Chat, Document editing and Meetings rather than requiring tokens.

On restart, reconciliation fills only missing deployment-owned values in the
encrypted draft and active snapshot. It preserves saved domains, provider
choices, storage location/prefix, policies, signing identities and credentials.
It never publishes an unfinished draft or marks a connection check successful.
Retain the original master key and deployment-generated secrets on updates.

## Personal mail and invitations

The first administrator owns the initial private inbox; standard operator
aliases deliver to it. New managed-team invitations reserve a private inbox
before activation mail is submitted. Send the activation link to an existing
reachable email address, rather than only to the new inaccessible inbox. Until
acceptance the reserved inbox has no owner and no team member can read it.
Acceptance assigns ownership in the same transaction as membership creation.

On upgrade, exact account-email matches become private. Existing members of
configured teams receive missing personal inboxes using their account's local
part and an active team mail domain. Address collisions receive a stable suffix;
reconciliation never takes an existing mailbox or alias from someone else.
Older ambiguous shared inboxes stay shared and need explicit operator review.
Members of teams with no mail domain receive their inbox when team mail setup
is completed. Account sign-in addresses do not change during backfill.

Private inbox ownership and current membership are checked by message readers,
attachments, drafts, sends, screening review, labels, threads and Loop/search.
Team administrators can manage mailbox metadata but cannot read another
member's private inbox through those APIs. Removing membership revokes access;
it does not delete or reassign retained mail.

## Matrix and the local assistant

The compact application container supervises Thura, LiveKit, Synapse and Ollama.
Synapse accepts only private loopback client traffic and the deployment's
registered application-service token. Public registration and federation are
disabled. Matrix is the internal conversation store; external SMIP trust is
separate. The server name is stable and cannot be changed in place on restart.

Small installations use Synapse SQLite in persistent local state. Compose keeps
its existing `matrix-state` and `model-state` volumes. Lidza-deploy uses the
platform app-storage attachment: `.thura-services/matrix` and
`.thura-services/models` beneath its `STORAGE_DIR`, including when file objects
use S3. These private paths are not Drive objects and need local backups.
Back up the state directory, including its signing key and database, during a stopped
or SQLite-consistent snapshot. Do not copy a live database file alone. Larger
deployments should plan a supported Synapse database migration and measure
capacity; the small-team profile is not a concurrency guarantee.

Ollama listens on loopback with one loaded model, one request in parallel and
no persistent loaded-model cache. Selecting Ollama defaults missing fields to
`qwen2.5:0.5b`; an explicit AI connection check downloads its approximately
398 MB weights into persistent model state. Cloud-provider choices leave the daemon idle
and do not download weights. Custom/larger models require a separately sized
service. This small assistant is not a validated phishing classifier; review
its output. The incoming-mail screening toggle and configured AI provider remain
independent of the assistant UI.

Synapse's vendor image retains its AGPL-3.0 notices; distributors must comply
with its source and notice obligations. Ollama is MIT licensed and this Qwen
checkpoint is Apache-2.0 licensed. Collabora remains subject to its own included
notices. Source references:
[Synapse](https://github.com/element-hq/synapse),
[Ollama](https://github.com/ollama/ollama),
[Qwen model card](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct).

## Publication checks and SMIP

Publication requires fresh successful checks bound to the exact saved settings:
mail acceptance, storage round trip, authenticated Matrix registration/whoami,
Collabora Word/Excel editing discovery, LiveKit room creation/deletion, one
actual AI response and the public SMIP TLS 1.3 challenge. These checks are
explicit and replay-safe. An AI test may incur the chosen provider's charge.
Discovery/API checks do not replace document-save or device-media acceptance.

Deployment generates a local SMIP chat/file signing identity without inventing
trusted peers. Cross-server exchange still requires explicit pairing (on
Administration → SMIP peers: fetch the peer's signed identity, confirm its
fingerprint with the other operator; see
[the SMIP guide](protocols/smip-thura.md#pairing-on-the-admin-page)), consent
and workspace bindings. Existing identities are retained. The separate SMIP
mail identity is retained with its domain provider configuration.

The manifest opts into the released framework v0.1.125 / Lidza-deploy
v0.2.103 protected gateway contract. Both deployment components must be current
and the host's Caddy-to-agent mTLS ingress must be configured; upgrading the panel
alone is insufficient. Original-client TLS evidence remains distinct from
backend TLS. Forwarded scheme headers never satisfy the check. See
[the rollout instructions](server-setup.md#smip-transport-behind-lidza-deploy).

## Current acceptance evidence

[The candidate runtime report](benchmarks/managed-services-candidate.json)
records real ARM64 Linux tests in a 2-vCPU/4-GiB VM. Six setup service checks
passed; the SMIP transport check correctly failed. Concurrent direct service
calls exchanged a two-user Matrix message, converted real DOCX/XLSX fixtures to
PDF, scanned a clean attachment and generated a local-model reply in 5.3 seconds.
The separate malware/learning smoke test passed, including EICAR and signature
reload. No container recorded an OOM.

The app peaked at 924 MiB of 1024 MiB, mail at 1286 MiB of 1536 MiB,
PostgreSQL at 86 MiB of 256 MiB and Office reached its 768 MiB limit with page
reclaim. That Office pressure requires real edit/save and concurrency acceptance
before promoting this budget. Conversion is not proof of editing/saving. These
measurements do not establish AMD64, sustained load, media connectivity or
production deployment acceptance. The profile remains `candidate_unverified`.

### October 21 protected gateway follow-up

Thura `2ecaac3` deployed through the real v0.2.103 agent against the existing
managed PostgreSQL attachment in the same bounded VM. A stock Caddy 2.10.2 edge
with verified private-hop mTLS returned HTTP 200 for an original TLS 1.3 setup
challenge; original TLS 1.2 with forged evidence and direct plaintext with forged
forwarding headers both returned HTTP 403. The fixture trusts its private edge
CA explicitly, without disabling certificate verification. All seven real saved
setup checks then passed in Chrome on desktop and a 390px mobile viewport, with
no horizontal overflow. The three service containers were healthy and database
bindings remained identical. [Sanitized results](benchmarks/gateway-acceptance.json).

This closes the local transport/publication-check gap. It does not establish
production host ingress, Internet SMIP delivery between paired servers, expanded
concurrency, physical-phone media, or production readiness. Production ingress
repair still requires the operator's MFA-protected root console. The isolated
fixture currently remains running with its retained private state for that work.
