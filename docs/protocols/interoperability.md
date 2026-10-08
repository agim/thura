# Thura protocol boundaries

SMIP work is isolated from app-completion work. [SMIP/0.1](smip-v0.1.md) is the new experimental chat/file reference. Existing protocols retain their documented guarantees; adapters must explicitly translate identities, permissions, identifiers and acknowledgement states. This map supplies the starting scope for “other protocols.”

| Protocol | Current role | SMIP relationship / next contract |
| --- | --- | --- |
| HTTPS / TLS 1.3 | Transport for SMIP; existing app/provider APIs use HTTPS in production. | Explicit endpoints and certificate verification. Pin peer signing keys separately; a certificate alone does not authorize a workspace recipient. No arbitrary endpoint/key fetches from messages. |
| Ed25519 | Origin envelopes and durable server receipts in the reference. | Signing intervals and revocation are explicit. They establish server-authenticated content, not individual-user authentication or E2EE. |
| Matrix | Implemented federated Chat connector and file-copy flow. | Keep Matrix room IDs, memberships, event IDs and read receipts intact. A bridge needs explicit room/peer mappings, a provenance ledger and loop suppression; a SMIP inbox receipt is not a Matrix read receipt. Do not copy encrypted Matrix content into a server-readable channel without an explicit policy. |
| SMTP, MIME, SPF/DKIM/DMARC | Implemented Debian Postfix mail ingress/outbound. | Chat/file SMIP is not a negotiated mail capability. Future gateway work must retain the original envelope/message IDs, recipient policy, MIME limits and SMTP authentication checks, expose where encryption terminates, and avoid dual-transport duplication. No automatic SMTP retirement or fallback. |
| iCalendar / iTIP / iMIP | Implemented ICS import/export, invitations, replies and recurrence. | Preserve UID, SEQUENCE, recurrence IDs and timezone semantics. A file carrying `.ics` is just a transferred file until an authorized user explicitly imports it. Transport acceptance must not create calendar invitations or RSVP automatically. |
| CalDAV | No connector in this release. | A future endpoint needs discovery, ETags/conditional writes, sync tokens, calendar ACLs, recurrence fidelity and idempotent imports. Keep it separate from SMIP chat/file transfer and the typed app API. |
| WebDAV | No connector in this release. | A future Drive adapter must map collections/resources, locks or conditional writes, versions, quota and authorization to Drive. A received SMIP file is an immutable copy, not a remote WebDAV mount or edit capability. |
| S3-compatible storage | Implemented optional object backend through Līdza storage. | Store received files through the pack after quota/checksum/authorization checks. Object keys and signed download URLs are local storage mechanisms, not federation identity or permission. No remote URL ingestion in SMIP/0.1. |
| ONLYOFFICE document protocol | Implemented optional authenticated editing/callback connector. | A transferred document never grants an edit session. Issue local authorized configuration/tickets; callbacks retain version and membership checks. |
| WebRTC / LiveKit | Implemented optional real-time media service. | SMIP may later carry an explicitly authorized invitation or meeting reference, not audio/video or a reusable room token. Media encryption/TURN/device acceptance and scoped meeting authorization remain independent. |

## Shared adapter requirements

- Remote domain/actor/resource identifiers belong in explicit mappings; never use them as local primary keys or capabilities.
- Keep `(transport, origin, remote ID)` provenance and deduplication in the same transaction as local effects. Include bridge provenance to prevent loops and preserve the source event when quoting/forwarding.
- Check current local membership and resource grants when sending and importing; distinguish transport acceptance, local import, application delivery and human read states.
- Keep durable retry/outbox records; reuse immutable transport IDs. Uncertain outcomes cannot trigger another transport automatically.
- Apply local quotas, content handling and moderation before exposing transferred data. Names are display text; no path traversal, HTML execution or implicit remote fetching.
- Preserve provider identity, receipts and errors rather than inventing a common “delivered” state with stronger guarantees than a transport supplies.

## Standards for follow-up implementations

[Matrix specification](https://spec.matrix.org/); [SMTP RFC 5321](https://www.rfc-editor.org/rfc/rfc5321); [message format RFC 5322](https://www.rfc-editor.org/rfc/rfc5322); [DKIM RFC 6376](https://www.rfc-editor.org/rfc/rfc6376); [SPF RFC 7208](https://www.rfc-editor.org/rfc/rfc7208); [DMARC RFC 7489](https://www.rfc-editor.org/rfc/rfc7489); [iCalendar RFC 5545](https://www.rfc-editor.org/rfc/rfc5545); [iTIP RFC 5546](https://www.rfc-editor.org/rfc/rfc5546); [iMIP RFC 6047](https://www.rfc-editor.org/rfc/rfc6047); [CalDAV RFC 4791](https://www.rfc-editor.org/rfc/rfc4791); [WebDAV RFC 4918](https://www.rfc-editor.org/rfc/rfc4918); [WebDAV sync RFC 6578](https://www.rfc-editor.org/rfc/rfc6578).

Public peer discovery, payload encryption, resumable file transfer, multi-recipient/group semantics, ordered/reply events, delivery/read receipts and moderation propagation require versioned designs and integration tests. They are not implied by the initial SMIP inbox implementation.
