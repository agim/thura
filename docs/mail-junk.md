# Junk filtering and corrections

Thura ships **Postfix and Rspamd together in the `postfix` container**. Postfix
keeps its durable mail queue; Rspamd provides local spam rules and Bayesian
learning from user corrections. The compact deployment runs Valkey in the mail container and persists its
learning data in a separate volume. No fine-tuned Llama model, custom training pipeline or paid feed
is required by this deployment.

In the Spam/Junk mailbox, open a message and select **Not spam**. Thura restores
it to Inbox, retracts your spam complaint, fences any in-flight screening result,
and saves a correction in the same database transaction. **Mark safe** is an API
alias, and the administrator's **Release** action also submits a safe correction.
**Report spam** moves the message to Junk and submits the opposite correction.

Learning runs in a background job. The status distinguishes **Learning queued**,
**Spam filter updated**, **Learning will retry**, and **Correction saved**. A
filter outage does not undo the user's Inbox restoration. Retries and repeated
clicks do not double-count a sample; changing the decision reverses the prior
classification. Corrections are scoped to the team, and dispatch rechecks the
actor's current membership. A correction never silently allowlists a domain or
sender. Required administrator approval and detected-malware blocks still apply.

Rspamd retains its standard minimum sample/token safeguards: currently 200 learned
examples per class before Bayesian scores are used, and 11 tokens per message.
Rules operate immediately while statistical learning accumulates. Very short
messages still return to Inbox and retain the user's correction, but are shown
as **Correction saved** rather than falsely claiming statistical learning or
retrying forever. Historical imports without raw MIME can learn from a stable
reconstruction of their stored text/HTML. An error reading an existing raw source
is retried rather than hidden by reconstruction.

## Deployment

For a new deployment:

```
python3 scripts/init-compose.py
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

For an existing private `.env`, add only the new generated controller secret:

```
python3 scripts/init-compose.py --upgrade-compact
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

The upgrade preserves all existing values. It requires `.env` to be a private
regular file; no secret is printed. The setup wizard exposes the service origins
and controller secret for independently managed deployments. Compose supplies:

| Setting | Value |
|---|---|
| `MAIL_RSPAMD_URL` | `http://127.0.0.1:11333` |
| `MAIL_RSPAMD_CONTROLLER_URL` | `http://127.0.0.1:11334` |
| `MAIL_RSPAMD_PASSWORD` | Private generated `THURA_RSPAMD_PASSWORD` |

The combined mail container shares Thura's network namespace, so these loopback
URLs work from Thura. Neither Rspamd port is published. Controller learning
requires the password. The supervisor stops both services if either child exits;
Compose restarts the container. Its health check verifies both supervised processes
and Rspamd readiness. Mail provider and receiving choices remain in the existing
setup wizard; Postfix receiving can still be combined with API-based sending.

The default [compact profile](compact-deployment.md) puts Postfix, Rspamd,
ClamAV and Valkey under one 1792 MiB / 0.75-vCPU limit. Learning remains in
Valkey database 0; app cache and realtime use databases 1 and 2. The complete
four-container stack has 3584 MiB of limits, leaving 512 MiB for a 4 GiB host.
Back up the mail queue, signatures, learning volume, database and private storage.
The earlier split arrangement remains in `deploy/compose.separate.yaml`.

The default profile uses free local rules and explicit user feedback. Restricted
third-party reputation feeds and automatic training from model guesses are off.
Operators may configure additional appropriately licensed feeds separately.

## Optional AI

The **Screen incoming email with AI** toggle remains in Mail settings. When
enabled, local checks run first and locally flagged messages skip the model;
remaining mail uses the provider configured in the existing AI settings through
Līdza's LLM pack. Screening requests have no assistant history or tools. Request
budgets and failed required checks retain mail for retry/review. Known malware
cannot be released by an AI verdict or **Not spam**.

This feature does not reuse a Codex interactive account as a production API key,
switch the operator's model or claim perfect detection. Earlier model experiments
remain historical evidence, not a requirement to train or bundle a local model.
