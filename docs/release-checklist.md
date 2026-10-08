# First-release checklist

The completed [brief](brief.md) defines this release. The prototype under `/workspace` remains labelled sample data; `/app` uses authorized server APIs and durable workspace data.

| Workflow | Implementation |
| --- | --- |
| Accounts and ownership | Invite-only password accounts; workspace-owned data; owner/admin/member roles; invitation/recovery flows; current-membership checks and last-owner protection. |
| Mail and Contacts | Shared directory and suggestions; persisted/autosaved drafts; signatures; labels and filtering; scoped conversations and reply headers; recipient privacy; private uploads/removal/forwards; send/schedule/undo; Debian Postfix ingress/outbound. |
| Drive and Office | Private resumable uploads; immutable versions and conflict checks; sharing/revocation; trash/restore/purge; quotas/cleanup; optional ONLYOFFICE editing. |
| Calendar | Shared/personal events; recurrence/exceptions; timezones; ICS import/export; explicit invitations/cancellations; RSVP/reminders; persistent member-only meeting links. |
| Chat | Optional Matrix channels/direct conversations, receipts, replies/backfill, authorized file copies, federation and administrator invitations/bans. |
| Meet | Optional LiveKit member-only rooms/tokens, media controls, signed webhooks/reconciliation and ending; Calendar/Chat entry points. |
| Interface | Lazy application tabs, saved workspace/app/theme preferences, accessible controls, phone layout and explicit loading/empty/unconfigured/error states. |
| Operations | Operator-chosen hosts/domains; documented deployment services; bounded jobs/storage; metadata/object backup and clean restore. |

Local release validation is recorded in [implementation status](implementation-status.md). All existing test assertions remain intact. Go tests that invoke jobs directly run with `JOBS_WORKERS=0`; browser acceptance uses one worker on this cloud machine. Pushes do not wait for GitHub Actions.

Each deployment must supply credentials and its own DNS/TLS/provider/storage endpoints, validate public mail delivery, and test physical devices/TURN on its networks. Local fixtures do not establish those results. No public deployment is performed by this implementation.

SMIP is intended for chat and file exchange between two servers. Its protocol/adapters, recording/transcription, AI summaries, E2EE/device recovery, native collaborative sheets, CalDAV/IMAP and expanded hosted ingress/contact integrations remain outside this first release.
