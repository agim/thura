# Mail connectors

An operator assigns a mailbox with `app_provision_mailbox` (`workspaceId`, `name`, `address`, optional `configPrefix`). The address is the sender; members cannot override it. A prefix such as `SALES_` selects sealed `SALES_MAIL_PROVIDER`, `SALES_MAIL_API_KEY`, `SALES_MAIL_DOMAIN`, and the other official Līdza mail settings. An empty prefix uses the application's `MAIL_*` settings. Invitation/recovery delivery uses the application's own mail pack independently.

Supported outbound providers are the official pack's SMTP, Mailgun, SendGrid, Postmark, and Resend transports. Local log/outbox capture is visibly labelled as not delivered. Queueing commits the draft state and job together. The default send time is ten seconds ahead; scheduled sends can be cancelled before their deadline. Message row locking serializes send/undo/edit. Provider errors retain a failed state and jobs retry. Provider acceptance followed by a lost database commit can cause duplicate delivery: this is at-least-once transport, not an exactly-once guarantee.

## Inbound relay contract

Point the trusted SMTP edge or hosted-provider adapter at `POST /api/v1/inbound/{mailboxId}`. The request body is the complete RFC 5322 message (maximum 12 MB). Configure a random secret of at least 32 characters as the mailbox prefix's `MAIL_INBOUND_SECRET`, through sealed credentials. Headers:

- `X-Thura-Timestamp`: current Unix seconds, within five minutes.
- `X-Thura-Delivery`: stable provider delivery ID, 1–200 characters.
- `X-Thura-Signature`: hexadecimal HMAC-SHA256 of the bytes `timestamp + "." + deliveryID + "." + rawMessage`.

The signature binds all message bytes and the delivery ID. Repeated deliveries return the original item; each mailbox serializes ingestion. The relay must authenticate its upstream provider, preserve the delivery ID on retry, and refresh the signed timestamp. This contract is implemented and tested; native provider webhook adapters and an SMTP listener are separate deployment components, not part of the app binary.

Raw MIME and attachments remain private in the official storage pack (local or S3). Uploads are limited to ten attachments and 10 MB combined; inbound MIME parsing also limits depth, parts and decoded body size. Downloads check current workspace membership and attachment ownership. The reader strips active/remote HTML content and renders it in a sandbox with a deny-by-default CSP. External images and links are blocked. No end-to-end encryption claim is made.

Storage and database transactions are separate: an interrupted inbound transaction can leave unreferenced private objects. A production operator should apply an orphan-retention cleanup policy. Do not automatically delete hash-addressed raw objects on rollback because another delivery may reference the same bytes.
