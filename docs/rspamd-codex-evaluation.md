# Rspamd and Codex evaluation — October 19, 2026

**Historical evaluation, not the shipping decision.** The user subsequently
authorized shipping Rspamd with durable Junk corrections instead of further model
experiments. See [Junk filtering and corrections](mail-junk.md). This report does
not certify a production Codex/API integration.

The evaluated combined candidate did not pass its original gates. Codex correctly classified all 21
messages that passed the recorded Rspamd stage, plus all 12 existing phishing
challenges. This is a blinded batch pilot, not full independent acceptance or a
production API integration.

The user superseded local-model testing with an explicit request to use Codex or
this session. No further Ollama inference was performed after that instruction.
The completed earlier Gemma experiment is retained privately as separate evidence;
its results do not describe Codex.

## Observed results

| Real message class | Legitimate verdict | Spam verdict | Phishing verdict |
|---|---:|---:|---:|
| Legitimate (10) | 9 | 1 | 0 |
| Ordinary spam (10) | 0 | 10 | 0 |
| Phishing (10) | 0 | 4 | 6 |

Rspamd's decisions are preserved: a phishing symbol or score >=6 holds the
message locally; every other message goes to the model. A model cannot downgrade
a local finding. Of the nine locally held messages, one was legitimate, three
were ordinary spam and five were phishing. Four of those phishing messages were
classified as generic spam. Codex saw the remaining nine legitimate, seven spam
and five phishing messages without their labels, sources, Rspamd scores or prior
model predictions, and classified all 21 correctly. It also classified all eight
instruction-injection and four long-tail phishing challenges correctly.

All 20 harmful real messages were flagged in this pilot. That secondary safety
result does not replace the existing class-separation gates:

- Phishing correctly classified >=95%: already unreachable for the frozen 75-case
  phishing cohort, because four cases retained generic spam labels. Even if every
  remaining case were correct, the upper bound is 71/75 = 94.67%.
- Legitimate false positives <=2%: one of ten pilot messages was incorrectly held
  by Rspamd. The complete 150-message legitimate cohort has not been evaluated by
  the combined pipeline, so the <=2% target is not established.
- Ordinary spam correctly classified >=90% and spam called phishing <=5%:
  10/10 and 0/10 respectively in the pilot; full-cohort gates remain unverified.
- Zero failed/invalid classifications: no unknown or invalid Codex verdicts in
  these 33 items. The separate earlier local-model run returned two unknowns.

The full 300-real-message run was not continued after a conclusive rejection
bound. No thresholds, labels or acceptance definitions were changed afterward.

## Model, timing and isolation

Actual Codex CLI startup metadata reports `gpt-6.1-sol`, provider `openai`, default
reasoning effort `none`, CLI `0.162.0-alpha.17.2`. It used the existing ChatGPT
sign-in. No API token was extracted or copied into Thura.

The 33-item batch took **22.995 seconds total**. This is not a per-email latency
measurement and must not be divided by 33 to claim production response times.
The model received complete bounded, deidentified text under opaque IDs, without
an expected label distribution. It ran ephemerally in an empty directory, with
read-only sandboxing, user configuration ignored, and shell, patch, apps,
delegation and web search disabled. Output IDs were checked for completeness and
uniqueness before scoring. Instructions inside mail were treated as data.

The reused local stage ran Rspamd 4.2.1, Valkey 8.1.1, Unbound and evaluation
clients jointly under **2 vCPU / 2 GiB**, no swap. Peak cgroup memory was
1,055,391,744 bytes (0.983 GiB), with no OOM or memory-limit events. Host Codex was
outside this cgroup. This does not certify the memory or latency of a future
Thura API adapter. ClamAV/attachment malware coverage was not part of this test.

## Evidence and limitations

The corpus consists of publisher-provided real mail: 150 legitimate messages,
75 reviewed ordinary-spam messages and 75 phishing messages, plus the 12 separate
authored regression challenges. Only the initial balanced 30 real messages were
used for this pilot. Their local-stage outcomes had already been exposed; the
Codex model was blinded, but this is not a fresh independent release validation.
Public archive overlap with foundation-model pretraining cannot be ruled out.

Raw historical MIME lacks trusted live SMTP envelope/authentication context.
Current DNS and active phishing feeds cannot faithfully reconstruct deliveries
from earlier years. The local profile excludes restricted reputation feeds,
training and paid threat feeds. These limitations prevent general claims about
Rspamd's accuracy on current live mail.

Source permissions and attribution:

- [SpamAssassin public corpus](https://spamassassin.apache.org/old/publiccorpus/readme.html),
  Justin Mason/contributors and original senders: publisher-provided filter-testing
  purpose permission, not CC0 or an Apache license for message contents. No SMTP
  delivery, public redistribution or message-link following.
- [José Nazario phishing corpus](https://monkey.org/~jose/phishing/README.txt),
  2021 archive, [CC BY 4.0](https://monkey.org/~jose/phishing/LICENSE.txt).
- [Bruce Guenter SPAM Archive](https://untroubled.org/spam/): publisher grants
  unrestricted use for spam research and development.
- Local threat map: [Phishing.Database](https://github.com/Phishing-Database/Phishing.Database),
  pinned active URL list under the project's MIT license. No paid feeds or training.

Private raw MIME, transformed evidence, generated explanations and account/session
logs remain in ignored research storage. The public benchmark JSON contains only
aggregate measurements, limitations, configuration identifiers and evidence hashes.

## Settings and release state

No live provider, workspace toggle, intake route or delivery policy was changed.
The Mail screening toggle remains available. The ordinary assistant's saved
configuration was not replaced with Codex. This test uses Codex only as explicitly
requested; it does not turn the interactive account into a production mail service.

A production release still needs a configured API provider, isolated per-message
requests through the Līdza LLM pack, request budgets, retry/hold handling, and fresh
end-to-end quality/resource validation. No background LLM screening is enabled by
this experiment.

Machine-readable evidence: [benchmark](benchmarks/rspamd-codex-evaluation.json).
