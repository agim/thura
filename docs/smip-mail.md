# Domain-discovered SMIP email

SMIP/0.2 adds server-authenticated email delivery to Thura's Mail app. It is a separate, experimental profile; SMIP/0.1 chat/file pairing and Matrix are unchanged. SMIP provides TLS 1.3 in transit, domain identity signatures, immutable messages and signed durable receipts. It is not end-to-end encryption: both servers can read email.

## Enable and advertise

In Server setup → Email service, configure the normal outgoing API provider, enable **Prefer verified SMIP email**, and enter the inbox domain in **Email identity domain**. Save the step: Thura generates a cryptographically random signing identity on the server and stores it in the encrypted draft. The wizard displays the domain, public key identifier, expiry and DNS instructions, never the private seed. Repeat saves preserve the existing identity and trust settings; changing its domain requires explicit identity management rather than silently replacing the key. Publish and restart every node. Existing operator-provided configurations remain supported without exposing their raw JSON in the wizard. `SMIP_MAIL_ENABLED=true` and `SMIP_MAIL_CONFIG` are also supported through sealed settings or environment overrides. Both are application settings; signing seeds are secrets. Keep the same identity and database across workers/nodes.

Advanced operator configuration shape (not required by the wizard; placeholders, never usable keys):

```json
{
  "domain": "example.org",
  "seed": "UNPADDED_BASE64URL_OF_32_RANDOM_BYTES",
  "notBefore": 1792368000,
  "notAfter": 1823904000,
  "requiredDomains": ["partner.example"],
  "deniedDomains": []
}
```

Generate the seed with a cryptographic random source. `notBefore`/`notAfter` are Unix seconds; the interval must include startup time. Keep the seed in encrypted settings, not a repository or shell history. The enabled identity must match each sending mailbox's address domain. The initial implementation supports one sending identity domain per Thura deployment; mismatched senders are refused instead of silently sent another way.

Publish this TXT record:

```text
_smip.example.org. 300 IN TXT "v=SMIP1"
```

Point `smip.example.org` at Thura, with a publicly trusted certificate for that hostname. It must reach Thura with **direct TLS 1.3** (a TCP/TLS passthrough proxy is fine). HTTP forwarding with `X-Forwarded-Proto` is deliberately insufficient. Līdza's built-in HTTPS supports `LIDZA_TLS_DOMAINS`, `LIDZA_TLS_EMAIL`, `LIDZA_TLS_ADDR` and `LIDZA_TLS_HTTP_ADDR`; include the application and SMIP hostnames. The ordinary loopback-HTTP Compose example needs an appropriate TLS deployment before public SMIP can be enabled. Expose HTTPS port 443; do not expose the database or private SMTP submission port.

The fixed endpoints are:

- `GET /api/v1/smip/v0.2/capabilities`: canonical, Ed25519-signed five-minute key/capability document.
- `POST /api/v1/smip/v0.2/mail`: canonical signed SMIP mail envelope; returns a signed receipt after the email and receipt commit together.

Private/local deployments can set operator-controlled `origins` (`{"partner.example":"https://host:port"}`) and `caFile` (a PEM CA bundle) in the secret JSON. Both peers need reciprocal discovery configuration. This is a deployment-level exception to public-address restrictions; users cannot supply endpoints. No TLS verification is disabled. Production discovery rejects private/reserved IPs, pins the resolved addresses for the request, refuses proxies/redirects, bounds requests and verifies document domain/profile/expiry/signature.

## Routing and downgrade policy

Every recipient is deduplicated across To/Cc/Bcc. Before any message submission, Thura freezes its content, selected provider configuration and per-recipient route in Postgres. Verified SMIP wins. Only a domain with a definitive absent DNS advertisement and **no previous verified capability** selects the configured provider. DNS timeouts, SERVFAIL, unsupported SMIP versions, invalid signatures, bad TLS, HTTP failures and unavailable advertised endpoints hold the message. Missing advertisements for a previously verified domain also hold it. `requiredDomains` refuses provider delivery even on first contact.

The persistent registry is visible under Administration → SMIP email domains. It records last check, endpoint, expiry, reason and sticky `everVerified` state, scoped by local identity domain. Negative checks expire after five minutes; verification failures retry after a short backoff. Operators must preserve this registry in backups. Deleting it destroys learned downgrade protection.

