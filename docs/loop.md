# Loop

Loop is Thura's internal, person-first timeline and search feature.
Open it from the workspace navigation or a contact's Loop button.
Save a contact to begin organizing their history; no organization is required.
An external contact is a record, never a login or an invitation.

## Sources and matching

- Mail matches parsed From/To/CC/BCC addresses exactly, case-insensitively.
  Display names and incidental mentions do not identify a contact. Drafts,
  pending/failed delivery, spam, and trash are excluded. Development captures
  are marked as not delivered.
- Documents match active, unexpired named-recipient Drive shares. Public,
  expired, and revoked shares do not create associations. Trash is excluded.
- Chats match saved local participant accounts by email. Entries represent
  conversations, not individual Matrix messages. External relationships can
  be linked manually without guessing identities from room names.
- Meetings match local participant records by email. These records can exist
  from token issuance and do not prove attendance.
- Calendar events match organizer or attendee email. A recurring series appears
  once; occurrence exceptions are read in Calendar. Cancelled events are labeled.

Use **Link existing item** to associate an accessible document, conversation,
meeting, or calendar event with the selected person. Linking neither grants
source access nor sends invitations. Removing a manual link does not suppress
an automatic participant/recipient match. Links to deleted or inaccessible
sources disappear from results; later restored access can make them visible.

## Search, dates, and permissions

Search contact names, emails, and organizations in the directory. Timeline
search covers mail subjects/plain text/addresses, document filenames and media
types, conversation and meeting names, and calendar titles/descriptions. Chat messages are searched live on Matrix as the current virtual user, scoped
to each associated accessible room. Search does not join rooms or change
membership. Matrix determines token matching and history visibility. A matching
conversation opens in Chat with its search term and paginated matching messages.

Current Drive version contents are extracted on demand: UTF-8 text, Markdown,
CSV/TSV, JSON/XML, HTML text, DOCX/XLSX/PPTX, and OpenDocument text formats. PDF
text uses the optional Poppler `pdftotext` executable on PATH (installed in the
local demo). Scanned PDFs need OCR, which is not included. Binary legacy Office
formats, encrypted files, and images are unsupported. ZIP expansion and output
are limited to 2 MiB, with at most 4,096 archive entries; PDF extraction has a
three-second timeout. Documents still use the existing 10 MiB upload limit.
No extracted text is persisted. Replacement, trash, and current access are
rechecked; obsolete versions cannot supply search matches.

Results stay grouped by source record, with a plain-text matching excerpt.
Content search scans at most ten candidate records per page, or stops between
records after six seconds, with a continuation cursor. Load more searches older
records. Source failures and unsupported files produce visible partial-search
notices; refresh retries them. Recordings and transcripts are not searched.
Use the source filter or open a result in its existing Thura app to read more.
Opening a meeting does not join it; opening an event does not send invitations.

The newest-first timeline uses source record creation times, except timed
calendar events use their start time. All-day events use record creation time
and display their date. Mail timestamps are stored creation times, not sender
Date headers or guaranteed delivery times. The stable cursor includes timestamp,
source, and ID. Scanning is bounded to 200 candidates; an empty batch can have a
continuation when candidate email text does not parse to an exact address.

Every request checks current workspace membership and source visibility.
Private chats require participant access; personal calendars require ownership.
Drive, mail, and meeting records retain their existing workspace access rules.
Manual links never bypass these rules, including in organization rollups.

Organization is optional and secondary: the rollup includes current saved
contacts whose company names match after trimming and case folding. Shared
source records appear once. This is current affiliation, not employment history.

## Remaining scope from issue #7

Saved contacts are the initial manual funnel. Automatic suggestions, accept/
dismiss/snooze, archive state, multiple identities, automatic-match exclusions,
and first-class organization records remain follow-up work. Historical
organization membership needs explicit affiliations before historical rollups.

## Routes

Open `/app/loop`. Contact timeline, source catalog, and manual-link API routes use `/api/v1/workspaces/{workspaceId}/contacts/{id}/loop` (plus `/sources` and `/links`).
