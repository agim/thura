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

## Deferred page code and private page metadata

Use TanStack lazy route components so the router resolves page code before server rendering and hydration. Separate sign-in from the authenticated workspace shell and load each application tab on demand. Keep shell CSS in the entry to avoid an unstyled hydration frame. Prerendered routes preload only their own module and static dependencies, avoiding a hydration loading waterfall; dynamic tab imports remain deferred and static pages retain zero scripts/preloads. File downloads use a small shared helper instead of importing the Mail renderer. Rolldown media groups capture package modules without recursively capturing shared React dependencies; strict execution order preserves initialization across chunks. A browser network test proves unopened applications and LiveKit remain unloaded. A shared static route metadata file drives server heads and browser navigation, with generic descriptions and noindex for private/account/share/invitation pages; it never includes workspace data or URL tokens.

## Debian Postfix as the default deployment mail transport

The developer selected the mail transport available through Debian/Linux packages and explicitly left hosts and domains to deploying users. Use Debian Postfix with the official Līdza SMTP provider, a persistent MTA queue, and exact recipient mappings into Thura's existing signed raw-MIME ingestion. The pipe adapter returns a temporary failure until Thura acknowledges persistence; repeated raw deliveries retain a stable idempotency key. Hostname, mailbox mappings, inbound secrets and app origin are operator settings. No fixed public host/domain or new public-registration policy is inferred.

## Serialized draft autosave

Persist edits after a 750 ms pause with one write in flight per editor. Drain newer snapshots before queueing a send, retain dirty state after failures, and show save status explicitly. A reload/close warns while edits remain unsaved; internal unmount attempts a final flush. Closing a browser before acknowledgment can still lose edits. Cross-browser concurrent editing remains last-write-wins; this is not a collaborative editor.

## Bounded complete Mail and Contacts lists

Replace the latest-200/first-500 API truncation with ordered cursor pages and literal server-side search. Fetch one extra row to prove whether another page exists; validate limits/search/cursor size and enforce membership before reading or parsing a cursor. Use timestamp/ID and name/ID pairs to handle tied sort values without offsets. Cursors select positions, not permissions, and concurrent edits can reorder a live listing; the client deduplicates IDs while rendering. No snapshot-consistency guarantee is implied.


## Mail contact suggestions and attachment-preserving forwards

Recipient fields query the existing workspace-scoped Contacts API after a short pause, returning at most ten matches. Explicit keyboard-focusable buttons insert only the address into the unfinished last comma-separated recipient. Manual entry remains available when the directory is unavailable. No Contacts page module or new dependency is loaded.

Creating a forward locks its source message, checks that it belongs to the authorized target mailbox, and copies attachment metadata in the draft transaction. New attachment IDs reference the same immutable private objects, preserving bytes without a second storage write. Replies do not copy attachments. The existing ten-file/10 MiB combined limit applies to forwards; rejection rolls back draft creation. Any future Mail purge must check all attachment object references before deleting source objects. `forwardId` is a create-only field and is rejected on draft updates.

## 2026-10-08: Mailbox threading and cross-app handoffs

Why: Use mailbox-scoped RFC message headers and bounded cursor pages for conversations; normalize reply recipients server-side and keep forwarded objects immutable. Calendar meeting references preserve physical locations and use event sequence checks. Cross-app intents require explicit user actions; browser storage holds only app/theme preferences. SMIP chat and file transport is a separate protocol track, as specified by the developer.

## 2026-10-08: Importable application factory on Līdza v0.1.89

Why: The framework update requires a clean root and integration tests in tests/. Application wiring and shared page metadata now live in app/, with app.New accepting the frontend filesystem. main embeds the production frontend; integration tests construct the same application without embedding frontend files. Existing tests and assertions are preserved.

## 2026-10-08: Read current drafts and scope browser upload recovery

Why: Navigation snapshots never initialize an editable draft until a fresh authorized server read succeeds. Upload recovery fingerprints bind account, workspace, destination, filename, content type and bytes without storing plaintext metadata. Removed sessions permit an explicit restart; restricted browser storage does not block ordinary uploads.

## 2026-10-08: SMIP/0.1 chat and file transport reference

Why: Implement the developer-specified two-server chat/file protocol in isolated internal/smip and docs/protocols files while app completion proceeds separately. Use standard Ed25519 and TLS 1.3, explicitly paired keys/recipient policies, fixed canonical signing bytes, atomic durable receipts and immutable retry IDs. The Unix reference journal deliberately uses a private single-writer persistent filesystem for atomic rename/fsync tests; eventual Thura adapters must use database/storage packs. The first reference is server-readable and not mounted in Thura; payload encryption, public discovery and application adapters need separate versioned work. Map Matrix, SMTP, calendar, file and media boundaries rather than silently weakening their semantics.

