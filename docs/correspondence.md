# Correspondence

Correspondence is Thura's internal, person-first timeline and search feature.
Open it from the workspace navigation or a contact's Correspondence button.
Save a contact to begin organizing their history; no organization is required.
An external contact is a record, never a login or an invitation.

## First implementation slice

- Search the full contact directory by name, email, or organization.
- Read a paginated, newest-first mail timeline across the workspace's mailboxes.
- Search subject, plain text, sender, and recipient; open an original message
  in Mail, including its existing attachment downloads and reply actions.
- Match parsed From/To/CC/BCC mailbox addresses exactly, case-insensitively.
  Names in display headers or message bodies do not identify a contact.
- Exclude drafts, pending/failed delivery, spam, and trash. Development captures
  are explicitly marked as not delivered.
- Switch to an optional organization rollup using the current saved contacts
  whose company names match after trimming and case folding.

The timeline uses the stored message creation timestamp. It does not claim to
be the sender's Date header or the delivery time. Each API call checks current
workspace membership; mail remains shared according to existing mailbox rules.
The candidate scan is bounded to 200 rows per request. An empty batch may have
a continuation; Load more continues without skipping subsequent candidates.

## Remaining scope from issue #7

Calendar/Meet, chat, and Drive relationships are not yet included. These need
source-specific identity links and permission checks; incidental name matches
must not associate documents or conversations. Source additions should retain
the same timeline/search surface and original-item handoffs.

Saved contacts are the initial manual funnel. Automatic suggestions, accept/
dismiss/snooze, archive state, multiple identities, correction/exclusion tools,
and first-class organization records remain follow-up slices. The current
company field groups by current affiliation, so it cannot preserve employment
history. Add explicit historical affiliations before presenting organization
rollups as historical organizational membership.
