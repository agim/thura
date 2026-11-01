# Experimental SMIP in Thura

SMIP is a separate opt-in track beyond the completed first release. Chat and file packets (SMIP/0.1) travel between servers that trust each other the way SMIP email does: by **domain discovery** (`_smip.<domain>`, a signed identity fetched from `https://smip.<domain>` over verified TLS 1.3, sticky downgrade protection). No operator-to-operator pairing is needed. Trusting a server only answers "is this really partner.example"; whether a message or a file reaches anyone is decided by the **workspace's sharing policy** and the **recipient's own acceptance**. Pinning a partner by fingerprint stays available for private deployments and extra assurance. Matrix conversations remain separate. The standalone reference sender outbox can target this endpoint using `smip.NewApplicationClient`.

## Operator configuration

Default: `SMIP_ENABLED=false`. No keys are generated silently. When disabled, the peer endpoint returns 503, Chat shows no external section and Settings → External sharing says it is unavailable. Authenticated workspace APIs remain scoped to current membership.

Set `SMIP_ENABLED=true`; the setup wizard generates `SMIP_CONFIG` (the local identity) when the server address is saved and stores it sealed. Operators may also supply the JSON themselves: `domain` (logical lowercase DNS identity), `seed` (canonical unpadded base64url of 32 random Ed25519 seed bytes), `notBefore` / `notAfter` (Unix signing interval), and optionally `peers` (domain → key-ID → trusted public key) with `origins` (sending peer domains → explicit HTTPS origins; an absent origin makes that peer receive-only), `originKeys` (local historical public signing keys and revocation flags for queued packets; the current signing key is added automatically if omitted) and `caFile` (a path to a private PEM trust bundle added to system roots; certificate verification is never disabled). The CA bundle is normally saved on the admin page instead (below); the JSON `caFile` is the advanced alternative. A trusted public key object uses `Public` (standard padded base64 of 32 public bytes), `NotBefore`, `NotAfter` and `Revoked`, matching `smip.Key`. Key IDs are SHA-256 fingerprints. Never use the public test seeds for deployments.

The identity and the JSON peers freeze at startup; invalid enabled configuration prevents startup. Only local key rotation (`originKeys`) still needs the JSON and a restart.

### Advertise the domain