## 2026-10-08: Pack audit added

Why: Complete roadmap milestone 1 with durable transactional workspace access and sharing audit events and operator audit visibility using the official pack.

Touches: lidza.json, packs.go

## 2026-10-08: Complete private previews and the isolated email reader

Why: Use standard-library bounded image decoders and the official storage/jobs packs. Cache one private version-bound Drive preview per file, re-encode first-frame PNG/JPEG/GIF thumbnails, and escape bounded UTF-8 text. Mail retains raw/source MIME while one server formatting allowlist, DOMPurify and sandbox/CSP protect reading; opt-in authorized CID thumbnails never enable remote content. PDF and active-document previews remain unsupported.

## 2026-10-08: Retain Matrix history across polling and reconnect

Why: Keep displayed windows by event ID, anchor backward paging to the first window and bridge reconnect gaps through at most 20 provider pages per refresh. Expose remaining gaps for explicit continuation, preserve history through transient failures and hide it on authorization errors. SMIP remains a separate first-release-excluded track.
## 2026-10-08: SMIP durable sender outbox and uncertain outcomes

Why: Persist the immutable signed packet and attempt marker before HTTP, and record acceptance only after a verified receipt is durably committed. Lost responses and interrupted attempts remain uncertain and retry the original packet, including after expiry for receipt reconciliation. Permanent errors pause for explicit operator resume; an expired packet is definitely unsent only when no attempt was recorded. Share the private Unix reference journal with bounded retention and due pages, retain historical public keys, and leave transactional Thura jobs/storage adapters as a separate integration.

## 2026-10-08: SMIP workspace consent and explicit immutable file import

Why: Freeze operator-paired server keys separately from transactional owner/admin bindings. Persist verified packets and receipts in Postgres before acknowledgement, never grant membership from a signature, and bound retained inbox records/bytes. Review only text and metadata; require a current member to explicitly import signed file bytes through Drive quota, immutable storage and audit transactions. Keep a dedicated API-hosted wire endpoint and explicit client profile, direct TLS 1.3, default-disabled capability and unchanged Matrix conversations.

## 2026-10-08: SMIP app outbox, durable dispatch and immutable Drive snapshots

Why: Commit signed packets and jobs together, recheck membership and consent under the workspace lock before authorizing each attempt, persist uncertainty before HTTP, and record acceptance only with a verified durable receipt. Freeze explicit peer origins and historical key trust; use unique attempt tokens, bounded reconciliation/backoff and an attempt budget. File sends retain verified immutable snapshots after source edits/trash. The browser retries lost queue responses with the same transaction ID and distinguishes queueing from transport acceptance, reading and import.

## 2026-10-08: Cancel SMIP sends only before a durable attempt

Why: Owners/admins need to withdraw an individual queued copy without disabling its whole stream. Share workspace/row locks with dispatch, require zero attempts, retain the original packet and quota, audit atomically, and make retries terminal and idempotent. Attempted packets cannot be recalled and the wire protocol stays unchanged.

## 2026-10-08: Admin page Server setup

Why: The server administrator manages encrypted draft/published snapshots, operator-paired providers and global policy in the existing admin frame. A one-time operator token creates the first account atomically; existing deployments require explicit ADMIN_USERS, and workspace roles grant no server authority. Optimistic revisions prevent stale tabs; publication, initial workspace/mailbox and audit commit together. Restart all nodes activates one snapshot, never individual draft fields. Refuse storage relocation while app objects exist. No secret is rendered, logged, returned or stored in browser persistence.

## 2026-10-08: Redirect unfinished installations into server setup

Why: Without accounts only /setup is reachable as a page. Until configuration is published, signed-in operators automatically enter the official /admin/setup wizard and other visitors sign in on /setup. An application-wide gate blocks normal APIs with 503, preserves setup assets and health, rechecks shared database state on every request, and leaves previously published settings available during draft restarts.

## 2026-10-09: Upgrade Thura to published Līdza v0.1.90

Why: Use the newest published framework and CLI through lidza update. Keep Thura operator-token bootstrap and explicit server-administrator authorization; the framework owner-claim model is additive and does not replace this flow. Regenerated schema includes the non-destructive owner-claim migration, and Docker pins the same release. Preserve app-specific ignore rules while excluding the new owner-claim token directory from Git and Docker contexts.

## 2026-10-09: Gate first production traffic on successful node activation

Why: Publication alone cannot prove that a process has loaded its mail, storage and policy settings. Register the activated revision only after decrypting, validating and successfully reconfiguring the official packs; first installations remain restricted and /readyz returns 503 until then. Existing initialized nodes retain availability through their previous valid settings during rolling updates. Explicit externally configured test fixtures are recognized only in test mode, and cannot initialize production without an encrypted snapshot.

