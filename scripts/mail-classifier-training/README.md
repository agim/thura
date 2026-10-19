# Free local three-class email training

Research/evaluation tooling, **not an enabled mail filter**. Never promote a
checkpoint merely because training completed or it fits the resource budget.
There are three distinct labels, in this fixed order: legitimate, spam, phishing.

## Rights and provenance

`sources.json` records every downloaded byte count and SHA-256. Raw emails,
checkpoints, ONNX weights and generated partitions stay in `.lidza/`, outside git.
`base.json` pins the Apache-2.0 DistilBERT checkpoint and its actual license file.
No access token, paid API, external training machine or assistant model is needed.

| Source | Publisher's permission | Use here |
| --- | --- | --- |
| [LLMGen English emails](https://huggingface.co/datasets/Dizzzy0x00/LLMGen-Phishing-Email-Dataset/tree/d717e5457b71cb44dd038dafe6a81cea5e0badab) | Apache-2.0 declaration; publisher describes generated emails | Synthetic legitimate/phishing examples, grouped split |
| [José Nazario phishing corpus](https://monkey.org/~jose/phishing/README.txt) | [CC BY 4.0 license](https://monkey.org/~jose/phishing/LICENSE.txt) | Real phishing: 2022–23 train, 2024 validation, 2025 final test |
| [Bruce Guenter spam archive](https://untroubled.org/spam/) | Publisher permits unrestricted use for spam research/development; recorded as `LicenseRef-Guenter-Unrestricted`, not CC0 | Only clear unsolicited service promotions; 2022–23 train, 2024 validation, 2025–April 2026 final test |
| `contrasts.json` | Original Thura contributors' examples, Apache-2.0 | Training only; greeting variants are not independent evaluation cases |

These are documented publisher grants, not an independent audit of every
message author's rights. Preserve attribution and modification notices for the
base and all datasets with any future model distribution. Do not publish raw
mail or treat the combined dataset as uniformly Apache licensed. Some original
phishing messages contain personal information; local preprocessing removes
email local parts/IPs but is not a complete anonymization guarantee.

SpamAssassin mirrors' CC0 claims were rejected because the primary corpus
retains original sender copyright without a matching blanket grant. Mixed-source
MIT-labelled aggregates without per-source rights mapping were also excluded.

The spam archive is not wholly ordinary spam: scams, credential theft and
impersonation occur in it. Selection requires a clear cold service pitch and
excludes ambiguous financial/reward/security language. This makes labels more
defensible but limits the ordinary-spam domain severely. Legitimate validation
and test mail is synthetic. **Even strong scores cannot certify production
false-positive rates on real inboxes.** No private inbox is used without consent.

## Reproduce

Use Python 3.12 and Docker. Training was run locally on Apple MPS; the measured
deployment inference runs in Linux Docker under the requested CPU/RAM limits.
The training machine is not restricted to 1 GiB. Allow several GB for packages,
raw sources, FP32 checkpoint, ONNX export and INT8 copy; this is separate from
runtime memory. Commands below assume a virtual environment at `$run/venv`.

```sh
umask 077
run=.lidza/local-mail-training
python3.12 -m venv "$run/venv"
"$run/venv/bin/pip" install -r scripts/mail-classifier-training/training-requirements.txt
"$run/venv/bin/python" scripts/mail-classifier-training/fetch.py --root "$run"
"$run/venv/bin/python" scripts/mail-classifier-training/prepare.py --raw "$run/raw" --output "$run/dataset"
"$run/venv/bin/python" scripts/mail-classifier-training/augment.py --dataset "$run/dataset" --output "$run/dataset-locked"
"$run/venv/bin/python" scripts/mail-classifier-training/baseline.py --dataset "$run/dataset-locked" --output "$run/baseline"
"$run/venv/bin/python" scripts/mail-classifier-training/train.py --base "$run/base" --dataset "$run/dataset-locked" --output "$run/neural-locked" --epochs 3 --batch-size 4
"$run/venv/bin/python" scripts/mail-classifier-training/export.py --training "$run/neural-locked" --output "$run/onnx"
docker build -t thura-local-mail-evaluation:local scripts/mail-classifier-training
```

The Guenter permission page is a hash-pinned snapshot of a live directory index.
If it changes, fetch fails closed. Retain the reviewed original, or review the new
grant and deliberately version the input manifest/dataset; never ignore a hash
mismatch to keep an old benchmark apparently reproducible. Source archives may
also change; hashes intentionally catch that. Dependencies are version pinned,
not a complete transitive wheel hash lock.

## Split and leakage controls

MIME parsing does not visit links or execute attachments. Bound message sizes,
archive expansion, evidence size and forward-pass windows are explicit. Prefer
readable plain text; extract visible HTML and link destinations, including HTML
incorrectly labelled plain text. Empty/image-only/oversized evidence needs
another scanner or manual review; it is excluded from this text benchmark.

Global exact hashes, repeated subject templates and MinHash/Jaccard five-word
shingles group related copies before assigning partitions. Later forced cohorts
win over older copies; older overlapping rows are discarded. Conflicting exact
labels are excluded. These controls reduce leakage, but cannot identify every
semantic campaign. Row IDs, groups, sources, labels and partition hashes are
recorded. Original contrasts have a held-out duplicate fence and are training only.

Model fitting and vectorizer vocabulary fitting open only training data.
Checkpoint/threshold selection uses validation only. Freeze the chosen model
and threshold artifact **before reading final test predictions**; do not select
another model or tune thresholds on the same final test after a failure.

## Constrained inference

The exported INT8 model and the TF-IDF/logistic regression baseline use no pickle
and no remote model code. Artifact hashes and label order are checked. Transformer
inputs use overlapping 510-content-token windows, without silent tail truncation.
Pooling is identical in neural training and serving: minimum legitimate logit,
maximum spam and phishing logits, then softmax. Scores are not calibrated safety
probabilities. Neural model code/preprocessing hash is bound into its manifest.

```sh
python3 scripts/mail-classifier-training/benchmark.py --models "$run/onnx" --corpus "$run/dataset-locked/validation.jsonl" --kind int8 --phase validation --output "$run/int8-validation.json"
# Freeze the model choice here, using validation only.
python3 scripts/mail-classifier-training/benchmark.py --models "$run/onnx" --corpus "$run/dataset-locked/test.jsonl" --kind int8 --phase test --selection "$run/int8-validation.selection.json" --output "$run/int8-test.json"
```

For the baseline, use `--models "$run/baseline" --kind baseline`. The container
has exactly one CPU quota, 1,073,741,824 bytes RAM, zero additional swap, no
network/ports, read-only mounts/root, no capabilities and a nonroot user. Reports
include Docker configuration, actual cgroups, OOM counters, model fingerprint,
partition hash, per-case predictions/latency, per-source cohorts and confusion.
No message bodies are written into benchmark output. CPU architecture and host
matter: a quota on Apple M5 is not a speed guarantee on every cloud vCPU.

Predeclared final-test gates: phishing recall >=95%, legitimate false-positive
rate <=2%, spam recall >=90%, ordinary spam called phishing <=5%, zero errors,
p95 <=5 seconds and >=5 sequential emails/minute. Later real phishing must pass
95% recall separately. Existing authored instruction/tail challenges are known
regression tests, not an independent population estimate; report them separately
and require their detection gates too. Missing ordinary-spam cases in a regression
fixture are not invented as successful spam detection.

Any required gate failure means **do not integrate**. A future accepted deployment
must use a dedicated private service, preserve the Mail settings toggle, never
fall back to the AI assistant, quarantine required-check failures and leave
antivirus priority intact. Raw licensed data remains local; paid APIs are absent.

Unit checks:

```sh
"$run/venv/bin/python" -m unittest discover -s scripts/mail-classifier-training -p 'test_*.py'
```
