# Debian Postfix transport

Start with [the server installation guide](install-server.md) for DNS/MX, Mailgun sending, administrator setup and user invitations. This document covers the receiving/SMTP transport.

Thura defaults to the maintained Postfix package available in Debian-family
distributions. Deployment supplies every hostname, domain and mailbox; the app
has no fixed public origin. Postfix is a separate SMTP service with a durable
queue. Thura remains a Līdza Go application using the official mail pack.

The supplied container uses Debian 13's pinned base image and installs the
distribution's current Postfix/Python/CA packages. Rebuild for security updates;
record the resulting image digest and package versions for a release. The
integration fixture tested Postfix 3.10.13. The same adapter can run beside
Debian's native Postfix package on a dedicated mail host.

## Configure a mailbox

1. Provision a mailbox through `app_provision_mailbox`, recording its UUID,
   canonical address and optional credential prefix. See [mail connectors](mail-connectors.md).
2. Privately copy [relay.example.json](../deploy/postfix/relay.example.json) to
   an operator-owned file outside the checkout. Replace its address, UUID and
   secret. Generate a random secret of at least 32 characters and seal the same
   value as the prefix's `MAIL_INBOUND_SECRET` in Thura. Never commit this file.
3. Set `baseUrl` to the origin the mail host uses to reach Thura. Loopback HTTP
   is allowed on the same host; other hosts require HTTPS. Redirects are refused.
   This internal relay origin is independent of the public `APP_URL` used for
   invitation and recovery links.
4. Add an exact mapping for every accepted address. Multiple domains and
   workspaces can coexist in one map. Domains are derived from these mappings;
   unknown recipients are refused during SMTP. There is no catch-all or automatic
   plus-address expansion. Restart after changing mappings or rotating secrets.
5. Set Thura's official `MAIL_PROVIDER=smtp`, `MAIL_SMTP_HOST=127.0.0.1`,
   `MAIL_SMTP_PORT=2525`, `MAIL_SMTP_SECURITY=none` for the same-host private
   listener. Prefix these names for a per-mailbox connector. Set the global
   `MAIL_FROM` for invitations/recovery separately. Plain SMTP is appropriate
   here only because submission stays on loopback.

The non-secret [production mail profile](../deploy/postfix/thura-mail.env.example)
provides these defaults; replace its system address and merge it into the app's
operator settings. Development remains captured/outbox mail until configured.

Public `APP_URL`, Thura TLS/proxy settings and all external service hostnames
are configured by each operator. A multi-host deployment must configure
authenticated TLS submission on its chosen MTA and use Līdza's SMTP credentials
and `starttls`/`tls` settings. The supplied local listener does not implement a
public authenticated submission service; never broaden `mynetworks` to expose
unauthenticated relay access.

## Container deployment on Linux

Build `deploy/postfix` as `thura-postfix:release`. Protect the private relay
configuration (mode 0600), mount it read-only, and persist the entire queue:

```sh
docker build -t thura-postfix:release deploy/postfix
docker volume create thura-postfix-queue
docker run -d --name thura-postfix --restart unless-stopped --network host \
  -e MAIL_HOSTNAME=mail.example.org \
  -v /srv/private/thura-relay.json:/run/secrets/thura-relay.json:ro \
  -v thura-postfix-queue:/var/spool/postfix \
  thura-postfix:release
```

`example.org` is illustrative: use the operator's actual hostname and domain
in the configuration and DNS. The default binds only `127.0.0.1:2525`. To accept
internet SMTP, set `SMTP_LISTEN=25`, mount the certificate and private key at
`/run/secrets/smtp-cert.pem` and `/run/secrets/smtp-key.pem`. Keep Thura's
private submission port 2525. The added MX listener uses the host's IPv4 interfaces; configure the
firewall deliberately. Only 127.0.0.1 may submit to arbitrary outbound domains.
Other clients may send only to mapped local recipients. The private inbound
adapter runs as the dedicated `thura-mail` account.