## Check saved providers before publishing setup

Email and storage checks use separate official-pack instances filled only from the encrypted draft, avoiding mutation of live providers. Explicit administrator confirmation permits one test email to that administrator and one unique storage object. Durable intent/audit precede network contact; repeated IDs never repeat side effects. A master-key HMAC binds success to relevant provider settings, and a newer intent supersedes earlier success. Publish requires matching successful checks. Generic result states avoid provider error leaks, and failed storage cleanup identifies only the reserved unique object for manual removal.

Storage location freezes after first publication, even before app content exists. Checking for existing files alone cannot prevent old nodes or concurrent uploads writing to the prior backend during a configuration change. Changing credentials for the same location remains supported; relocation needs a coordinated offline migration.

## 2026-10-09: Patch Go and affected transport dependencies

Why: Go 1.27.2 and fixed x/net, DTLS, STUN, CEL and gRPC releases remove reachable/imported security advisories. Staticcheck 2026.2.1 needs published x/tools v0.51.0 to read the patched compiler export format; scripts/install-analysis-tools.sh builds that analyzer in an isolated module for cloud setup and CI without changing application dependencies.

## 2026-10-17: Person-first Correspondence mail slice

Why: Reuse explicitly saved contacts as the manual organization funnel and their optional company field for a secondary rollup. Parse exact mailbox identities after a bounded SQL candidate scan; retain workspace authorization, cursor pagination, and original-message handoffs. This first slice covers mail only; normalized organizations, historical affiliations, qualification suggestions, chat, calendar and document links remain follow-up work.

## 2026-10-17: Preserve native light-dark theme colors in production CSS

Why: Thura changes color-scheme at runtime. The default CSS target lowered light-dark() to static compiler helper variables, leaving invalid concatenated colors when neither helper was defined. Preserve native CSS with cssTarget: esnext so the existing shared palette works for both themes. Correspondence reuses the workspace tokens and secondary controls; its browser test checks actual light and dark background colors.

## 2026-10-17: Correspondence source records and explicit associations

Why: Matrix owns chat messages, while Thura owns conversation, Drive, meeting, and calendar metadata. A unified source-record query supports existing exact mail matching, named file recipients, local participants, and calendar invitees without inventing message or attendance history. Explicit contact/source links cover external relationships and unrelated project documents; every read rechecks source permissions. A tuple cursor includes source to distinguish equal timestamps/IDs across tables. Single-record metadata endpoints let existing apps open older items beyond their recent-list limits. The person remains primary and organization remains an optional current-contact rollup. Source content indexing and automatic suggestions are separate work.

## 2026-10-17: Live person-scoped content search

Search Matrix using the current virtual user's native room-filtered search endpoint, never the application-service bot or a shared message index. Do not join rooms during search. Use live current-version Drive text extraction with checksums, bounded ZIP/XML parsing, and an optional timed Poppler PDF process. This avoids retaining stale private text after edits, replacement, or access changes. Keep one timeline entry per source, with excerpts and Chat search handoff; cap candidate scanning and surface partial coverage. Matrix endpoint contract: https://spec.matrix.org/v1.16/client-server-api/#searching.

## 2026-10-17 — Zero-based communication workspace

The user selected Mail-0/Zero as the visual foundation and explicitly rejected admin dashboard templates. Adapt Zero's neutral light/dark palette, compact navigation, separated list/reading panels, and mobile detail/back flow to the existing React application. The source reference is commit 64c5480c341750578da0746f2db9ad84da686334; its MIT notice is retained in docs/licenses/zero-MIT.txt. The application-specific stylesheet is scoped to the live workspace. Thura keeps its own backend, authentication, source permissions, and person-first Correspondence model; no Zero AI services, provider integrations, or account system are imported.

## 2026-10-17 — Loop product name

The user selected Loop as the short, brandable feature name. Navigation, headings, contact actions, search labels, and current documentation use Loop. The feature route is /app/loop, and contact APIs use /contacts/{id}/loop with matching generated client operation names. Documentation lives at docs/loop.md. Internal database identifiers remain unchanged.

## 2026-10-17: Mobile communication flows and private default calendars

Mobile screens prioritize reading and the primary action. The live shell uses a persistent seven-app bottom navigation and an account menu, with 44px touch targets and safe-area padding. Mail uses a folder selector and a separate reader; tablet mail also switches between list and reader instead of squeezing three panels together. Secondary forms and tools use a shared compact disclosure, retaining their desktop presentation. Calendar follows viewport changes, opens in agenda view on phones, and gives the event editor its own reading space. Loop keeps person context and content search visible while moving timeline management out of the main flow. Sign-in and loading use the same workspace surfaces and theme.

