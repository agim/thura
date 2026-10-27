# Incoming mail protection

All **new** received email uses one pipeline, whether received through verified SMIP, managed Postfix, or the signed inbound-relay API. A signed SMIP receipt means durable acceptance, including quarantine. It does not certify content safety. Raw MIME and attachments remain private.

In **Settings → Mail → Incoming mail protection**, workspace owners/admins can change AI screening, unknown-sender approval, sender/domain hourly limits and the quarantine threshold. Members can inspect warnings and report/retract their own spam complaints; approval, blocking, release and rescan require workspace administration. The message's protection panel uses the same actions on desktop and phone. Held mail is in **Spam**, with a reason and individual AI/antivirus states.

## Policy and reputation

Defaults are 60 messages per sender/hour, 600 per domain/hour, risk threshold 70/100, AI screening enabled, and unknown-sender approval disabled. Domain limits and complaint reputation pool subdomains under the registrable parent domain. These limits are workspace-wide UTC-hour buckets, independent of the destination mailbox. Duplicate deliveries return the original receipt/item before charging another allowance. Rate-limited deliveries return 429 without storing a second message. Postfix retains its queue and retries; SMIP honors bounded Retry-After without provider fallback. Other inbound adapters must retain and retry the same delivery ID.

Verified SMIP and unverified relay identities have separate rate/reputation/approval namespaces. Relay HMAC authenticates the relay, **not** a claimed From address; spoofed relay mail cannot inherit a verified SMIP sender's approval. The bundled Postfix/Rspamd edge validates SPF, DKIM and DMARC using the actual connection; independent relay operators must perform equivalent checks and preserve signed SMTP metadata. DMARC quarantine cannot be cleared by a low spam score or an AI verdict. These counters are not network-edge denial-of-service protection; rotating spoofed addresses/domains still require edge limits.

Reputation is private to each workspace. Reports are unique per actor/message, and repeated reports on the same message count once toward reputation. Three distinct reported messages produce poor sender reputation; ten reported messages across an authenticated-class/domain produce poor domain reputation. Poor reputation contributes risk 80. An explicit block contributes risk 100. Retracting feedback removes only the current actor's complaint. Reports and decisions are audited; deleting an email removes its associated complaint records. There is no global reputation service or automatic expiry/decay in this version.

When unknown-sender approval is enabled, only an explicit trusted identity or a verified SMIP sender exactly matching a current workspace contact/member is known. An address in Contacts alone does not authenticate relay mail. Approving a sender removes the approval requirement; it does not skip scanning or automatically release a suspicious message. Admins explicitly release reviewed messages after required checks complete. Antivirus-positive content cannot be released. Rescan reruns the current policy and returns mail to quarantine. Previously received email is marked **Not screened · historical email**; it is not silently backfilled or falsely labelled safe.

## Scanning and failure handling

Static checks inspect sender display names and lookalike domains against a bounded directory (500 contacts plus 1,000 members), deceptive/obfuscated links, private/IP destinations, executable and macro names/content, active PDF content and ZIP metadata. ZIP inspection is bounded to 512 entries, 50 MiB expanded size and compression ratio 100. Links are **never fetched**, so screening cannot cause requests to attacker-selected/internal destinations. This is structural detection, not a remote URL reputation feed or browser sandbox. The existing sandboxed reader continues blocking external links, remote images and active HTML.

The private ClamAV service scans complete raw MIME, including attachment/archive/mail parsing and phishing signatures. Encrypted archives, scan limits and uninspectable content are treated as unsafe. No attachment bytes are sent to the LLM. Downloads, inline-image rendering and forwarding/sending copies check quarantine state; positive antivirus findings remain blocked even after human review. Existing already-downloaded copies cannot be recalled.

AI receives sender, subject, plain/HTML bodies, attachment names/types and static findings as untrusted evidence. It has no tools, link fetching or mail-sending capability. It must return a validated structured classification; malformed, refused, tool-calling, truncated or oversized responses are failures. Input is limited to 24,000 JSON bytes; larger email stays held for manual handling rather than silently classifying an excerpt. A benign AI verdict never lowers deterministic risk. Hosted providers receive this content automatically and may charge; choose local Ollama to retain inference locally. AI is an additional signal, not a guarantee against prompt injection, phishing or spam.

Mail with pending/unavailable required checks stays in Spam and its attachments remain inaccessible. Workers lease attempts and reconcile crashes every minute. Five failed attempts leave a visible error requiring repair and Rescan; no fake/safe fallback occurs. Settings revisions fence old worker results. Explicit reports/moves/release also fence in-flight results. Turning AI off affects future scan attempts, including queued jobs; use Rescan for previously held/error mail. Disabling AI does not disable static or configured antivirus checks.

## Deployment

The setup wizard exposes infrastructure defaults; existing workspace policy is changed in Mail settings. Keys follow the existing sealed configuration system and environment precedence:

| Setting | Default | Purpose |
| --- | --- | --- |
| `MAIL_AI_SCREENING` | `true` | Initial AI choice for a new workspace policy; does not overwrite existing choices. |
| `MAIL_AI_PER_HOUR` | `120` | Maximum screening job reservations per workspace/UTC hour (1–10,000). Provider-level retries obey the configured LLM retry limit. Excess email waits until the next hour. |
| `MAIL_CLAMD_ADDRESS` | empty outside Compose | Private `host:port` ClamAV INSTREAM endpoint. Blank disables signature scanning, visibly; static inspection remains. |
| `THURA_AI_ENABLED`, `LLM_*` | existing AI configuration | Required enabled model. No model/fake production provider means required AI scans cannot complete. |

`deploy/compose.yaml` builds the nonroot `deploy/clamav` image and connects Thura to `clamav:3310`. No scanner host port is published. Its signature volume persists across upgrades. Allow FreshClam access to the official update service and allocate memory for the signature database (the acceptance container used 3 GiB). Initial signature-download failure prevents scanner startup and keeps mail held. Monitor FreshClam logs/signature freshness and scanner health; the application verifies scan completion but does not independently enforce signature age. Update images/signatures regularly. ClamAV is separately licensed GPL software in its own container; retain upstream notices/source obligations when distributing images.

A required service outage intentionally holds mail instead of delivering it unchecked. Configure the model in AI settings before enabling incoming traffic, or explicitly turn AI screening off for the workspace. Quarantine consumes private mail storage; ordinary retention and edge volume controls are still necessary. No spam/phishing-proof claim is made.

## Verification

`tests/mail_abuse_test.go` covers shared signed-relay/two-server SMIP intake, rate rollback and duplicate receipts, workspace boundaries, approval/complaints, AI opt-out/budget/failure, stale-worker fencing and antivirus/quarantined attachment enforcement. Unit tests cover structural deception, executable/ZIP/PDF detection and the ClamAV framing protocol. `e2e/mail-protection.spec.ts` checks actual desktop/phone settings and review screens. Existing mail reader, autosave and delivery suites remain intact.

For real local services, run `go run ./scripts/mail-abuse-acceptance -scanner 127.0.0.1:13310 -model gemma3:4b` against an operator-owned private ClamAV listener and installed Ollama model. It uses only synthetic content: harmless mail, the harmless EICAR antivirus test and a phishing fixture with an embedded instruction to classify it as safe. It never sends customer email.
