# AntiSpamInstitute model assessment — October 19, 2026

**Do not integrate these candidates as Thura's default mail filter.** The small
spam candidate was actually evaluated; the large phishing candidate was inspected
but not downloaded or benchmarked. No mail-intake settings, assistant settings
or production model were changed by this assessment.

## Small spam model: measured failure at the documented threshold

[`spam-detector-bert-MoE-v2.2`](https://huggingface.co/AntiSpamInstitute/spam-detector-bert-MoE-v2.2)
revision `9527f5d20e700a38e500895c8439fc7d7eb721e7` declares Apache-2.0 and
documents 0=Not Spam, 1=Spam. Its safetensors weights are 17,549,312 bytes
(SHA-256 `5b56b515173c54d053bd8fb7af27ad17a56c4716a048ad243c1511dcb4b16303`).
Generic `LABEL_0`/`LABEL_1` in config are not treated as independent label evidence.

The card describes BERT-base plus mixture-of-experts, but the actual
[pinned config](https://huggingface.co/AntiSpamInstitute/spam-detector-bert-MoE-v2.2/blob/9527f5d20e700a38e500895c8439fc7d7eb721e7/config.json)
and safetensors headers show standard BERT-tiny: two encoder layers, hidden size
128 and a two-class linear head, with no expert/router/gating tensors. Config
names `mrm8488/bert-tiny-finetuned-sms-spam-detection`. This is a documentation
discrepancy, not proof of intentional misconduct or identical upstream weights.
The upstream repository has no declared license field; preserve provenance
review before any redistribution rather than assume the new card resolves it.

Actual FP32 ONNX inference ran on Linux arm64 Docker on Apple M5: one CPU quota,
1 GiB memory, zero swap, no network, read-only filesystem, nonroot, one inference
thread. Every overlapping 510-content-token window was read; maximum spam
probability >=0.5 flags a message. This is explicit long-text pooling, not a claim
that the publisher trained this aggregation. No threshold search was performed.

| Validation cohort | Flagged as spam | Interpretation |
| --- | ---: | --- |
| 218 legitimate emails | 205 (94.0%) | False positives; target <=2% |
| 52 ordinary spam emails | 48 (92.3%) | Spam detection |
| 706 phishing emails | 661 (93.6%) | Only flagged as spam; no separate phishing label |

The corpus is Thura's locked validation partition, SHA-256
`8fa79da66de455c7cf745cd3bfd5e2eaf38503996ab63e7a2ff19dd1faa39185`.
Legitimate examples are synthetic; ordinary spam is a conservative cold-service
promotion subset; phishing mixes synthetic mail and real 2024 Nazario messages.
It is not a representative production estimate and does not establish absence
of overlap with undisclosed publisher training data. The final test partition
was not opened for this comparison. Raw messages are not redistributed.

Peak cgroup RAM was 103,649,280 bytes (98.8 MiB), p50 17.9 ms, p95 53.6 ms and
maximum 325.2 ms, with no inference failure/OOM. This is a resource fit, not a
detection-quality pass. The model's two published short-text examples give their
documented classes in native PyTorch. Five native/exported logit comparisons
agreed to within 0.000001. These checks guard against an obvious swapped-label
or broken-export explanation; they do not make the classifier production-safe.
Native PyTorch inference over all 218 legitimate validation emails independently
confirmed the same 205 flags, with zero native/ONNX decision mismatches.

## Large phishing model: not verified within the budget

[`bert-MoE-Phishing-detection-v2.4`](https://huggingface.co/AntiSpamInstitute/bert-MoE-Phishing-detection-v2.4)
revision `1b3c6ff35bc29b810b71cc1fb5d3d1faa49a7aa0` declares MIT but has no
substantive model card, training recipe, dataset ledger or documented evaluation.
The [config](https://huggingface.co/AntiSpamInstitute/bert-MoE-Phishing-detection-v2.4/blob/1b3c6ff35bc29b810b71cc1fb5d3d1faa49a7aa0/config.json)
describes standard BERT-large, 24 layers/1024 hidden units, with benign/phishing
labels and the `ealvaradob/bert-finetuned-phishing` origin. Safetensors size is
1,340,622,760 bytes (about 1.25 GiB), already greater than the whole 1 GiB memory
budget before runtime overhead. Quantization could change feasibility, but no
CPU/RAM/accuracy result has been established here. Do not describe it as fitting
or failing after INT8 quantization without an actual measurement. The named
upstream declares Apache-2.0, whose applicable notices must not be lost merely
because this repository declares MIT.

Neither model alone separates legitimate email, ordinary spam and phishing.
Combining two binary classifiers also requires a separately validated routing
policy; two publisher accuracy claims cannot be combined into a three-class
accuracy guarantee. The small model's very high observed false-positive rate is
sufficient to reject its documented default on this workload. Changing its
threshold would be a new validation experiment, not an accepted integration.

## Reproduce the small-model comparison

Use `scripts/mail-classifier-training/README.md` to prepare the locked validation
partition and CPU image. `antispam.json` pins all inputs; no pickle or remote model
code is loaded. No weights are committed or licensed for redistribution by this
change.

```sh
run=.lidza/local-mail-training
"$run/venv/bin/python" scripts/mail-classifier-training/fetch.py --root "$run" --antispam
"$run/venv/bin/python" scripts/mail-classifier-training/antispam.py --models "$run/antispam-spam" --prepare
python3 scripts/mail-classifier-training/antispam_run.py --root "$run" --output "$run/antispam-validation.json"
```
