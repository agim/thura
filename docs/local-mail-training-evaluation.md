# Local three-class training — October 19, 2026

**Decision: do not integrate either locally trained candidate.** Both run within
1 CPU / 1 GiB, but the quality gates fail. No trained candidate was activated,
no model replaced the AI assistant, and the Mail settings toggle is retained.
Training and testing used this Mac and local Docker; no paid cloud service ran.
These are TF-IDF/logistic regression and DistilBERT experiments, not Llama training.

## Dataset and rights

The pinned source ledger and reproduction instructions are in
`scripts/mail-classifier-training/README.md`, `sources.json` and `base.json`.
Sources are the publisher-declared Apache-2.0 LLMGen English synthetic emails,
José Nazario's CC BY 4.0 phishing archive, Bruce Guenter's explicitly permitted
spam research archive, and original Apache-2.0 training contrasts. Preserve each
source's own license/attribution; the combined data is not uniformly Apache.
The original messages and model weights are not committed or redistributed.

SpamAssassin mirrors with unsupported CC0 labels and mixed aggregates without
source-specific permission were excluded. The spam archive contains phishing
as well as ordinary spam, so only unambiguous unsolicited service promotions
were admitted to the ordinary-spam class; reward, credential, financial and
impersonation ambiguities were excluded rather than silently relabelled.

| Locked partition | Legitimate | Ordinary spam | Phishing | Total |
| --- | ---: | ---: | ---: | ---: |
| Training | 1,059 | 89 | 2,500 | 3,648 |
| Validation | 218 | 52 | 706 | 976 |
| Final test | 184 | 102 | 755 | 1,041 |

Training includes 384 original contrast variants belonging to 24 families. They
are training only, not independent evaluation cases. Global exact hashes,
subject templates and approximate five-word shingle groups are disjoint between
partitions. Later real phishing stays held out: 2022–23 train, 2024 validation,
2025 final test. Older overlapping campaigns are dropped. Hashes, licenses,
source IDs and exclusions are recorded in the generated dataset manifest.

Important limits: legitimate held-out mail is synthetic, ordinary spam covers
only a narrow service-pitch domain, approximate grouping misses some semantic
campaigns, and image-only/insufficient readable evidence is excluded. Publisher
permission declarations are tracked, not presented as a complete independent
copyright audit. These results cannot certify real-inbox false-positive rates.

## Training and selection

The baseline fits word/character TF-IDF vocabularies and balanced logistic
regression on training only. It takes about 3.5 seconds locally. Coefficients and
vocabularies use JSON/NumPy, not executable pickle files.

DistilBERT starts from the pinned Apache-2.0 base, trains every layer for three
epochs on Apple MPS, and takes 1,780 seconds (29.7 minutes). Seed 42, batch four,
class-weighted cross entropy, AdamW, warmup/decay and gradient clipping are fixed.
Overlapping windows include the whole evidence, with identical document pooling
in training and inference. Best native validation checkpoint is epoch three.
Training uses the Mac's GPU/memory; the deployment inference limit is measured
separately in Docker. Do not describe this as training in a 1 GiB container.

| Full validation result | Baseline | DistilBERT, native FP32 epoch 3 |
| --- | ---: | ---: |
| Phishing correctly identified | 696/706 (98.6%) | 699/706 (99.0%) |
| Legitimate messages incorrectly flagged | 0/218 | 0/218 |
| Ordinary spam correctly identified | 44/52 (84.6%) | 42/52 (80.8%) |
| Ordinary spam wrongly called phishing | 0/52 | 6/52 (11.5%) |

Both fail predeclared spam separation gates (>=90% spam recall; <=5% spam called
phishing). The baseline has the better validation selection rank. Its choice and
thresholds (phishing 0.7, spam 0.5) were written before final test predictions were
read. No thresholds/model choice changed after viewing that test.

## Final test versus known regression checks

The frozen baseline was evaluated once on the 1,041-message final partition:

- Phishing: 737/755 (97.6%) identified as phishing; 16 missed and two called spam.
- Later real 2025 phishing: 405/422 (96.0%) identified as phishing.
- Legitimate synthetic messages: 0/184 incorrectly flagged.
- Ordinary spam: 99/102 (97.1%) identified; two missed and one called phishing.

That partition passes its numerical gates, but does **not** establish overall
acceptance. On the unchanged 76-case authored regression/challenge fixture, the
baseline falsely flags 5/32 legitimate emails (15.6%), identifies 30/32 ordinary
phishing examples, catches all eight embedded-instruction examples, and misses
one of four long-tail phishing requests. These fail the legitimate-mail and
challenge gates. The authored fixture is known regression data, not a population
accuracy estimate. Its failures cannot be hidden by the larger aggregate score.

## Actual deployment resource measurements

Linux arm64 Docker on Apple M5; exactly one CPU quota, memory.max=1,073,741,824,
memory.swap.max=0, no network, no ports, read-only filesystem/mounts, nonroot,
no capabilities. Reports verify Docker configuration, actual cgroups and no OOM.
Host training/other services can affect elapsed time; this is not a speed promise
for every x86 cloud vCPU.

| Run | Peak cgroup memory | Inference p50 / p95 | Quality decision |
| --- | ---: | ---: | --- |
| Baseline, full final test | 98.8 MiB | 0.91 / 2.06 ms | Overall rejected by validation/regression failures |
| Selected epoch-3 INT8 DistilBERT, 51-case CPU pilot | 390.7 MiB | 315 / 1,898 ms | Rejected: 18/19 phishing, 12/16 spam |

The selected neural CPU pilot contains 16 examples per class plus the longest
validation messages not already selected. Thresholds are frozen from full native
validation (0.99/0.5), not tuned on that pilot. Zero of 16 synthetic legitimate
messages are flagged; maximum inference is 3.15 seconds. It is a resource/stress
pilot, not full final-test performance. Exported INT8 file is 138,867,923 bytes;
FP32 export is 267,945,901 bytes. Large embedding tables remain FP32.

An earlier epoch-1 INT8 export also completed the full 976-case CPU validation:
94.5% phishing recall and 71.2% spam recall, with no legitimate flags. It is not
the final selected epoch-three model and its results must not be attributed to
that checkpoint. Quantization can change decisions; resource fit is insufficient.

## What needs improving

Acquire more properly permitted real legitimate/security/business mail and
ordinary spam with auditable labels, including opt-in marketing hard negatives.
Do not assume every archive spam message is phishing, or every security-related
message is malicious. Keep unknown/insufficient evidence for review. Add
campaign/source-separated evaluation and fresh held-out examples before another
training cycle; do not repeatedly tune against this exposed final set.

The separate generic Llama 1B and AntiSpamInstitute phishing comparisons use a
newly authorized 2 GiB inference budget. They are different candidates, not an
undisclosed memory increase for the results above. No Llama fine-tuning has run.
