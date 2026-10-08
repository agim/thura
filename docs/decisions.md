# Decisions

What shaped thura and why, one entry each, newest last: a pack added, Rust
chosen for a module, a dependency taken, a schema tradeoff, an
integration. Read this before working in those areas. Record yours in the
same commit as the change: `lidza decision add "Title" --why "..."`
(MCP: `lidza_decision_add`). A recipe in docs/lidza-guide.md says how
this app does something; a decision here says why it is done that way.

## Reuse the uploaded prototype behind a preview boundary

Preserve the supplied six-app interactions while making the persistence boundary explicit. Imported JavaScript stays in `src/workspace` with ESLint enabled; `allowJs` supports an incremental TypeScript conversion. New product pages are strict TypeScript and use generated API clients. Lucide React 1.52.0 supplies the prototype's icons. The preview has its own local-storage namespace and a persistent sample-data banner.

## Invite-only account and contact foundation

Use the published Līdza auth/db packs. The developer chose invite-only workspaces, workspace-owned data, and owner/admin/member roles. Public registration and external providers are disabled; local password accounts are an explicitly provisional implementation choice. Initial account/workspace creation uses operator MCP tools. Contacts are workspace-scoped in SQL and each request checks current membership, so a session cannot retain revoked access. Invitation acceptance, membership management, and production mail will follow in later slices.

## Atomic invitations and serialized role administration

Use random 256-bit invitation tokens and persist only their SHA-256 hashes. Invitation creation and transactional mail delivery jobs commit together; new invited accounts use Līdza password validation/hashing and are inserted with membership and token consumption in one transaction. Existing accounts must authenticate as the invited subject. Workspace-row locking serializes role changes, revocation, and acceptance; last-owner checks cannot race, and acceptance never overwrites an existing role. Owners manage privileged roles; administrators manage ordinary members.

## 2026-10-08: Who owns the data: Each workspace owns its data; access is limited to its members and ex…

Why: Answered in the brief interview (docs/brief.md): Each workspace owns its data; access is limited to its members and explicit grants. Every query is scoped by it; the most expensive answer to change later.

Touches: docs/brief.md

## 2026-10-08: Pack db added

Why: Persist workspace membership and contacts in Postgres; auth uses the same database.

Touches: lidza.json, packs.go

## 2026-10-08: Pack jobs added

Why: Queue transactional invitation emails and subsequent mailbox deliveries.

Touches: lidza.json, packs.go

## 2026-10-08: Pack mail added

Why: Send invitations and account recovery using the official provider boundary.

Touches: lidza.json, packs.go

## 2026-10-08: Pack storage added

Why: Store immutable inbound MIME and private mail/Drive attachments through the official local/S3 boundary.

Touches: lidza.json, packs.go

## 2026-10-08: Mailbox-specific delivery and signed inbound relay

Why: Resolve each mailbox’s sealed credential prefix inside a custom delivery job using a non-queued official Mail instance; preserve the global invitation handler. Serialize draft/send/undo transitions and verify HMAC over raw MIME before ingestion. Keep storage private, expose capture truthfully, and sandbox sanitized email HTML. Document at-least-once transport and deployment relay requirements.

## 2026-10-08: Private versioned Drive with resumable uploads and gateway shares

Why: Use 1 MB chunks and 24-hour sessions scoped to the initiating member, with a 10 MB file limit. Verify chunk/file SHA-256, serialize finalization, preserve immutable versions and reject stale replacements. Share tokens are random and hashed; downloads recheck expiry, revocation and trash through the app, optionally matching an authenticated recipient. No public object URLs or executable previews are issued.

## 2026-10-08: Optional ONLYOFFICE connector with signed and versioned saves

Why: Pin DocumentServer 9.0.4 for integration tests and expose DOCX/XLSX editing as a separately configured service. Sign editor configs and short-lived immutable source tickets, check current workspace membership at source/callback access, constrain saved-file downloads to the configured origin without redirects, and serialize callback saves with optimistic version checks and checksum-based idempotency. Community edition is AGPL-3.0; no fidelity or production-readiness claim follows from the connector alone.

## 2026-10-08: Bounded timezone-aware calendars and ICS interoperability

Why: Use compatible FullCalendar 6.1.21 standard plugins with Luxon, loaded on demand. Keep all-day dates separate from instants, retain IANA recurrence zones, bound rules/views, skip nonexistent DST occurrences before COUNT, and select first ambiguous occurrences. Use row-locked sequences and explicit exception resets. Import ICS atomically by calendar-scoped UID/sequence, preserving cancellation and Unicode; export VTIMEZONE. Permit only the empty stylesheet's CSP hash for the widget's CSSOM rules, without enabling arbitrary inline styles.

## Calendar RSVP grants and transactional reminders

Event invitations use official mail-pack transactions and a per-version dispatch
ledger. RSVP grants bind an attendee to one event sequence, hash random 256-bit
tokens, expire after 30 days, and convey no workspace membership. ICS replies
must carry the grant and match UID, sequence, and attendee; ordinary incoming
email headers never authorize replies. Responses currently apply to a series.
Personal reminders use minute schedules, current membership checks, bounded
recurrence expansion, and a transactional notice ledger so retries cannot queue
duplicate notices. A fifteen-minute catch-up window bounds late delivery.

## Matrix through the current-membership API boundary

Use an outbound-only Matrix application service and virtual account identities.
Thura's Go API holds the application-service token and checks current workspace
and private-room grants on every operation; no Matrix credential is issued to
the browser. This initial protocol client replaces the research's browser SDK
recommendation because direct Matrix tokens would require a separate revocation
and device lifecycle. Rooms are explicitly server-readable. Federation grants
apply to a shared channel, with admin-controlled invitations/bans and joined
history. A dedicated two-domain Synapse v1.139.0 test proves bidirectional
messages, authorized file bytes, transaction-id deduplication, and remote bans.
Encrypted-room/device recovery work remains a separate design decision.

## Scoped LiveKit access and explicit media lifecycle

Meetings map workspace-owned UUIDs to server-controlled LiveKit room names.
The control plane uses the official protocol package for scoped JWTs and signed
webhooks, with no new authentication authority. Guests, recording and
transcription are disabled in the first version. Cameras/microphones start off.
Short token lifetimes bound admission reuse; webhook checks and a minute
reconciliation job terminate revoked participants. Self-hosted token reuse and
in-flight webhook delivery create a documented revocation window, so media
revocation is not described as instantaneous. Ended rooms cannot be recreated
when the required SFU auto-create setting is disabled. Physical-device and
restrictive-network acceptance remain deployment gates.

## 2026-10-08: Dependency github.com/livekit/protocol

Why: Use the official LiveKit token grants and signed webhook verifier rather than hand-built JWT or checksum verification. The vendor client confines the room-service protocol to workspace-bound meetings.

## 2026-10-08: Dependency github.com/teambition/rrule-go

Why: Use a maintained MIT recurrence parser rather than inventing RRULE parsing. The Calendar wrapper bounds occurrence count and time range and tests timezone/DST semantics.

## Drive budgets and reference-aware recovery

Serialize workspace quota reservations and immutable version saves with the workspace lock. Count all retained versions and active upload reservations; bound files, folders, sessions and versions so API listings cannot silently hide newly created resources. Purge requires an administrator and an already trashed file. Queue object deletions in the metadata transaction and recheck references in workers. A 48-hour orphan grace period protects in-flight writes, with prefix subdivision to avoid first-page starvation. Offline recovery bundles keep a checksummed Postgres dump and local object inventory together, refusing populated restore targets. Provider state and secrets require separate operator backup sets.