Every member receives a stable private `My calendar` within each workspace on their first calendar-list request, including existing accounts. A deterministic workspace/subject UUID and a workspace-locked transaction prevent duplicates and recheck membership before provisioning. The list returns its default ID explicitly and places it first; the frontend selects it unless a source handoff or user selection requests another calendar. Existing owner-only calendar access remains authoritative. Shared calendar creation stays explicit and unchanged. Integration coverage checks stable concurrent reads and isolation between members.

Calendar creation also seeds the detail cache with the server response before selecting the new calendar, so imports and event actions cannot race a stale previous calendar during the handoff. Browser fixtures retain their assertions while following the new mobile account menu and neutral canvas colors. New viewport tests cover all seven live apps and exercise touch targets, disclosures, default calendar selection, composing, and returning to the inbox at 320–1440px.

## 2026-10-17: Content-first Drive and contextual forms

Drive starts with searchable, sortable files in a saved list/grid view, compact folder navigation, and a New menu. Familiar Google Drive interaction patterns inform the organization; no Google code or assets are copied. File tools stay with the selected file; management and sharing expand on demand. Contacts starts with contacts and search, with creation/editing in an accessible native dialog. Mail labels and signature settings also use dialogs rather than permanently expanding the navigation column. Native dialogs provide focus containment and Escape dismissal. Existing APIs and permissions remain authoritative.

## 2026-10-18 — One live workspace with content-first navigation

The browser-local `/workspace` prototype is retired and redirects to `/app`; its sample app code and sign-in links are removed. Each live app now has a stable `/app/{mail,contacts,loop,drive,calendar,chat,meet}` URL so reloads, direct links and browser history select the same application. The shared app bar replaces the global side navigation, leaving each application's own controls in its local sidebar.

Header search is contextual for Mail (current folder), Contacts, Drive (file names), and Loop (person lookup or the selected person’s content). The other apps offer a workspace-scoped people/file lookup using the existing authorized endpoints. Secondary expandable tools now use the shared native dialog, including keyboard dismissal and focus containment. Mail, Contacts and Calendar use compact content views and dedicated editing actions; Drive no longer repeats a hero, search field and root headings before the file list. Desktop and phone screens are reviewed in the running app as well as through browser regression tests. Sites' local-only workflow was consulted; Thura remains the existing Go/React application and is not published as a separate Site.

The Drive view preference validator is module-scoped to prevent repeated hydration effects from fighting list/grid updates. Browser coverage observes the selected view while a file's detail pane is open. The obsolete prototype tests are replaced with live routing, search, dialog and responsive coverage with the user's explicit approval.

The shared inner-page dimensions use a 200px desktop navigation rail, 24px column gap, 16px page padding, consistent page surfaces, and compact title/action placement. On phones, local controls collapse into compact selectors and contextual actions. Dialog forms use their own grid so toolbar flex sizes cannot create oversized form gaps. Visual review covers all seven live apps at 1440×900 and 390×844, plus existing automated coverage from 320–1440px.

Settings now live at `/app/settings`, `/app/settings/mail` and `/app/settings/calendar`: appearance, mailbox signatures and label management, timezone and calendar creation. Mailbox and mobile folder switching are in the account menu; a single mailbox does not add a redundant chooser to Inbox. Loop uses the same plain page-title treatment as the other apps.

Shared controls replace system-facing selects, date/time inputs, file-input chrome and JavaScript alert/confirm interfaces. Custom combobox/listbox menus support keyboard navigation and custom date/time calendars use the shared dialog. File uploads retain the required hidden browser file input behind a Thura button. Confirmation dialogs preserve explicit destructive-action consent. Accessibility lint recognizes the shared control components; browser tests exercise the visible controls and their selections instead of hidden native options.

All seven app landing views use `AppPageHeader` and `NewAction`: a plain 20px title with one 100×44px Plus/New button in the same position. The accessible label identifies the action (New message/contact/chat/event/meeting). Editing and detail views keep their focused back navigation. Main navigation order is Inbox, Calendar, Contacts, Loop, Drive, Chat, Meet. Regression coverage compares header geometry, typography and action contents across phone and desktop sizes.

The route audit also found the legacy standalone Contacts/Members and guest/account/setup pages. `/contacts` now redirects to `/app/contacts`; `/members` redirects to `/app/settings/members`. Membership management reuses the existing permissions and APIs inside Settings, with compact member rows and an invitation dialog. Public/account/setup/share/RSVP pages use one branded `PublicLayout` with the workspace palette, forms and controls; the old utility navigation is removed. Framework-owned `/admin` screens remain in the official Go admin frame.

