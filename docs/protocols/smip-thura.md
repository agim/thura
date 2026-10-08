# Experimental SMIP in Thura

SMIP is a separate opt-in track beyond the completed first release. The app currently supports receiving paired-server chat/file packets, reviewing them in Chat, and explicitly importing files into Drive. It does not yet send through app jobs or replace Matrix conversations. The standalone reference sender outbox can target this endpoint using `smip.NewApplicationClient`.

## Operator configuration

Default: `SMIP_ENABLED=false`. No keys are generated silently and no trusted peers are discovered from packets. When disabled, the peer endpoint returns 503 and the review panel is absent. Authenticated workspace APIs remain scoped to current membership.

Set `SMIP_ENABLED=true` and store `SMIP_CONFIG` through Līdza's sealed credentials. The JSON object has `domain` (logical lowercase DNS identity), `seed` (canonical unpadded base64url of 32 random Ed25519 seed bytes), `notBefore` / `notAfter` (Unix signing interval), and `peers` (domain → key-ID → trusted public key). A trusted public key object uses `Public` (standard padded base64 of 32 public bytes), `NotBefore`, `NotAfter` and `Revoked`, matching `smip.Key`. Key IDs are SHA-256 fingerprints. Never use the public test seeds for deployments. Compare peer fingerprints through an authenticated operator channel.

The process freezes this configuration at startup. Invalid enabled configuration prevents startup. Restart every receiving node to apply rotation/revocation; drain old nodes so their previous trust cannot admit further packets. Retain necessary historical public keys for reconciliation. Workspace consent is separate and takes effect transactionally without restarting.

The app endpoint is `POST /api/v1/smip/v0.1/messages`; it accepts only direct TLS 1.3 and the strict SMIP JSON wire format. Use Līdza's operator-selected `LIDZA_TLS_DOMAINS` HTTPS listener, or an equivalent deployment that preserves a verified TLS connection to the handler. Ordinary HTTP or TLS termination followed by plaintext proxying is rejected. Forwarded headers never substitute for TLS state. TLS certificates must verify for the explicitly paired HTTPS origin; origins may differ from logical domains. `NewApplicationClient` selects this path, while `NewClient` retains the standalone reference path. No automatic path or transport fallback occurs.

Apply deployment rate limits, connection limits, timeouts, clock synchronization and private backup/retention policy. Admission is capped at two concurrent requests per receiving process. No public deployment is implied by enabling local fixtures.

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
| `GET /inbox?cursor=…` | Current member; bounded text/metadata without file payloads. |
| `POST /inbox/{id}/import` | Current member; explicit Drive import with optional local `folderId`. |

The Chat panel appears only for enabled deployments. Owners/admins manage bindings there; members review content and import files. The screen labels the feature experimental and explicitly states that app sending is unavailable.
