# Mail connectors

An operator assigns a mailbox with `app_provision_mailbox` (`workspaceId`, `name`, `address`, optional `configPrefix`). The address is the sender; members cannot override it. A prefix such as `SALES_` selects sealed `SALES_MAIL_PROVIDER`, `SALES_MAIL_API_KEY`, `SALES_MAIL_DOMAIN`, and the other official Līdza mail settings. An empty prefix uses the application's `MAIL_*` settings. Invitation/recovery delivery uses the application's own mail pack independently.

Supported outbound providers are the official pack's SMTP, Mailgun, SendGrid, Postmark, and Resend transports. Local log/outbox capture is visibly labelled as not delivered. Draft edits autosave after a pause with serialized writes; failures remain visibly unsaved. Wait for “Draft saved” before closing. Concurrent browser editors are last-write-wins. Queueing commits the draft state and job together. The default send time is ten seconds ahead; scheduled sends can be cancelled before their deadline. Message row locking serializes send/undo/edit. Provider errors retain a failed state and jobs retry. Provider acceptance followed by a lost database commit can cause duplicate delivery: this is at-least-once transport, not an exactly-once guarantee.

Folder listings support cursor paging and literal server-side search; defaults are 50 messages, maximum 200 per request. Search is not limited to the first page. Concurrent edits may move rows between pages.

## Inbound relay contract

Point the trusted SMTP edge or hosted-provider adapter at `POST /api/v1/inbound/{mailboxId}`. The request body is the complete RFC 5322 message (maximum 12 MB). Configure a random secret of at least 32 characters as the mailbox prefix's `MAIL_INBOUND_SECRET`, through sealed credentials. Headers:

- `X-Thura-Timestamp`: current Unix seconds, within five minutes.
- `X-Thura-Delivery`: stable provider delivery ID, 1–200 characters.
- `X-Thura-Signature`: hexadecimal HMAC-SHA256 of the bytes `timestamp + "." + deliveryID + "." + rawMessage`.

The signature binds all message bytes and the delivery ID. Repeated deliveries return the original item; each mailbox serializes ingestion. The relay must authenticate its upstream provider, preserve the delivery ID on retry, and refresh the signed timestamp. This contract is implemented and tested. The default SMTP listener and signed queue adapter are supplied as the separate Debian Postfix deployment; see [Postfix](postfix.md). Hosted-provider-native webhook adapters remain separate integrations.

Raw MIME and attachments remain private in the official storage pack (local or S3). Uploads are limited to ten attachments and 10 MB combined; inbound MIME parsing also limits depth, parts and decoded body size. Downloads check current workspace membership and attachment ownership. The reader strips active/remote HTML content and renders it in a sandbox with a deny-by-default CSP. External images and links are blocked. No end-to-end encryption claim is made.

Storage and database transactions are separate: an interrupted inbound transaction can leave unreferenced private objects. A production operator should apply an orphan-retention cleanup policy. Do not automatically delete hash-addressed raw objects on rollback because another delivery may reference the same bytes.


Recipient suggestions search the shared workspace Contacts directory by name/email after two typed characters. Select a suggestion by clicking or tabbing to its button and pressing Enter; earlier comma-separated recipients are retained. Suggestions are optional and manual entry remains available.

Forward creates a new draft with the source's private attachments, within the same mailbox. Replies omit attachments. Forwarded files receive separate metadata IDs and retain references to immutable stored bytes. The existing attachment limits and current membership checks still apply. Future retention/purge jobs must check every attachment reference before deleting a stored object; deleting or trashing a source must not invalidate a forward.

## Organization and conversations

Current members share mailbox labels (at most 50 per mailbox, 40 UTF-8 bytes per name) and a mailbox signature (up to 5,000 UTF-8 bytes; empty clears it). Label creation is serialized, case-insensitive duplicates are refused, assignment is idempotent, and filtering is combined with the current folder/search. Deleting a label removes assignments without deleting mail. Signatures apply to newly created drafts/replies; existing drafts retain their saved body.

Replies derive recipients server-side from the authorized source message, exclude the mailbox's address, deduplicate visible recipients and omit Bcc. Reply-all also includes original visible recipients. Replies preserve normalized In-Reply-To/References headers; forward starts a new conversation and retains attachments. Received Message-ID and parent references are stored and correlated within the same mailbox. Conversation pages have the same bounded limits as folder listings; custom or missing sender headers can prevent correlation. A provider owns the outgoing Message-ID, and matching depends on its returned message identifier. This is header-based threading, not subject-only merging.

Draft attachment removal checks current membership, draft status and attachment ownership, and removes the reference only. Immutable bytes remain available to other drafts/forwards; future mail-object retention must respect all references.