- 2026-10-18: The official administration frame uses an app-owned neutral Thura theme. Server setup reuses compact forms and step navigation, custom provider menus, and a dialog for draft restart. The controls asset uses the official admin Page Downloads gate; configuration actions, sealed credentials, revision checks and typed publication confirmations are unchanged. Focused workspace screens share DetailBack; phone file details hide the landing toolbar and put version history in a dialog.

- 2026-10-18: DateTimeControl composes separate reusable DateControl and TimeControl fields; the date picker contains only dates and AM/PM time selection is directly available beside the date. Calendar event editing, scheduled mail and date-range filters use the same primitives. App-local sidebars and compact behavior share a 1000px breakpoint, including the compact-screen React subscription, so medium-width Mail no longer retains an isolated desktop sidebar.
- 2026-10-18: AppSidebar is the shared sidebar implementation for Inbox, Calendar, Contacts, Loop, Drive, Chat, Meet and Settings. Small screens expose every local menu through the same visible Browse action and dialog; choosing a navigation item closes it. Loop's unselected people list remains primary content. Drive storage lives with its local menu, and preview uses one show/hide action with separate retry spacing.

- 2026-10-18: Chat uses a searchable conversation list, compact sender rows with day separators, a separately scrolling history and a persistent composer. Enter sends; Shift+Enter adds a line. Paging preserves the user's scroll position, and reconnect/access-revocation logic remains authoritative. The server resolves display names only from current workspace membership; unknown Matrix identities remain visible with the full provider ID retained in accessible descriptions and search results. Meet uses available/past meeting navigation, validated workspace links and a muted pre-join lobby. Joining still uses the existing member-scoped LiveKit API with camera and microphone initially off; no simulated call or device permission request is introduced. Both apps retain AppPageHeader, AppSidebar and shared dialogs across desktop and mobile.

## Event-scoped guest meetings and explicit transactional delivery

The user authorized invited external guests with a meeting link and PIN. Keep
ordinary member meeting links unchanged. A calendar-owned grant binds one event
to one room, with a random numeric ID/PIN, PIN hashing and master-key sealing,
fragment-based invitation URLs, rate-limited POST admission, short room-only
LiveKit tokens and existing signed webhook/minute reconciliation enforcement.
Create event/room/grant and optional invitation outbox together, so failures
cannot leave partial calendar records or send incomplete invites. Camera and
microphone start off for members and guests. Compose the shared in-call view
from LiveKit's public components to retain labelled controls at phone sizes.

## Workspace labels shared across resources

A label is workspace-owned and reusable on mail, Drive files, contacts, calendar events, chat rooms and meetings. One shared dialog offers New label first and creates plus assigns it atomically. The catalog and assignment writers serialize on the workspace lock, reuse case-insensitive names on create-and-apply retries, and preserve current workspace membership and resource-specific access checks. Settings → Labels manages the catalog; sharing a label never grants access to its resources.

Keep the existing mail_label and mail_tag table names and old label IDs so stored assignments and mailbox API clients remain compatible. New catalog entries carry workspace_id and no mailbox_id. Existing entries resolve their workspace through their original mailbox, preserving stored data without destructive migration. ResourceLabel records non-mail assignments; mail retains its existing tags and filtering queries. Resource reads always pass through the ordinary access checks, including personal calendars and direct chats.

## 2026-10-19: Shared action rows and human calendar repeat controls

Why: All application action groups use ActionRow for consistent sizing, alignment, compact icon labels and app-styled hints. Repeat choices use shared menus and date controls while retaining RRULE only at the API boundary; imported custom schedules stay unchanged until explicitly replaced. Action modals close on confirmed success and preserve failed submissions.

## 2026-10-19: Shared recipient and action controls

Why: Email and contact fields reuse RecipientInput for workspace-scoped suggestions, keyboard selection and removable recipient pills. Authentication omits directory lookup. ActionButton owns visual variants while ActionRow owns responsive placement. Dialog menus remain within the dialog scroll area.

## 2026-10-18: Shared rich text editing

Use Tiptap/ProseMirror for a shared WYSIWYG surface rather than browser editing commands. Email and calendar descriptions use a compact toolbar; native Drive HTML documents expose headings, font size, alignment, highlighting and tables. Keep plain-text email/ICS bodies alongside sanitized authored HTML. Inbound email retains its stricter reader policy. Native documents keep an account-scoped recovery copy in this browser tab and reuse Drive's authenticated downloads and resumable, version-checked uploads; DOCX/XLSX continue through the existing ONLYOFFICE integration so their formats are not silently converted. Recipient labels are inline in compose and Cc/Bcc expand into separate rows using the shared RecipientInput.

