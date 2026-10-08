# Implementation status

The uploaded `Orbit_Workspace_React (1).zip` now runs inside the published Līdza application. Its source was inspected and imported into `src/workspace`; its built assets, dependency lockfile, standalone server, and setup instructions were not adopted. No Rails runtime or local Līdza checkout is used.

## What works

- `/workspace`: Thura-branded six-app preview, preserving the prototype's layout, browser-local state, themes, Contacts-to-Chat and Calendar-to-Meet interactions. A persistent banner describes its simulated behavior. Its storage namespace is `thura:demo:v1:`; it never contains live account data or credentials.
- `/contacts`: real sign-in, workspace selection, shared contacts with create/edit/delete/search/favorites, persisted through typed Go APIs to Postgres. All frontend API requests use the generated Līdza client. Signing out clears the client query cache.
- Public registration and external identity providers are disabled. Operators provision initial owners; workspace administrators invite members by email. Password sign-in is a provisional starting point while the sign-in brief question remains open.
- Workspace membership is read from `auth_member` for every request. Recognized owner/admin/member roles all read and edit workspace contacts. Unknown roles and app-wide scopes grant no workspace access. Contact mutation queries include both workspace and contact IDs.
- `/members`: roster, invitations, revocation, role changes, and member removal. Owners manage all roles; admins invite/remove ordinary members. Workspace locking protects the last owner, including simultaneous removals.
- `/invite`: one-time invitation acceptance for new or existing accounts. Tokens are hashed, expire in 72 hours, and are delivered through the official mail/jobs packs. New account creation, verified-email marking, membership, and acceptance commit atomically. Existing accounts must sign in as the invited email. Old invitations cannot change an existing member's role; removing the inviter's authority invalidates their outstanding invitations.
- `/forgot`, `/reset`, `/verify`: frontend account recovery and verification flows backed by Līdza authentication.
- The operator can create a workspace with an existing account as its owner. Workspace and owner membership are inserted in one transaction.

## Run and provision

Run `lidza install`, then `lidza dev`. Open `/workspace` for the sample or `/contacts` for real contacts. The cloud environment uses `/workspace/.lidza-tools/env` for its toolchain and `/workspace/.lidza-tools/start-services.sh` for Postgres and Valkey.

Use the application's local `lidza mcp` tools for initial operator provisioning:

1. `app_provision_account`: `email`, `name`, and a strong `password` (at least 12 characters; Līdza's password policy also applies). Supply the password privately, never in committed files or saved command examples. This creates an account without opening a session or sending an email.
2. `app_create_workspace`: `name` and `ownerEmail` of that account. This grants the owner role. Repeating this operation creates another workspace; it is not an idempotent upsert.
3. Sign in at `/contacts` with that account.

These tools carry operator authority and have no public HTTP API route. Keep any `LIDZA_MCP_TOKEN` private; remote MCP access, if explicitly enabled, has the same operator authority. No production account has been provisioned by this implementation.

## Validation and limits

`lidza check --json`, `lidza test`, and `lidza test --e2e` are the validation commands. Go integration tests exercise sign-in, blocked public registration, contact validation and CRUD, workspace isolation, contact-ID substitution, and membership revocation. Browser tests cover the original scaffold, six preview apps, contact/chat and calendar/meeting handoffs, local persistence, phone overflow, refused sign-in, and real persisted contact CRUD plus sign-out.

Validation includes role escalation attempts, invitation expiry/revocation/replay, existing-account identity checks, and concurrent acceptance/last-owner removal. Implementation slices are committed and pushed after local validation; GitHub Actions results do not block further work. Remaining brief questions stay explicitly open.

The imported prototype remains JavaScript, checked by ESLint, while the new pages and API boundaries are strict TypeScript. `allowJs` enables the incremental port; it does not enable JavaScript type checking. Its custom CSS is scoped to the preview. New product pages use the starter's Tailwind tokens. The preview mounts after hydration to prevent server-rendered sample state from overwriting saved browser data.

Contacts currently show a maximum of 500 rows without pagination. The real Contacts page is a functional first slice, not a completed port of the preview's contact design. Mail transport, SMTP ingress, attachments, server-backed drafts, federation, shared file storage/editing, real meetings, and SMIP remain pending. Production invitation/recovery delivery requires the operator's mail credentials and public APP_URL; development captures emails and tests use the outbox provider.

Next: server-backed mailboxes/drafts and the per-mailbox connector implementation. Continue the remaining brief questions as those decisions become necessary; existing unanswered fields are not invented or marked skipped.