Initial absence on unauthenticated DNS cannot prove a domain never supported SMIP. An attacker who suppresses a first-ever advertisement can influence initial provider selection; set `requiredDomains` for partners requiring secure-only delivery. This is not a DNSSEC implementation. Once a recipient route is selected it never switches transport, including after restart, outage, expiry or configuration changes.

## Receipts, retries and duplicate prevention

SMIP attempts commit an uncertain marker and lease before contacting the peer. A retry sends the identical signed envelope, identity and payload. The receiver locks its mailbox and commits message metadata and signed receipt in the same transaction. Replaying that envelope returns the original stored receipt. Conflicting reuse of its identity is refused. HTML/plain text, visible To/Cc, reply references and exact attachment bytes are retained; Bcc never enters a remote payload or stored MIME headers. Accepted means persisted by the recipient server, not read by a person.

API-provider recipients are submitted through the official mail pack. Visible provider recipients share one submission; when only blind provider recipients remain, each gets a separate private submission with its own durable attempt marker. Secure recipients are excluded from that submission. The provider attempt marker commits first. Any timeout, lost response, crash, redirect or failure with an uncertain acceptance outcome is held with **no automatic provider retry** and no SMIP/provider switch. This deliberately trades automatic recovery for preventing duplicate submissions when the provider has no common idempotency contract. A provider acceptance response is not a final-delivery/bounce guarantee. SMTP/Postfix remain explicit configured fallback options; they have the same one-submission uncertainty handling on this path.

For mixed SMIP/provider messages, the pack's current API cannot separate envelope recipients from visible address headers. The provider copy therefore lists only its provider-routed To/Cc recipients (a Cc is promoted to To when required by the pack; a blind-only submission addresses just that recipient); SMIP copies retain the original visible To/Cc. Bcc privacy and no double submission take priority. This limitation can affect Reply all across transports and must be addressed in the framework's envelope API before promising fully preserved mixed-route headers.

Mail shows delivery state and **Delivery by recipient**, including route, hold reason and attempts. Pending SMIP verification/receipts are reconciled by durable jobs each minute, with bounded backoff and a 32-attempt budget. Permanent rejections and exhausted budgets remain held. There is no unsafe resend button for an uncertain provider outcome. Investigate the original provider transaction before any intentional new message. Disabling SMIP cannot turn an existing transmission into a legacy provider retry. Membership revocation fences new attempts under the workspace lock.

This profile currently covers emails composed/scheduled in Mail. Framework account notifications, workspace invitations, calendar notices and setup provider probes retain their existing official mail-pack queue. They do not yet use domain discovery; do not describe the switch as covering every system notification.

Retain original transmissions and receipt ledgers until an explicit audited retention policy is implemented. Outgoing snapshots are bounded to 1,000/128 MiB per workspace; registry entries are bounded to 1,000 per local identity, counting verified and unexpired entries only; expired never-verified entries (an unauthenticated sender naming random domains, provider recipients) are evicted oldest first to make room. Genuine capacity exhaustion answers inbound 503, holds new outbound domains under `registry_capacity` with a five-minute retry and never selects a less secure route. Back up the database, master key and private object storage together. Keep old public signing keys in `keys` through the retry lifetime and preserve original validity intervals; revoked keys are rejected. Publish future public keys at least one discovery cache interval before switching the active signer. Never rotate by rewriting queued signatures.

## Verification

```sh
umask 077
DATABASE_URL='postgres:///thura_correspondence_test?host=/tmp' JOBS_WORKERS=0 go test ./internal/smip ./internal/smipmail ./tests -run 'TestSmip|TestMail'
```

`tests/smip_mail_test.go` starts two complete Thura application instances with separate fresh Postgres databases, separate private storage and real TLS 1.3 listeners. Tests cover both directions, plain/HTML/attachment content, lost receipt and immutable concurrent retry, restart, one inbox copy, registered background workers, API-only recipient selection, uncertain API holds, domain outages and sender revocation. Local test origins and CA replace public DNS/certificate provisioning; this is real local SMIP delivery, not a public-domain interoperability or third-party deliverability certification. DNS tests use an actual local DNS responder for absence, failures, conflicting/versioned records and SSRF rejection.
