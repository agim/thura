# Experimental SMIP in Thura

SMIP is a separate opt-in track beyond the completed first release. The app supports paired-server chat/file packets, their review in Chat, and explicit file import into Drive. It also sends workspace-authorized chat and immutable Drive file copies through durable jobs. Matrix conversations remain separate. The standalone reference sender outbox can target this endpoint using `smip.NewApplicationClient`.

## Operator configuration

Default: `SMIP_ENABLED=false`. No keys are generated silently and no trusted peers are discovered from packets. When disabled, the peer endpoint returns 503 and the review panel is absent. Authenticated workspace APIs remain scoped to current membership.

Set `SMIP_ENABLED=true` and store `SMIP_CONFIG` through Līdza's sealed credentials. The JSON object has `domain` (logical lowercase DNS identity), `seed` (canonical unpadded base64url of 32 random Ed25519 seed bytes), `notBefore` / `notAfter` (Unix signing interval), and `peers` (domain → key-ID → trusted public key). Optional `origins` maps sending peer domains to explicit HTTPS origins; an absent origin makes that peer receive-only. Optional `originKeys` retains local historical public signing keys and revocation flags for queued packets; the current signing key is added automatically if omitted. Optional `caFile` supplies a private operator-selected PEM trust bundle added to system roots, useful for explicitly paired private deployments; certificate verification is never disabled. A trusted public key object uses `Public` (standard padded base64 of 32 public bytes), `NotBefore`, `NotAfter` and `Revoked`, matching `smip.Key`. Key IDs are SHA-256 fingerprints. Never use the public test seeds for deployments. Compare peer fingerprints through an authenticated operator channel.

The process freezes this configuration at startup. Invalid enabled configuration prevents startup. Restart every receiving node to apply rotation/revocation; drain old nodes so their previous trust cannot admit further packets. Retain necessary historical public keys for reconciliation. Workspace consent is separate and takes effect transactionally without restarting.

The app endpoint is `POST /api/v1/smip/v0.1/messages`; it accepts only direct TLS 1.3 and the strict SMIP JSON wire format. Use Līdza's operator-selected `LIDZA_TLS_DOMAINS` HTTPS listener, or an equivalent deployment that preserves a verified TLS connection to the handler. Ordinary HTTP or TLS termination followed by plaintext proxying is rejected. Forwarded headers never substitute for TLS state. TLS certificates must verify for the explicitly paired HTTPS origin; origins may differ from logical domains. `NewApplicationClient` selects this path, while `NewClient` retains the standalone reference path. No automatic path or transport fallback occurs.

Apply deployment rate limits, connection limits, timeouts, clock synchronization and private backup/retention policy. Admission is capped at two concurrent requests per receiving process. No public deployment is implied by enabling local fixtures.

Use one logical SMIP identity and consistent frozen configuration across every app/worker node sharing a database. Distinct servers use distinct deployment databases.

## Workspace consent and review

An owner/admin authorizes a binding using the operator-configured peer domain, remote sender, local service-recipient address, and stream. This is consent to receive into the entire local workspace, not a private direct conversation. Every workspace member can review accepted content. A remote server's sender assertion does not prove a human identity. Local and remote workspace IDs never serve as bearer permissions.

A binding's ID is a caller-allocated UUID used for idempotent creation. Reusing it with different content returns 409. One `(peer, stream, recipient)` mapping can belong to only one workspace; at most 50 retained bindings exist per workspace. Disable is irreversible through this API; it preserves consent history and existing receipts rather than deleting deduplication state. A later authorization uses a different stream. New admissions recheck the binding under the workspace lock. A revoked peer key still blocks duplicates even when a durable receipt exists.

Accepted packets and their receipts/public verification keys commit together in Postgres before a successful response. The receiver uses `(origin, message ID)` for deduplication, preserves original receipt bytes after restart and refuses changed content under the same ID. Each workspace retains at most 1,000 records and 64 MiB of decoded content, including imported files. At capacity it returns 503; operators must address retention without destroying idempotency history. There is no automatic deletion policy.

Chat review renders plain text. Inbox pages contain at most 50 metadata/text rows, ordered by UUID; cursors mark live positions, not snapshots or permissions. File payloads and key configuration never appear in list responses. The signed packet remains server-readable in private Postgres storage; backup encryption is the operator's responsibility.

## Explicit Drive import

A file packet may contain at most 10 MiB, matching the app's Drive limit. Names must already satisfy Drive's filename policy; traversal/path-like names are refused. Files are opaque `application/octet-stream` downloads, not executable previews. This adapter does not provide malware scanning.

A member explicitly selects a local folder or Drive root and imports an accepted file. The transaction rechecks current membership, file integrity, folder access, retained/reserved quota and the file-count limit. It writes immutable bytes through the storage pack, then commits the version, source-transfer marker and audit event together. Concurrent retries return the same file/version. A trashed/purged prior import is not silently recreated. Failed/ambiguous commits leave any unreferenced private object for ordinary Drive sweeping after its grace period; immediate deletion could destroy a committed object.

Transport acceptance, import completion and human reading are different states. Disabling a binding does not erase accepted workspace content. Removing workspace membership revokes review/import access. No automatic fallback to SMTP, Matrix or a new message ID occurs.

## App API

