# Mail filters

A mailbox files its new mail by filters, as Gmail does: "mail from this sender
goes under its own label and stays out of my Inbox". Settings → **Mail filters**
(`/app/settings/filters`), or **Filter messages like this** on a received
message (the reader's toolbar and the message's context menu), which opens a
new filter for that sender.

## A filter

| Part | Fields | Meaning |
| --- | --- | --- |
| When a new message matches | From, To, Subject, Has the words | Case-insensitive "contains". Every filled field must match; inside a field a comma or ` OR ` separates alternatives, any one of which matches. From is the sender's name and address (`@acme.example` matches a domain), To covers To and Cc, Has the words searches the subject and the body. A filter with no criterion is refused: it would match everything. |
| Do this | Apply the label, Skip the Inbox, Mark as read, Star it, Move it to Trash | At least one. The label is one of the workspace's labels, or a new one by name (reused when the name exists). Skip the Inbox moves the message to Archive. Trash wins over Skip the Inbox. |
| Also apply to matching mail already in the Inbox | on by default for a new filter | Files the received mail already in the Inbox (the newest 5,000) in the same request and reports how many. |

A label with a filter is the "folder" of that mail: the label view lists every
message carrying it, in any folder but Spam and Trash, and **Show this label
under Mail's folders** pins it in the member's Mail sidebar (pins are personal;
recipe "Pin labels in an app sidebar").

## When filters run

- When a message **reaches the Inbox**: clean at intake, or released to the
  Inbox by screening (`internal/mailabuse`: `Enqueue` and the screening
  worker), in the same transaction. All enabled filters of the mailbox run
  together: every matching filter's label is applied, and the strongest move
  wins.
- Never on mail held as Spam or quarantined, and a filter only ever moves mail
  **out of** the Inbox: it cannot rescue held mail or move it back.
- Mail a filter takes out of the Inbox does not notify; Settings →
  Notifications → Recent notifications says "Skipped: a mail filter moved it
  out of your Inbox". Mail a filter only labels, stars or marks as read still
  notifies.
- Editing or deleting a filter never moves mail it already filed. Turning a
  filter off keeps it for later.
- Deleting a label deletes the filters that file into it (the database
  cascades), so mail is not archived into a label that no longer exists.

## Who may change them

Filters belong to the mailbox (`mail_filter`, at most 100 per mailbox).
Everyone who can read the mailbox sees them. A private mailbox's filters are
its owner's; a shared mailbox files mail for every member, so only workspace
owners and admins create, change or delete its filters (as with its signature
and sender name). Another member's private mailbox is a 404.

## Code

- `internal/mailfilter`: matching and filing (`Match`, `File`, `Arrive`); no
  access rules, called inside the transaction that put the message in the Inbox.
- `internal/mailbox/filters.go`: the settings (`ListFilters`, `SaveFilter`,
  `DeleteFilter`) and their access rules.
- Routes under `/api/v1/workspaces/{workspaceId}/mailboxes/{mailboxId}/filters`.
- `src/components/MailFilters.tsx`: the Settings page; its view (mailbox, the
  open form, a prefilled sender) is in the address (`appViews.filters`).
- Tests: `internal/mailfilter/mailfilter_test.go`, `tests/mail_filters_test.go`,
  `e2e/mail-filters.spec.ts`.
