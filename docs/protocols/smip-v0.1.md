# SMIP/0.1: server chat and file transport

Status: experimental reference profile, implemented in [internal/smip](../../internal/smip). This profile is independently identified as `smip/0.1`; incompatible changes require a new version and endpoint. It is not mounted in Thura, enabled as a Chat/Drive provider, or an SMTP replacement. The developer's requirement is chat and sending/receiving files between two servers. This document turns that requirement into a testable first transport; the uploaded transcript is research, not a binding specification.

## Guarantees and trust

Two explicitly paired operators configure the logical peer domain, HTTPS origin, allowed public signing keys, key validity intervals and recipient/stream policy. Each server authenticates the other's server signature. A sender's own application must authorize the local person and file before signing; the receiver independently authorizes the destination and conversation. A signature from a peer does not grant workspace membership or prove that an individual human authored the content.

TLS 1.3 protects the direct connection. Ed25519 binds every envelope field and payload; a signed receipt binds the receiver, sender, ID and envelope digest. The receiving server can read the content. The disk reference stores plaintext represented as base64, with private directory/file permissions. It is neither user-to-user end-to-end encryption nor encrypted storage. Rotation of signing keys does not itself provide forward secrecy. No claim of spam elimination, automatic global reputation, or verified domain discovery is made.

Attackers may alter/replay packets, substitute recipients or receipts, send large/malformed requests, interrupt responses, or present unknown/revoked keys. Compromised authorized servers can sign malicious content, retain copies and misstate human activity; local policy, quotas, moderation and user-visible trust remain necessary. Local filesystem owners, compromised endpoints and malicious TLS trust roots are outside this reference's protection.

## Pairing and keys

- Domains are lowercase ASCII DNS names with at least two labels; IDNs use their canonical A-label form. IP addresses and single-label names are not protocol identities.
- Endpoints are explicit HTTPS origins. They may differ from the logical domain, but pairing must establish that association independently. Operators compare key fingerprints through an authenticated channel. TLS certificates must verify for the configured endpoint.
- Key IDs are lowercase SHA-256 hex digests of the raw 32-byte Ed25519 public key. Private keys remain sealed operator credentials; fixture seeds are publicly known test material.
- Each trusted key has a `notBefore` inclusive / `notAfter` exclusive signing interval, expressed in UTC Unix seconds, and an explicit revocation flag. Envelope `created` must fall in that interval. Public keys may remain available after the signing interval so in-flight messages can be verified until expiry.
- Operators can rotate keys with an overlap (30 days is an operational proposal, not a security guarantee). `Receiver.SetPeer` atomically replaces a peer's trust set after in-flight admissions complete; `SetSigningKey` rotates receipt signing. Construct a new `Client` with the updated receipt key set and close the old client's idle connections.
- Preserve old public receipt keys while corresponding outbox records may need reconciliation. Revocation rejects that key, including duplicate requests; it can make an old acknowledgement unverifiable. Surface that as an uncertain outcome rather than silently redelivering under a new ID.
- The reference performs no origin callback, DNS fetch, automatic key download or trust-on-first-use. A packet cannot cause arbitrary outbound discovery requests. Public discovery, domain-paired payload encryption, HPKE and user/device key management need separate profiles and review.