## 2026-10-19: Pack llm added

Why: Shared user-invoked AI drafting, rewriting, summaries and preparation across workspace resources, using the official LLM pack and operator-managed settings.

Touches: lidza.json, packs.go

## 2026-10-18: User-invoked AI assistance across workspace resources

Use the official Līdza LLM pack for Ollama, OpenAI, Anthropic and Google Gemini, preserving one provider interface, encrypted credential storage and token usage reporting. Keep model IDs configurable instead of a hardcoded model catalog. The server administrator configures and enables the service in Settings; it starts disabled and production fake responses are unavailable. Workspace members explicitly review the text included in each bounded request. The model receives no retrieval or mutation tools and cannot send mail/chat, modify contacts, invite participants or record calls. Outputs require a user action to insert/replace text; Drive appends suggestions so existing formatted content survives. Meet prepares agendas from supplied context and never pretends to summarize unrecorded audio. Existing resource authorization supplies the visible context; the generation endpoint accepts user-provided text and never reads other resource rows.

## 2026-10-19: Contextual AI sidebar and embedded writing assistance

Follow Gmail's interaction pattern within Thura's shared visual system: a nonmodal workspace sidebar for reading and resource work, and an embedded Help me write surface inside email compose. The sidebar shares quick prompts, a prompt box, reviewed source text, safe text output and explicit insertion with the writing helper. At desktop widths it sits beside the resource; smaller screens use a full-width assistant view below navigation and return to the resource on close. The sidebar stays open across app switches and automatically replaces its context when the viewed resource or text changes, aborting the previous request and clearing its suggestions. Closing an editor removes its insertion callback. Workspace changes clear the assistant; inline writing suggestions retain an explicit refresh guard when the draft changes. User-triggered suggested email replies create drafts and never send. Follow-ups include only the last visible suggestion within the existing bounded request, with no provider-layer changes or new model tools.


## 2026-10-19: Persistent AI context and Līdza v0.1.123

Sidebar visibility belongs to the workspace rather than an app component. App transitions mask the previous resource immediately, then select the highest-priority context offered by the current app. Viewed email IDs, file details/previews/editors and chat room IDs supply current context; changing it remounts the request surface and aborts obsolete requests. Closing a temporary contact action menu retains its reviewed contact text but removes its insertion callback. Inline writing still protects a newer draft from older output. No app switch triggers a model request.

Updated the CLI and framework module from v0.1.90 to the newest published v0.1.123 release, regenerated pack schemas/clients/recipes and applied the additive auth/job migration. Thura keeps its existing account, workspace, provider configuration and explicit AI request flows.


## 2026-10-19: Workspace-shared AI result cache

Use Postgres for durable exact-request response caching without adding infrastructure. The SHA-256 key includes the reviewed source text, file ID/version where available (including changes beyond the reviewed excerpt), user instructions/follow-up, action/system prompt, resolved provider/model, provider configuration (including credential rotation) and framework version. Only the digest and successful complete response are stored, scoped to the current workspace; inputs and credentials are not persisted as cache metadata. This lets every authorized member reuse a file summary while content or settings changes select a fresh entry. No resource-ID lookup or access grant is added: requests still require membership and explicitly supplied text. Recheck membership after waiting and generation, and check enabled state before cache access.

An atomic expiring Postgres lease coalesces concurrent equal requests across users and processes without holding database connections during model calls. Cancelled or failed requests release their lease; crashed processes expire after 45 seconds. Errors, cancelled requests, empty replies and token-limited replies are not cached. Expired entries regenerate after 30 days; successful writes prune expired entries and retain at most 1,000 entries per workspace. The assistant marks reused responses; connection tests always reach the provider.


Līdza v0.1.123 pins local storage with an `os.Root` handle. Thura records the configured directory identity at startup and checks it before app-owned object writes, retaining the existing rollback-and-retry behavior if that directory is moved, replaced or unavailable. All writes still use the official storage pack; setup probes use their separate draft-configured client.


## 2026-10-19: Drive layout follows available content width

The AI dock and file details can narrow Drive independently of the browser viewport. Container queries now hide secondary file columns when the list is narrow and use a focused detail view with Back to files when two readable panes no longer fit. Filenames remain single-line with ellipsis and full accessible names; label badges keep horizontal text and dark lettering on their pale background. Regression tests measure text width, badge height, pane boundaries and stable geometry through polling at desktop and mobile sizes. Visual acceptance requires inspecting actual names and labels, not merely checking that the page has no horizontal overflow.

The local demonstration uses loopback-only Ollama with gemma3:4b, configured through the existing encrypted AI settings and official LLM pack. A live verification generated a summary for one member, reused it for another member with no provider call, then uploaded version 2 of that same file and verified a fresh provider call and changed response. Local model data and startup configuration stay outside git.