Partners find this server exactly as SMIP email partners do ([SMIP email](../smip-mail.md#enable-and-advertise)): publish `_smip.<domain> TXT "v=SMIP1"` for the SMIP/0.1 identity domain and point `smip.<domain>` at Thura with a publicly trusted certificate and **direct TLS 1.3** ([transport hostnames](../smip-mail.md#transport-hostnames)). One record serves both profiles: each publishes its own signed document on that origin, `GET /api/v1/smip/v0.2/capabilities` for mail and `GET /api/v1/smip/v0.1/identity` for chat/files, and chat/file packets arrive at `POST /api/v1/smip/v0.1/messages` on the same origin. When the chat/file identity and the mail identity use the same domain (the usual setup), nothing extra is published.

## Server trust by discovery

A chat/file partner domain is trusted once it verifies through discovery, for inbound packets and for sends alike:

1. `_smip.<domain>` must hold exactly one `v=SMIP1` (an operator-pinned origin replaces DNS, see below).
2. Thura fetches `https://smip.<domain>/api/v1/smip/v0.1/identity` through the same SSRF-safe client as mail discovery: TLS 1.3 only, no redirects or proxies, bounded body and time, one DNS resolution dialled directly, public addresses only unless the origin was pinned by an operator.
3. The document (`smip-pairing/1`) must be canonical JSON for exactly that domain and profile, unexpired (at most five minutes), and signed by one of its own current, unrevoked keys.

The result is kept in the registry SMIP email uses (`smip_mail_domain`, profile `smip/0.1`, scoped by the local identity), with the same rules, because it is the same code (`smipmail.Registry`): a verified document is reused until it expires; a definitive absence is cached for five minutes; failures retry after fifteen seconds; capacity is 1,000 live entries per local identity and profile, evicting expired never-verified ones first. **Key continuity** follows the mail rules exactly: the trusted keys are those of the current verified document, so a rotated key is accepted only once a newly fetched, valid, signed document publishes it (publish the new key at least one cache interval before signing with it, and keep the old key in `originKeys` for in-flight packets). A key the document marks revoked is refused.

**Downgrade protection**: a domain that verified once is marked `everVerified` and never reads as "does not support SMIP" again. If its record disappears, its signature stops verifying, TLS fails or the endpoint errors, inbound packets get **503** (held, the sender retries) and sends are held with `peer_unverified`, never sent elsewhere. A domain that never verified and publishes no record is unknown: its packets get **401** and starting a chat with it says "partner.example doesn't support secure delivery". A packet naming its sending domain triggers at most one bounded, coalesced discovery per cache interval, as in mail; it never supplies keys itself.

The registry is visible under **Administration → SMIP domains** (formerly SMIP email domains) with a Profile column; operators must preserve it in backups, since deleting it destroys learned downgrade protection.

## Private CA bundle and pinned origins

**Administration → SMIP peers → Private deployment** holds what private deployments without public DNS or public certificates need, shared by mail and chat/files ([private deployments](../smip-mail.md#private-deployments)):

- **Pinned origins**: a partner domain and the HTTPS origin to reach it. The pinned origin replaces `_smip.<domain>` and `https://smip.<domain>` for that domain in chat/file discovery exactly as in mail discovery; the signed identity is still fetched and verified. A pinned origin may be a private address because an operator typed it.
- **Private CA bundle**: paste or upload PEM certificates only; each must parse, a private key or any other block is refused, and the bundle is bounded to 256 KiB and 100 certificates. The page shows the subject, issuer, validity and fingerprint of every certificate it trusts, and the JSON `caFile` bundles read-only with a note that they come from deployment configuration. While saved it replaces the JSON `caFile` (the system roots always stay); removing it brings the JSON bundle back.

Both are stored in the database and read on every discovery, packet admission and dispatch attempt, so a change applies on the next request on every node without a restart. Saving and removing are audited (`smip.trust.origin.*`, `smip.trust.ca.*`). Certificate verification is never disabled.

## Pinned partners (optional)

**Administration → SMIP peers** (server administrator only) keeps the fingerprint pairing flow as **pinned partners**, for private deployments without public DNS and for extra assurance. It shows this server's domain, the endpoint origin to share, its signing keys with validity and SHA-256 fingerprints, and never the seed. `GET /api/v1/smip/v0.1/identity` serves the same keys as the public, canonical, Ed25519-signed document discovery uses (five-minute lifetime, direct TLS 1.3 only, 503 while SMIP is disabled).

To pin: enter the partner's domain and, for a private deployment, its HTTPS endpoint. Thura fetches the document through the SSRF-safe client (with no endpoint it uses `https://smip.<domain>`, which must resolve to public addresses; a typed endpoint may be private but its certificate must verify). The verified document is stored as a **pending proposal** that trusts nothing. The operator reads each key's fingerprint to the partner's operator, compares it, ticks "I verified this fingerprint with the peer's operator" and confirms; only then are the keys stored as trusted, with who paired and when. The confirmed key IDs must be exactly the fetched ones, and a proposal older than an hour must be fetched again. Propose, pair, discard and revoke are audited (`smip.peer.*`).

A pinned partner **replaces discovery for its domain entirely**: its confirmed keys are the only keys accepted, its typed endpoint (if any) is where sends go, and **Revoke** refuses every key of the domain (JSON keys included) and removes its endpoint, without falling back to discovery. A revoked key is never trusted again; the partner has to rotate. Trust is read from `smip_peer` for every admitted packet and every dispatch attempt and merged with the JSON `peers`: per key ID the database wins; a key revoked in either source is revoked; a pending proposal has no effect; a domain keeps a sending endpoint only while it has an unrevoked key. Peers paired before discovery existed keep working unchanged.

Retain necessary historical public keys for reconciliation.

## Sharing policy

Owners and admins set the policy in **Settings → External sharing** (`/app/settings/external`), with choices rather than free text:

- **External chat**: Off / Only listed domains / Any verified domain.
- **External file sharing**: Off / Only listed domains / Any verified domain.
- **Listed domains**: rows added and removed one at a time, each validated as a DNS hostname (lowercased, at most 200).

The default, before an owner or admin saves anything, is **Only listed domains with an empty list** for both: nothing external is accepted or sent. Owners and admins can also **block a domain** from a chat request; a blocked domain is refused in every mode until it is unblocked on the same page. The page also shows an overview of the workspace's external conversations (addresses, state, message count, last activity; never content). Saving, blocking and unblocking are audited (`smip.policy.update`, `smip.domain.block`, `smip.domain.unblock`) in the same transaction.

The server enforces the policy under the workspace lock **before storing anything** and again **before every send attempt**. A refused inbound packet gets the protocol's refusal, **403** ("recipient or stream denied"), never an internal error; a refused send is blocked with `policy_refused`. Members get 403 from the policy routes.

## Chat requests and acceptance

A packet whose recipient is a local mailbox address (a person's private mailbox, or a shared mailbox) and that matches no legacy stream binding becomes part of an **external conversation** between that local address and the sender's address:

- The first packet from a sender opens a **chat request**. Only its recipient sees it: the owner of the private mailbox, or, for a shared address, every member (the same rule as Mail; a request belongs to nobody else, who get 404). The request in Chat shows the sender, the domain, whether it is verified (**Verified domain** through discovery, **Pinned partner**, or **Not verified now**) and a preview of what was sent.
- Until it is accepted nothing appears as a normal conversation and no notification is sent beyond the request itself. A pending request holds at most 20 packets; further packets get 403.
- **Accept** opens the conversation: later messages flow both ways. **Decline** refuses that sender from then on (403); **Mark as spam** does the same. **Block domain** (owners and admins only) blocks the whole domain for the workspace and declines the request. A member changes their mind in **Settings → External sharing → Declined senders**, where **Accept** opens the conversation again.
- Accept, decline, spam, block and conversation starts are audited (`smip.request.accept`, `smip.request.decline`, `smip.request.spam`, `smip.domain.block`, `smip.conversation.start`).

Acceptance is purely receiver-side: the receiving server stores a pending packet and acknowledges storage with its signed receipt, so the wire format is unchanged. "Accepted" on the sender's side still means "durably stored by the receiving server", not "accepted by the person". A private address whose owner has left the workspace refuses packets. A workspace holds at most 1,000 external conversations.

## Incoming files

A file packet is admitted only when the file policy allows its domain. It may contain at most 10 MiB, matching the app's Drive limit; names must already satisfy Drive's filename policy and traversal/path-like names are refused. In an accepted conversation it is an **incoming share**: the recipient chooses a Drive folder (or the root) and **Save to Drive**, or **Decline**. Saving rechecks the conversation, the file policy, file integrity, folder access, quota and the file-count limit under the workspace lock, writes immutable bytes through the storage pack and commits the version, transfer marker and audit event (`smip.file.save`) together; a retry returns the same file, and a removed saved file is not silently recreated. Declining keeps the signed record and receipt (`smip.file.decline`). Files are opaque `application/octet-stream` downloads; this adapter does not scan for malware.

## Starting a chat and sharing files

A member starts an external chat in Chat → External → New by typing an address such as `name@partner.example`. Before anything is created the server checks the chat policy ("Your organisation doesn't allow chatting with partner.example"), then the partner (a pinned partner needs an active key and an endpoint; anyone else must verify through discovery: "partner.example doesn't support secure delivery", or "can't be verified right now" for a held domain). The message comes from the member's own address at the SMIP domain (their private mailbox there); a member without one is told so. Starting the same chat again returns the same conversation; starting one with a sender they declined accepts them again.

Messages and Drive file copies go through the durable outbox below: the file policy is checked when queueing and before every attempt, a domain that does not support secure delivery is refused at once, and one that cannot be verified right now is queued and held. The first message reaches the partner's person as a chat request on their side.

## App API

All local operations use Līdza authentication and generated client methods. Routes under `/api/v1/workspaces/{workspaceId}/smip`:

| Route | Permission / result |
| --- | --- |
| `GET /policy`, `PUT /policy` | Current owner/admin (403 for members); the policy, domain list, blocked domains and conversation overview; saving is audited. |
| `POST /policy/unblock` | Current owner/admin; unblock a domain, audited. |
| `GET /conversations` | Any member: their own requests and conversations and those of shared addresses, what the policy allows and their own external address. |
| `POST /conversations` | Any member: start an external chat after the policy and discovery checks; idempotent per address pair. |
| `GET /conversations/{id}` | The recipient only (404 to others): the conversation and its messages in both directions. |
| `POST /conversations/{id}/accept`, `/decline`, `/spam` | The recipient only; audited. |
| `POST /conversations/{id}/block` | The recipient, if an owner or admin (403 otherwise); blocks the domain and declines. |
| `POST /conversations/{id}/messages` | The recipient, in an accepted conversation; queue chat (`body`) or a Drive file copy (`fileId`) with a caller `transactionId`. |
| `POST /conversations/{id}/files/{itemId}/save`, `/decline` | The recipient, in an accepted conversation; save an incoming file to Drive (optional `folderId`) or decline it. |
| `GET /bindings`, `POST /bindings`, `DELETE /bindings/{id}` | Current owner/admin; legacy stream bindings (below). |
| `POST /outbox`, `GET /outbox`, `POST /outbox/{id}/cancel`, `POST /outbox/{id}/resume` | Current owner/admin; legacy stream sends (below). Conversation sends are not listed here. |
| `GET /inbox`, `POST /inbox/{id}/import` | Current owner/admin; legacy stream review and import. A conversation's packets are never listed or importable here. |

## Legacy stream bindings

Before discovery, a workspace owner or admin authorized a binding of an operator-paired peer domain, remote sender, local service-recipient address and stream, as consent to receive into the whole workspace. Existing bindings keep working, as an explicit owner/admin allow-rule for exactly that peer, stream, sender and recipient: they count as a listed domain for their own stream, so only "Off" in the policy (for the packet's kind) or a blocked domain stops them, on receipt and before each send (`policy_refused`). New external conversations need no binding; Settings → SMIP streams no longer offers the free-text authorization form and shows the existing bindings, their review, import and sends for owners and admins. A remote server's sender assertion does not prove a human identity, and local and remote workspace IDs never serve as bearer permissions.

A binding's ID is a caller-allocated UUID used for idempotent creation. Reusing it with different content returns 409. One `(peer, stream, recipient)` mapping can belong to only one workspace; at most 50 retained bindings exist per workspace. Disable is irreversible through this API; it preserves consent history and existing receipts rather than deleting deduplication state. New admissions recheck the binding under the workspace lock. A revoked peer key still blocks duplicates even when a durable receipt exists.

## Storage and retention

Accepted packets and their receipts/public verification keys commit together in Postgres before a successful response, for bindings and conversations alike. The receiver uses `(origin, message ID)` for deduplication, preserves original receipt bytes after restart and refuses changed content under the same ID. Each workspace retains at most 1,000 records and 64 MiB of decoded content, including imported files. At capacity it returns 503; operators must address retention without destroying idempotency history. There is no automatic deletion policy.

Views render plain text. File payloads and key configuration never appear in list responses. The signed packet remains server-readable in private Postgres storage; backup encryption is the operator's responsibility.

Transport acceptance, a person's acceptance, saving to Drive and human reading are different states. Removing workspace membership revokes access to requests, conversations and files. No automatic fallback to SMTP, Matrix or a new message ID occurs.

## Legacy stream review and import

The stream review (Settings → SMIP streams, owners and admins) lists bounded text/metadata of binding packets, at most 50 rows per page ordered by UUID; cursors mark live positions, not snapshots or permissions. An owner or admin explicitly imports an accepted stream file into a chosen folder or the Drive root through the same immutable import as Save to Drive (`smip.file.import`). Disabling a binding does not erase accepted workspace content.
## Durable app sending

Conversation sends and legacy stream sends share one signed, durable outbox and dispatch job. A conversation send is signed from the conversation's local address to its remote address on the conversation's stream; a legacy stream send uses the binding in reverse (its local recipient becomes the signed sender, the remote sender the packet recipient, the stream unchanged). Addresses are the servers' assertions, not proof that a remote human authored the message. A server signature never substitutes for local membership checks.

Before every attempt of a conversation send the worker rechecks, under the workspace lock, that the requester is still a member who may see the conversation (`membership_revoked`), that it is still accepted on this side (`conversation_closed`), that the policy still allows the kind with that domain (`policy_refused`), that the packet still matches the conversation (`conversation_changed`) and the origin key (`origin_key_unavailable`), and the partner: a pinned partner's endpoint, or discovery, done before the lock is taken so no network fetch happens inside the transaction. A domain that never supports secure delivery blocks (`peer_unsupported`); one that cannot be verified right now, or lost its verification, is held without an attempt (`peer_unverified`) and retried with backoff, and a never-attempted packet still expires after seven days. A discovered partner is reached through the SSRF-safe discovery client and its receipt is verified against the keys of the same verified document.

For legacy streams, `POST /outbox` takes a caller-generated UUID `transactionId`, `bindingId`, and either `body` or `fileId`. It checks the current owner/admin role under the workspace lock, enabled consent, the configured endpoint, signing-key validity and retained queue capacity. File copies read and verify the authorized current Drive version before signing. The signed packet and durable job commit together. Retrying the same transaction preserves its original intent; changed content or another actor under that ID returns 409. Changing or trashing the original file does not change a queued snapshot.

A worker rechecks the originating member, its owner/admin role and the binding immediately before authorizing an attempt. Revoked membership (`membership_revoked`), a requester demoted below admin (`role_revoked`), withdrawn consent, changed mappings, unavailable origin keys and missing endpoints pause attempts before network contact. A previously authorized in-flight transmission cannot be recalled; removal is not a claim that earlier attempts never reached the peer.

The worker commits an uncertain attempt marker before HTTP, with a unique attempt token and a crash lease longer than its 30-second HTTP lifetime. A verified signed receipt commits separately as acceptance. A failed/ambiguous local receipt commit leaves the original attempt uncertain; retry recovers the peer's original receipt. Concurrent workers converge on one active attempt, and final writes reject superseded attempt tokens. No job creates a new packet, re-signs it or switches transports.

Worker and reconciliation job kinds are scoped to the logical origin domain; disabled deployments register no SMIP workers. This keeps another identity or a disabled node from claiming pending sends. A recurring reconciliation job queues at most 50 due records per run using unique job keys. Retry uses jittered exponential backoff capped at 150–300 seconds, with 32 network attempts per packet. The schedule runs once per minute, so actual dispatch may be later than the retry timestamp. Permanent HTTP failures pause as blocked; transport errors and invalid receipts remain uncertain. Owners/admins may explicitly resume a blocked packet while its attempt budget remains. Exhausted attempts require operator reconciliation with the peer rather than a fresh automatic ID. A never-attempted packet expires after seven days and is definitely unsent by this outbox; previously attempted packets can still recover an older receipt after expiry.

The retained outbox has its own 1,000-record / 64 MiB decoded-content limit per workspace, including accepted packets. It stores packet/receipt bytes and public verification keys, never private keys; no automatic archive/deletion policy exists. Backups include the database queue and receipts. Key rotation must retain unrevoked historical origin and peer receipt keys needed for reconciliation. Resume keeps the original authorization actor, ID, payload, signature and attempt history. It does not restore removed membership or withdrawn consent.

The browser retains a failed queue request ID while retrying unchanged input, clears it after a confirmed queue response, and displays server delivery states. Accepted means a verified durable transport receipt; the partner person's acceptance, reading and remote Drive saving remain unconfirmed. The contract remains experimental, server-readable and default-disabled. It does not provide guaranteed ordering, reply/group semantics, malware scanning, E2EE, Matrix bridging or SMTP fallback.

## Cancelling an unattempted send

Owners/admins can cancel an individual pending or blocked send only while its durable attempt count is zero. Cancellation and dispatch take the same workspace/row locks: a successful cancellation guarantees this outbox never attempted that packet; if dispatch has already committed an attempt, cancellation returns 409. In-flight, uncertain, accepted and previously attempted blocked sends cannot be recalled. The UI offers cancellation only for eligible sends and always relies on the server to resolve races.

Cancellation is a local terminal state, not a new wire event or remote recall command. The audit event and state commit together; an audit failure rolls both back. A repeated cancellation returns the same record without another audit event, and retrying the original queue request cannot resurrect it. Existing jobs safely do nothing and reconciliation excludes cancelled packets. The signed packet, original transaction ID and retained quota accounting remain intact; cancellation does not delete transfer history or reclaim retention capacity. Current workspace authority is required even for retries.
