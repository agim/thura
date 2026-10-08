# First-release checklist

The completed [brief](brief.md) defines this release. The prototype under `/workspace` remains labelled sample data; `/app` uses authorized server APIs and durable workspace data.

| Workflow | Implementation |
| --- | --- |
| Accounts and ownership | Invite-only password accounts; workspace-owned data; owner/admin/member roles; invitation/recovery flows; current-membership checks and last-owner protection; durable transaction-bound access/sharing audit and manager-only history. |
| Mail and Contacts | Shared directory and suggestions; persisted/autosaved drafts; signatures; labels and filtering; scoped conversations and reply headers; recipient privacy; private uploads/removal/forwards; send/schedule/undo; Debian Postfix ingress/outbound; server sanitization, sandboxed formatted reading and opt-in authorized CID thumbnails. |
| Drive and Office | Private resumable uploads; immutable versions and conflict checks; sharing/revocation; trash/restore/purge; quotas/cleanup; private version-bound background PNG/text previews; optional ONLYOFFICE editing. |
| Calendar | Shared/personal events; recurrence/exceptions; timezones; ICS import/export; explicit invitations/cancellations; RSVP/reminders; persistent member-only meeting links. |
| Chat | Optional Matrix channels/direct conversations, receipts, retained history, anchored backfill and bounded reconnect bridging, authorized file copies, federation and administrator invitations/bans. |
| Meet | Optional LiveKit member-only rooms/tokens, media controls, signed webhooks/reconciliation and ending; Calendar/Chat entry points. |
| Interface | Lazy application tabs, saved workspace/app/theme preferences, accessible controls, phone layout and explicit loading/empty/unconfigured/error states. |
| Operations | Operator-chosen hosts/domains; documented deployment services; bounded jobs/storage; metadata/object backup and clean restore. |

Local release validation is recorded in [implementation status](implementation-status.md). All existing test assertions remain intact. Go tests that invoke jobs directly run with `JOBS_WORKERS=0`; browser acceptance uses one worker on this cloud machine. Pushes do not wait for GitHub Actions.

Recovery checks verify fresh draft loading across app navigation, failed-read retry, metadata/account-scoped resumable uploads, removed-session restart and uploads with blocked browser storage. They are included in the complete browser suite below; see the recovery completion entry in implementation status.

The roadmap reconciliation adds audit history, private previews, safe CID reading and retained Chat history/reconnect bridging. All 44 browser checks, the integrated Go suite, real Matrix/Postfix fixtures, a 42-table/1,109-file offline restore and the current production build pass. Strict verification has zero warnings, and all 12 production pages pass the default size/CLS budgets. See the latest entry in implementation status for limits and evidence.

Each deployment must supply credentials and its own DNS/TLS/provider/storage endpoints, validate public mail delivery, and test physical devices/TURN on its networks. Local fixtures do not establish those results. No public deployment is performed by this implementation.

SMIP is intended for chat and file exchange between two servers. Its experimental transport reference is tracked separately; enabling application adapters, recording/transcription, AI summaries, E2EE/device recovery, native collaborative sheets, CalDAV/IMAP and expanded hosted ingress/contact integrations remain outside this first release.