## 2026-10-19: Embedded Office editing and explicit PDF reconstruction

The user accepts a self-hosted editor embedded in Drive. Extend the existing ONLYOFFICE integration to native DOCX/XLSX/PPTX/PDF editing and read-only imports for legacy/common document, spreadsheet and presentation formats. Convert imports into separate editable copies; never save modern bytes under a legacy extension. Keep signed source sessions, member revocation, fixed service download origins and version-conflict protection. Browser assets and sockets use an authenticated same-origin API proxy, with no application credentials forwarded to the service. Only the editor subtree permits same-origin framing. Signed callback cache paths map to the fixed upstream, not the supplied callback host.

PDF conversion uses the same self-hosted service. Scanned PDFs use explicitly requested bounded local recognition in the existing preview worker, followed by optional reviewed-text reconstruction through the official LLM pack. Līdza v0.1.123 exposes text messages, not image attachments; do not claim vision inference. The reviewed text becomes a genuine editable DOCX through a bounded OOXML writer, with the original PDF unchanged. Refuse truncated AI output. Temporary renderer files are worker scratch only; durable results remain in private storage. Runtime service installation and secrets are local demo infrastructure, excluded from git. See docs/drive-documents.md for formats, boundaries and deployment requirements.

## 2026-10-19: Full-screen file opening and shared resource menus

Why: Double-click and Enter open desktop files in a dedicated layout with an X back to Drive. Keep mobile/coarse-pointer opening read-only with the installed Community edition; the user permits ONLYOFFICE’s slim mobile editor when the installed edition supports it. Hide and inert the browse frame, relocate the existing AI dock into the document layout, and preserve HTML draft recovery and versioned saves. Reuse one bounded keyboard-accessible resource menu for file and email commands; keep conversion under Manage file so the existing five-action alignment contract remains intact.


## 2026-10-19: Free Collabora editor with mobile saves and discovered formats

Why: Thura is an open-source platform that others may host commercially; the user requires a freely deployable editor with mobile editing. Real desktop DOCX/XLSX edits were saved and reopened. The user's physical Android changes were verified in stored versions: Word version 4 and Excel version 3, B3=7, formula =B3*C3, calculated result 70. Collabora CODE is the sole service; the user explicitly requested complete removal of the previous connector. The demo pins official ARM64 CODE 26.04.5.1.

Use discovered extension/MIME edit/view actions across Writer, Calc, Impress and Draw. Writable native formats retain their format; read-only imports require a separate converted copy. PDF uses Collabora's advertised view_comment action with a read-only grant. Direct PDF-to-DOCX failed the demo fixture, so retain the explicit reviewed-recognition/Word-copy workflow. HTML and images retain Thura's native surfaces. All document conversion uses Collabora.

WOPI grants bind file/member/audience/expiry/read-only state, with current permission checks on every read/write. Row locks and persistent document locks protect immutable versioned saves, quota and duplicate checks, and block conflicting uploads. Browser assets/sockets use the authenticated same-origin proxy with no forwarded app credentials. Keep the service root, WOPI allowlist and trusted TLS deployment configuration in docs/collabora.md; secrets remain encrypted or local ignored infrastructure.


## 2026-10-19: Remove the previous editor completely

The user explicitly requests Collabora as the replacement with no alternate editor retained. Remove prior provider code, callback/source/proxy routes, browser SDK, provider selection and credentials, deprecated session bookkeeping and demo container. Replace protocol-specific tests with equivalent Collabora WOPI/iframe/conversion checks, as explicitly approved by the user. Retain original files and immutable versions. One Compose stack may deploy the app and Collabora as separate containers; do not place the office engine in Thura's minimal application image.

## 2026-10-19: Managed Postfix Compose with independent receiving and sending

Why: The user requests an included Postfix service configured by the initial wizard, including Postfix receiving with Mailgun or any other supported outgoing transport. A private token-authenticated loopback sidecar protocol publishes activated exact mailbox mappings without exposing outgoing keys or a Docker socket. First setup uses only private outbound submission until mailbox publication and activation; Postfix retains its durable queue and reloads mapping changes. Keep external signed relays and all official mail-pack providers available independently.

## 2026-10-19: Domain-discovered SMIP mail with immutable transport selection