All local operations use Līdza authentication and generated client methods:

| Route under `/api/v1/workspaces/{workspaceId}/smip` | Permission / result |
| --- | --- |
| `GET /bindings` | Current member; consent metadata and enabled capability. |
| `POST /bindings` | Current owner/admin; idempotent authorization, audited in transaction. |
| `DELETE /bindings/{id}` | Current owner/admin; disable new admissions, audited in transaction. |
| `POST /outbox` | Current member; queue chat or a verified immutable Drive snapshot and its job atomically. |
| `GET /outbox?cursor=…` | Current member; bounded delivery metadata without signed payloads or keys. |
| `POST /outbox/{id}/cancel` | Current owner/admin; idempotently cancel a pending/blocked packet before any network attempt, audited atomically. |
| `POST /outbox/{id}/resume` | Current owner/admin; resume a blocked original packet within the retry budget. |
| `GET /inbox?cursor=…` | Current member; bounded text/metadata without file payloads. |
| `POST /inbox/{id}/import` | Current member; explicit Drive import with optional local `folderId`. |

The Chat panel appears only for enabled deployments. Owners/admins manage bindings there; members review content and import files. The screen labels the feature experimental and distinguishes queued, cancelled, uncertain, blocked, accepted and expired sends from incoming acceptance and Drive import. Sending controls appear only for enabled bindings with an operator-configured outgoing endpoint.

## Durable app sending

Both operators authorize matching streams and service addresses. The sender uses the binding in reverse: its local recipient becomes the signed sender, the remote sender becomes the packet recipient, and the stream remains unchanged. These are workspace service identities, not a claim that a remote human account authored the message. A server signature never substitutes for local membership checks.

`POST /outbox` takes a caller-generated UUID `transactionId`, `bindingId`, and either `body` or `fileId`. It checks current membership under the workspace lock, enabled consent, the configured endpoint, signing-key validity and retained queue capacity. File copies read and verify the authorized current Drive version before signing. The signed packet and durable job commit together. Retrying the same transaction preserves its original intent; changed content or another actor under that ID returns 409. Changing or trashing the original file does not change a queued snapshot.

A worker rechecks the originating member and binding immediately before authorizing an attempt. Revoked membership, withdrawn consent, changed mappings, unavailable origin keys and missing endpoints pause attempts before network contact. A previously authorized in-flight transmission cannot be recalled; removal is not a claim that earlier attempts never reached the peer.

The worker commits an uncertain attempt marker before HTTP, with a unique attempt token and a crash lease longer than its 30-second HTTP lifetime. A verified signed receipt commits separately as acceptance. A failed/ambiguous local receipt commit leaves the original attempt uncertain; retry recovers the peer's original receipt. Concurrent workers converge on one active attempt, and final writes reject superseded attempt tokens. No job creates a new packet, re-signs it or switches transports.

Worker and reconciliation job kinds are scoped to the logical origin domain; disabled deployments register no SMIP workers. This keeps another identity or a disabled node from claiming pending sends. A recurring reconciliation job queues at most 50 due records per run using unique job keys. Retry uses jittered exponential backoff capped at 150–300 seconds, with 32 network attempts per packet. The schedule runs once per minute, so actual dispatch may be later than the retry timestamp. Permanent HTTP failures pause as blocked; transport errors and invalid receipts remain uncertain. Owners/admins may explicitly resume a blocked packet while its attempt budget remains. Exhausted attempts require operator reconciliation with the peer rather than a fresh automatic ID. A never-attempted packet expires after seven days and is definitely unsent by this outbox; previously attempted packets can still recover an older receipt after expiry.

The retained outbox has its own 1,000-record / 64 MiB decoded-content limit per workspace, including accepted packets. It stores packet/receipt bytes and public verification keys, never private keys; no automatic archive/deletion policy exists. Backups include the database queue and receipts. Key rotation must retain unrevoked historical origin and peer receipt keys needed for reconciliation. Resume keeps the original authorization actor, ID, payload, signature and attempt history. It does not restore removed membership or withdrawn consent.

The browser retains a failed queue request ID while retrying unchanged input, clears it after a confirmed queue response, and displays server delivery states. Accepted means a verified durable transport receipt; reading and remote Drive import remain unconfirmed. The paired-server contract remains experimental, server-readable and default-disabled. It does not provide ordered conversations, reply/group semantics, malware scanning, public discovery, E2EE, Matrix bridging or SMTP fallback.

## Cancelling an unattempted send

Owners/admins can cancel an individual pending or blocked send only while its durable attempt count is zero. Cancellation and dispatch take the same workspace/row locks: a successful cancellation guarantees this outbox never attempted that packet; if dispatch has already committed an attempt, cancellation returns 409. In-flight, uncertain, accepted and previously attempted blocked sends cannot be recalled. The UI offers cancellation only for eligible sends and always relies on the server to resolve races.

Cancellation is a local terminal state, not a new wire event or remote recall command. The audit event and state commit together; an audit failure rolls both back. A repeated cancellation returns the same record without another audit event, and retrying the original queue request cannot resurrect it. Existing jobs safely do nothing and reconciliation excludes cancelled packets. The signed packet, original transaction ID and retained quota accounting remain intact; cancellation does not delete transfer history or reclaim retention capacity. Current workspace authority is required even for retries.