Configure MX/A records, reverse DNS, SPF and an appropriate DKIM signer/DMARC
policy for the chosen domains. Review abuse limits, filtering and public TLS.
Postfix's queue and Thura's signed relay do not establish sender authenticity
or internet deliverability by themselves. Outbound uses DNS/MX unless the
operator selects `POSTFIX_RELAYHOST=[host]:port`. That optional upstream must be
a trusted private relay; authenticated upstream relays require native Postfix
SASL/TLS configuration. Blocked outbound port 25 needs an approved submission
relay, rather than a claim of successful internet delivery.

## Native Debian-family host

On a **new dedicated MTA instance**, install `postfix python3 ca-certificates`
through apt, create a system account/group named `thura-mail`, and install
`configure.py` and `relay.py` as mode-0644 files in a mode-0755 `/opt/thura-postfix/`
directory so the pipe account can read them. Copy the supplied systemd
unit to `/etc/systemd/system/thura-postfix.service`. Create a root-owned mode-0600
`/etc/thura-postfix/environment` containing operator values for `MAIL_HOSTNAME`,
`SMTP_LISTEN` and `THURA_RELAY_CONFIG` (the private JSON path). Stop the package's
default service, reload systemd, and enable/start `thura-postfix.service`.

The configurator sets Thura's routing/restrictions and preserves Debian's
standard Postfix services. It owns this instance's main/listener configuration;
do not run it against an existing unrelated production MTA. For an existing MTA,
adapt the exact-recipient transport map and pipe command manually, retaining
the operator's submission, filtering and TLS policy. Debian-derived package
versions need the same fixture acceptance check; only the recorded Debian
version has been exercised here.

## Queue semantics and recovery

SMTP acceptance means Postfix queued the message. The pipe exits successfully
only after Thura returns a successful ingestion response. Timeout, HTTP failure,
invalid credentials or an unreachable app produce EX_TEMPFAIL (75), retaining
mail for Postfix's retry policy. Retry IDs bind the canonical envelope recipient
and raw bytes; timestamps/signatures are refreshed. Exact repeated bytes for
one recipient intentionally collapse into one Thura item. New SMTP submissions
usually add different Received headers and are separate deliveries; matching
Message-IDs alone are not a deduplication guarantee. Public inbound SMTP and
raw ingestion are bounded to 12 MiB; Thura applies its additional MIME/body/
attachment bounds. Private submission allows 16 MiB of MIME wire data so a
valid 10 MiB uploaded attachment survives base64 overhead. This is an outbound
submission allowance, not an increased inbound API limit. Remote recipients
can impose smaller limits.

Permanent MIME/policy rejection also remains deferred for operator inspection.
Postfix eventually expires undeliverable messages under its queue lifetime and
bounce policy; this is not indefinite retention. Monitor deferred queue age and
failures (`postqueue -p`/`postqueue -j`), inspect logs privately, and use
`postqueue -f` after correcting the cause. Logs contain addresses and queue IDs.
Back up queue state and secret/configuration files in addition to Thura's
metadata/objects; retain ownership/modes on restore. Review ambiguous outbound
attempts before replaying recovered jobs. External SMTP remains at-least-once.

## Local acceptance fixture

Use the synthetic `*_test` database and normal e2e seed only. Build the image
as `thura-postfix:test`, run `lidza test --e2e` to build/seed the test app, export
the private test `DATABASE_URL` without printing it, then run:

```sh
python3 -m unittest discover -s deploy/postfix -p 'test_*.py'
python3 scripts/postfix-fixture.py --pg-container lidza-dev-postgres
```

The fixture owns temporary loopback ports 3002/3003/2525/2526/2527 and its named container,
so stop other test fixtures first. It verifies actual SMTP ingress, retry deduplication after a lost acknowledgment, unknown-recipient and open-relay refusal, queued delivery while the
app is unavailable, queue survival across MTA restart, and official Līdza SMTP
outbound bytes (including a full 10 MiB attachment) through Postfix to a local sink. It stops its app/container and
removes only its synthetic mailbox metadata. Private raw objects and disposable
queue/log evidence remain in ignored test storage. No internet recipient is used.