The reference uses standard [Ed25519 (RFC 8032)](https://www.rfc-editor.org/rfc/rfc8032) and [TLS 1.3 (RFC 8446)](https://www.rfc-editor.org/rfc/rfc8446). A later payload-encryption profile should use a reviewed implementation of an established construction, such as [HPKE (RFC 9180)](https://www.rfc-editor.org/rfc/rfc9180), rather than designing cryptographic primitives from the transcript.

## Wire envelope

`POST /smip/v0.1/messages`, with `Content-Type: application/json`, over TLS 1.3. No redirects, transparent relay, query options or alternate transport negotiation.

```json
{"envelope":{"version":"smip/0.1","id":"opaque-unique-id","from":"a.example","to":"b.example","sender":"alice@a.example","recipient":"bob@b.example","stream":"conversation-1","kind":"chat","created":1800000000,"expires":1800003600,"keyId":"<64 lowercase hex characters>","name":"","payload":"aGVsbG8"},"signature":"<unpadded base64url Ed25519 signature>"}
```

The example is schematic; [the public file vector](../../internal/smip/testdata/file-v0.1.json) contains valid keys/signatures.

| Field | Contract |
| --- | --- |
| `version` | Exactly `smip/0.1`. |
| `id` | Sender-domain-unique, immutable delivery ID; 1–128 ASCII letters, digits, `.`, `_` or `-`. Applications should allocate cryptographically random UUIDs. Uniqueness is across destinations, kinds and key rotations within the origin domain. |
| `from`, `to` | Logical server domains; at most 253 characters, with DNS label limits. |
| `sender`, `recipient` | One address each, bound to `from` and `to` respectively. Local identifiers use the ID alphabet and maximum length above. No multiple recipients or implicit plus addressing. |
| `stream` | Opaque conversation/transfer context, 1–128 characters using the ID alphabet. Both applications must explicitly authorize its meaning. It is not a bearer capability. |
| `kind` | `chat` or `file`. |
| `created`, `expires` | Positive integer Unix seconds smaller than 2^53. Expiry follows creation by at most seven days. A new packet may be at most 120 seconds in the receiver's future and must not be expired. |
| `keyId` | Origin signing-key fingerprint. |
| `name` | Empty for chat. For files, unpadded base64url of a nonempty UTF-8 display name, at most 255 decoded bytes, without NUL. Never interpret it as a filesystem path or HTML. |
| `payload` | Unpadded canonical base64url. Chat: UTF-8 plain text, at most 64 KiB decoded. File: arbitrary bytes, including an empty file, at most 16 MiB decoded. No remote URL fetches. |
| `signature` | Unpadded canonical base64url of the 64-byte Ed25519 signature. |

A file is an immutable byte snapshot. This profile does not offer resumable transfer, file editing, revocable copies or streaming media. A recipient may retain an accepted copy after the sender removes access to its original.

### Canonical bytes

The signing bytes are the ASCII prefix `SMIP-ENVELOPE` followed by one LF, followed by this compact JSON array in exactly this order:

```text
[version,id,from,to,sender,recipient,stream,kind,created,expires,keyId,name,payload]
```

All string values use the restricted ASCII alphabets above, and integers use ordinary decimal notation without a fraction/exponent. There is no whitespace or alternative escaping. File names and message text are base64url precisely so their UTF-8 representation cannot introduce cross-language JSON escaping differences. The envelope digest is SHA-256 hex of these signing bytes, excluding the signature itself.

The wire object is also compact, in the field order shown in the example, with envelope before signature. Unknown or duplicate fields, trailing data, alternate escaping, pretty printing and noncanonical base64 are rejected. This is a deliberately narrow profile, not a claim to implement general JSON canonicalization or HTTP Message Signatures. The public vector and independent Node verifier establish its exact representation.

## Durable receipt and retry

```text
Receipt fields: version,id,from,to,digest,accepted,keyId,signature
Receipt signing bytes:
SMIP-RECEIPT<LF>[version,id,from,to,digest,accepted,keyId]
```

Receipt `from` is the receiving domain and `to` the sending domain. `digest` binds the complete signed envelope. `accepted` is the first durable acceptance time: within the envelope's validity interval, allowing the same 120-second future skew. Receipt signing and key IDs use Ed25519 and SHA-256 as above. Verify the receiver's pinned key, signing interval, signature, message ID, both domains and digest before recording acknowledgement.

`accepted` means the server durably stored the payload and receipt. It does not mean a person received/read a chat, a file was imported into Drive, malware scanning passed, or an application side effect completed.

The receiver verifies authentication and destination before consulting its inbox. Idempotency is keyed by `(origin domain, message ID)`:

1. A known ID with the identical envelope digest returns its original signed receipt, even if the message has since expired or local recipient policy changed. It performs no new delivery. A revoked origin key is still rejected.
2. A known ID with different signed content returns 409; there is no overwrite.
3. A new ID must pass expiry, local policy and current receipt-key checks. Its payload and receipt are committed together before sending 201.
4. Concurrent identical admissions converge on one record. The duplicate receives 200 and the same receipt bytes.
5. Storage failures return 503 and no successful receipt. A lost response is uncertain: retry the original packet unchanged.

The sender application must durably save the signed packet before its first attempt. This reference does not yet implement a Thura job/outbox adapter. Use bounded exponential backoff with jitter and operator limits. Preserve the original ID, timestamps, signing key and bytes; do not re-sign/recreate messages on retry. Network errors, invalid/unverifiable receipts and lost acknowledgements are uncertain outcomes. HTTP 429/500/502/503/504 are classified retryable by `HTTPError`; 400/401/403/409/413/415/422 require correction, policy/credential action or a terminal rejection. Never silently switch an uncertain SMIP delivery to SMTP or Matrix.

Original sender availability is irrelevant to receipt verification. A restart can recover the original receipt from the receiver inbox. No sender boolean callback substitutes for content authentication.

## Reference storage and hosting

`FileInbox` is a Unix reference backend, separate from Thura's database and storage packs. Prepare its parent directory first. It creates a private 0700 directory, acquires a lifetime OS `flock`, writes 0600 temporary records, synchronizes them, atomically renames them, and synchronizes the directory before acknowledgement. The parent directory entry is synchronized at opening. Process death releases the lock. Retries synchronize existing records too, so an earlier uncertain synchronization is not silently treated as durable. Record paths use hashes of origin/ID; user names never form paths.

Use a trusted local filesystem with working atomic rename, flock and fsync; network filesystems are not qualified. Corrupt records fail closed. A process-exit test proves restart behavior, not power-loss behavior on every filesystem. Content and historical receipts are retained; this reference has no automated retention/garbage-collection policy. A production store needs transactional tombstones/digests, quotas, recovery and policy reviewed before deleting deduplication history.

The handler caps bodies at `MaxWire` (base64 expansion plus 4096 bytes) and admits at most two concurrent requests. Busy requests return 503 with `Retry-After: 1`. Host it with TLS 1.3, `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, bounded connection counts and per-peer abuse/rate controls. The admission policy is a decision function without side effects and must not mutate receiver configuration while called. These are host responsibilities; do not expose a default-timeout server publicly. A deployment must also configure credentials, clock synchronization, quotas and audit logging without payloads/secrets. The client verifies certificates, enforces TLS 1.3, refuses redirects and bounds response bodies and request time.

## Tests and adapter boundary

```sh
. /workspace/.lidza-tools/env
cd /workspace/thura
go test -race ./internal/smip
node scripts/smip-vectors.mjs
```

Tests exercise two independently configured TLS servers, bidirectional chat/binary files, exact 16 MiB file transfer, malformed/oversize packets, recipient/stream denial, tampering and key revocation, key rotation/historical receipts, conflicting IDs, concurrent storage, lost acknowledgements, restart/process death, unavailable/corrupt storage, backpressure, TLS/redirect policy and forged acknowledgements. The Node verifier independently reconstructs canonical bytes and verifies both signatures and payload bytes using Node crypto.

Thura integration is a separate change: a sealed operator capability, durable sender jobs, current-member authorization before signing, recipient/stream mapping, transactional `(origin,id)` import, safe plain-text rendering, file size/quota/checksum/scanning checks through the storage pack, explicit moderation, and user-visible pending/accepted/imported/read states. It must neither trust a remote workspace ID nor grant membership from a peer signature. Import-time membership checks remain necessary even after transport acceptance. Existing Matrix remains the operational chat provider until that adapter passes its own acceptance suite.
