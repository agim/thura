# Calendar

Workspace members can use shared calendars; personal calendars are visible only to their creator. Events, attendees, exceptions, and sequences live in Postgres. Every route checks current workspace membership and personal-calendar ownership. Shared-calendar members can edit its events. Timezone views default to America/New_York and accept IANA identifiers; the binary includes timezone data.

All-day events store start/end dates separately, with an exclusive end date. Timed events store instants plus their recurrence timezone. The browser rejects nonexistent local start/end times and chooses the first ambiguous fall-back time. Recurrence expansion preserves the local wall clock, skips nonexistent DST occurrences before counting, and uses the first ambiguous occurrence. Daily/weekly/monthly/yearly rules require COUNT or UNTIL, produce at most 1,000 occurrences, and finish within ten years. Clock-frequency rules, multi-time expansions and Easter extensions are refused. Views span at most 93 days and return at most 5,000 occurrences. A workspace can have 100 calendars and 1,000 stored events.

Edits use the current event sequence and row locking; simultaneous changes cannot overwrite each other. Per-occurrence edits retain a stable original instance key, including when an occurrence is moved. Changing a series' timing/recurrence requires an explicit exception reset. Cancellations retain event metadata and disappear from active views.

ICS import is limited to 1 MB/200 VEVENT records; export is limited to 8 MB. Imports commit atomically and match UID within the selected calendar. Older/repeated sequences cannot resurrect cancelled events or create duplicate series. An imported cancellation can contain just UID/SEQUENCE. Attendees, escaped/folded Unicode text, all-day dates, EXDATE and recurrence overrides are supported. Export includes VTIMEZONE transitions for supported years. IANA timezone identifiers and UTC are supported; custom/proprietary timezone definitions are not interpreted. Floating timestamps use the selected import timezone. VTODO and alarms are not imported. Imported content is data; it never changes app instructions or executes HTML.

The UI uses FullCalendar 6.1.21 standard MIT plugins and Luxon 3.7.2. These versions share compatible peer dependencies, including React 19. The widget is loaded only when Calendar opens. Its CSSOM stylesheet uses the CSP hash of an empty style element; other inline styles/scripts remain blocked. Recurrence uses the MIT-licensed rrule-go 1.8.2 with explicit time/iteration bounds and DST normalization.

Calendar creation/editing/import currently stores data without sending invitations. Invitations, RSVP handling and reminders are the next slice. ICS interoperability is bounded to the documented subset; external calendar synchronization and CalDAV remain deferred.

## Invitations, replies, and reminders

Sending invitations is an explicit action for the saved event version. The
transaction queues one official mail-pack message per attendee with a
`METHOD:REQUEST` attachment (or `METHOD:CANCEL` after cancellation). Repeating
that action for the same version within 30 days queues no duplicates. The mail
worker can retry; delivery itself follows the provider's at-least-once semantics.
Log/outbox providers capture messages locally and do not deliver email.

Each attendee gets a random 256-bit RSVP grant, stored only as a hash, for
this event and its current sequence. The `/rsvp#token` page moves the token
into memory and submits it in API request bodies. It reveals this event and
the recipient's own response, and grants no workspace or account access.
Removal of the attendee, expiry (30 days), event edits, or cancellation
invalidate the link. Bearer-link possession authorizes the response; an
arbitrary inbound email's From header does not. An ICS `METHOD:REPLY` can be
uploaded through the link and must match UID, sequence, and attendee identity.
Replies and invitations currently apply to the whole series; occurrence-only
replies are explicitly rejected. The service does not ingest REPLY attachments
from unverified external email automatically.

Members can subscribe to their own email reminder, 1 minute to 1 week before
an event, with at most 100 subscriptions per account. All-day reminders use
09:00 in the event's timezone. A minute schedule scans subscriptions using
cursor pages, skips cancelled occurrences and events, and checks current
workspace membership and personal-calendar ownership before queuing. A
transactional notice ledger deduplicates each recipient/version/occurrence.
The job catches up reminders at most 15 minutes late; a longer outage can
miss reminders. Removing a subscription prevents future queuing but cannot
recall mail already handed to the delivery worker. Monitor job failures and
mail capture/provider status in the official admin pages.
