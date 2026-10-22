# Teams and domains on a self-hosted Thura server

Thura is one self-hosted installation, configured by its server administrator.
This is not public signup, SaaS onboarding or delegated domain administration.
The administrator manages every team's domains, mail providers, mailboxes and
SMIP identities in **Settings → Teams and domains**. Owners and members keep
the existing workspace-scoped data and membership permissions.

## Setup

1. Sign in as the installation's bound administrator or an explicitly configured
   `ADMIN_USERS` account. Use the installation's main `APP_URL` for administration.
2. Select an existing team, or add a team with a name. The server administrator
   becomes its first owner and can invite the team’s members through the existing
   Members page. There is no separate customer signup or owner-account onboarding.
3. Assign the domains you control. A domain may serve the web app, email, or both.
   Each exact domain belongs to one team. A domain with another team's existing
   mailboxes cannot be reassigned until those addresses are resolved.
4. Configure each email domain independently. Mailgun, SendGrid, Postmark and
   Resend use their official HTTP APIs. SMTP and included Postfix are explicit
   alternatives. SMTP hosts must be listed by the operator in
   `THURA_TENANT_SMTP_HOSTS`; custom HTTP endpoints are not accepted through this UI.
5. Create the team's mailbox addresses, select its primary web domain, and
   activate the team. Configuration is read from the database across nodes;
   changing application configuration does not require restarting Thura.

Administrator assignment is the ownership proof. No DNS TXT ownership challenge
is required for centrally managed domains. Assignment, removal, provider changes,
activation and key rotation are audited. The optional DNS verification mechanism
is restricted to that same administrator; there is no team-admin bypass or
self-service ownership claiming.

Application setup does not create DNS records or certificates. Publish A/AAAA
records for web domains, MX records for incoming mail, and the sending provider's
SPF/DKIM/DMARC records. Configure the HTTPS ingress/certificates for every domain.
Keep the primary installation URL separate from team domains: it remains the
central administration and account-recovery origin.

## HTTPS and SMIP

Each enabled email domain has a distinct generated Ed25519 identity. Private
seeds and provider credentials are encrypted under the installation master key,
bound inside the ciphertext to their team and domain. Settings return only
credential-presence flags and public key metadata. Rotation retains historical
public keys so already signed deliveries can still be checked.

For `example.org`, publish TXT `_smip.example.org` = `v=SMIP1`, and point
`smip.example.org` to Thura. SMIP requires **direct TLS 1.3**, including through a
TCP passthrough proxy. Ordinary HTTP forwarding and `X-Forwarded-Proto` do not
satisfy this requirement. Which hostnames the HTTPS listener must cover, for the
built-in listener and for the deployment ingress, is stated once in
[SMIP email: transport hostnames](smip-mail.md#transport-hostnames). Updating
ingress/DNS/certificate configuration remains an operator deployment task; the
domain's SMIP identity shows a read-only status (TXT found or missing, where
`smip.<domain>` points and whether that is this server, TLS 1.3 reachable) that
never blocks publishing.

Public SMIP requests select the identity by the configured hostname. Requests to
operator-pinned private origins may additionally carry `X-Thura-SMIP-Domain` for
virtual hosting; it cannot cross an already-bound team host or bypass packet,
key, recipient or TLS verification. The header is routing metadata, not an
identity proof. Public capability documents never contain private keys.

Verified SMIP is preferred when enabled. Recipient routes, content and provider
credentials are frozen in the durable outbox. Failed discovery or a previously
verified recipient never silently downgrades. Disabling secure transport holds
existing secure routes; it never resubmits them through the provider. Uncertain
API submissions remain held instead of being automatically sent again.

## Receiving and links

The included Postfix sidecar uses the existing private token-authenticated
loopback configuration endpoint. It receives only exact enabled mailbox mappings
for activated teams and obtains per-domain inbound HMAC secrets through that
private endpoint. Disabling/removing a team domain removes its receiving route
without disabling other teams. The operator must enable the Compose Postfix
service and configure its mail hostname/TLS once in Server setup. Sending through
an HTTP API can be combined with Postfix receiving.

Signed raw-MIME relays remain supported. For an external relay, turn off Postfix receiving and set a relay signing secret
in the Email step (at least 32 random characters); configure the same secret in
your operator-controlled raw-MIME relay. The value is write-only and cleared
from the form after saving. Generated Postfix secrets stay inside the private
server/sidecar boundary. Provider webhooks are not
silently accepted as unauthenticated email.

Invitations, calendar replies/reminders and meeting links use the team's primary
web domain. Team notifications use the team's configured mailbox and durable mail
outbox, including its SMIP preference. Installation-wide password reset and
verification mail remain installation account operations using the deployment
mail provider and main origin.

## Isolation and compatibility

HTTP `Host` selects a configured team; forwarded headers do not. Team hosts reject
requests for other workspace IDs, even when an account belongs to both teams.
Membership is still checked. Signed share/invitation/meeting/calendar grants must
also match the bound host. Unknown hosts fail closed. `THURA_PUBLIC_HOSTS` is an
explicit comma-separated list of operator-controlled aliases for the main app;
never put team domains in it. Loopback remains available locally; private IP
access is allowed only in development mode unless explicitly configured as an
alias. Cookies remain host-only; there is no cross-domain session-sharing trick.

Existing installations retain their explicitly configured legacy mailboxes and
single SMIP identity until the administrator assigns a team configuration.
Managed teams never fall back to those deployment credentials when their own
configuration is disabled, absent, unapproved or incomplete. Existing files,
messages, memberships and mailbox IDs are preserved.

There is no subscription, billing, public registration or paid service added.
Public DNS/TLS and third-party provider deliverability require the operator's real
domains and credentials; local fixtures do not certify those external services.