Why: The user requires verified SMIP before the configured API provider, without silent downgrades or duplicate sends, and actual two-server delivery. A separate SMIP/0.2 profile preserves paired SMIP/0.1 chat/files. Fixed DNS/HTTPS capability discovery authenticates domain keys through TLS 1.3 and a signed short-lived document; sticky verified-domain history and required-domain policy prevent learned/explicit secure routes from falling back. Thura freezes message/provider configuration and recipient routes, commits attempt markers before network contact, and atomically stores inbound email plus its signed receipt. Retry exact secure packets; hold ambiguous API results because the official mail pack has no universal idempotency/envelope contract. Keep API recipients separate from secure recipients and expose per-recipient delivery states. No vendor SDK. The current adapter covers Mail; framework/system notification queues remain explicit separate boundaries. See docs/smip-mail.md for first-contact DNS and mixed-route header limitations.

## 2026-10-19: Admin page SMIP email domains

Why: Server administrators need to inspect learned secure delivery requirements. The official admin frame and existing server-administrator gate expose only domain registry rows for the active local mail identity across workspaces: domain, state, endpoint, timestamps, failure reason and sticky verification. No message bodies, recipient addresses, signing secrets or provider credentials are shown. The page is read-only and cannot clear trust or trigger a downgrade.

## 2026-10-19: Shared inbound abuse screening and optional automatic AI

Why: SMIP authenticates a sending domain, not its intentions. The user requests rate limits, reputation and complaints, spam quarantine, scanning, impersonation warnings, unknown-sender approval and automatic AI screening with a settings opt-out. Attach one screening record and durable job to the shared intake transaction, before any received message appears in Inbox; retain the existing signed-receipt/deduplication boundary. Use workspace-local sender/domain counters and complaint reputation, with separate verified-SMIP and unverified-relay identities. Never create a global blacklist from one workspace's feedback.

The official LLM pack classifies bounded, quoted untrusted mail without tools or link visits, using the configured model. A per-workspace hourly budget bounds automatic usage. Required failures and exhausted budgets hold mail; incomplete/invalid model output is never a clean verdict. Static findings and private ClamAV whole-MIME scanning remain independent of AI. The included nonroot ClamAV Compose service has no public port or application storage mount and updates signatures through FreshClam. Store no model credentials in messages/jobs. Human release is explicit and audited; it cannot bypass pending required checks or a positive antivirus result. Existing email is visibly unassessed until explicitly rescanned. See docs/mail-protection.md for limits and operational requirements.


## 2026-10-19: Reject the first free pretrained email classifier after measured evaluation

The user requires a separate incoming-mail classifier with no paid API/training dependency, a retained Mail settings toggle, and a hard 1-vCPU/1-GiB benchmark before integration. The pinned Apache-2.0-declared DistilBERT candidate fits the resource limit but fails quality gates: INT8 flags 9/32 authored legitimate emails and 6/100 publisher legitimate emails; FP32 retains the major failures. Both miss all four long-tail phishing challenges despite complete overlapping-window processing. Do not activate or bundle this candidate. Preserve the research-only benchmark and rejection evidence in docs/mail-classifier-evaluation.md; no serving endpoint or intake integration is added by this evaluation. Publisher test data is not independent validation, and the corpus/weights are not redistributed. Future model selection must review license/provenance, independently measure false positives/recall, and retain failure quarantine without assistant fallback.

## 2026-10-19: Keep local mail models disabled after quality failures

Why: Licensed three-class training and AntiSpamInstitute evaluation fit their measured resource limits but fail spam separation, legitimate-mail false positives or phishing challenge gates. Preserve the workspace toggle and assistant separation; do not activate candidates based on aggregate accuracy or memory fit. See docs/local-mail-training-evaluation.md and docs/antispaminstitute-evaluation.md.

## 2026-10-19: Reject the locally fine-tuned Llama 1B checkpoint after constrained CPU evaluation

Why: The user authorized local Mac training and 2-vCPU/2-GiB serving tests, with accuracy prioritized over screening latency. The corrected three-class MLX QLoRA pipeline completed 100-step timing pilots and a fixed 600-step run, but the exported Q4 checkpoint falsely flagged all 32 legitimate controls and separated only 9/20 ordinary spam cases despite detecting 44/44 phishing cases. Retain the Mail toggle and assistant separation; do not activate or bundle this artifact. Preserve original timings and the explicitly relaxed latency preference. See docs/llama-local-training-evaluation.md.

## 2026-10-19: Keep class-only Llama v2 disabled after constrained CPU regression

Why: The corrected one-decision three-class objective trained successfully and eliminated legitimate-mail false positives on this fixture, but the Q4 CPU service detected only 22/44 phishing cases and failed instruction/tail gates and conservative memory headroom. Native same-corpus scoring reached 41/44, below target, and fusion probes preserved native predictions; export/runtime parity remains unresolved. Retain the Mail toggle and assistant separation, preserve the unchanged quality gates, and do not activate or redistribute weights. See docs/llama-classification-v2.md.
