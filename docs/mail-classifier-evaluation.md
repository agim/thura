# Free email classifier acceptance — October 19, 2026

**Decision: reject this candidate for integration.** It fits 1 CPU and 1 GiB
comfortably but fails detection-quality gates. No candidate model was connected
to mail intake, bundled with Thura, or substituted for the AI assistant. The
existing Mail settings toggle is unchanged. No paid service or training was used.

## Candidate, labels and license

Evaluated `cybersectony/phishing-email-detection-distilbert_v2.4.1`, source revision
`6724dec17360ab1f6450553f1d950404f66a5dde`, using onnx-community's conversion at
`8b488fdbc432690dd9323f5fd8119c5f3290b46b`. INT8 weights are 67,538,702 bytes;
FP32 reference weights are 267,959,729 bytes. Both are pinned and SHA-256 checked.

The source model card's example misnames class 1. The source dataset card defines
0=legitimate email, 1=phishing email, 2=legitimate URL, 3=phishing URL. The four-class
model config and measured predictions agree with that distinction. The binary
decision uses the sum of classes 1 and 3 >=0.5; it does not discard the URL classes
or interpret generic `LABEL_1` as safe. Thresholds were not tuned on these results.

The source model explicitly declares Apache-2.0; its DistilBERT base includes the
Apache 2.0 license text. The ONNX conversion identifies the source model but has
no separate license declaration/file. The dataset card has no explicit license
declaration. These checks establish publisher declarations, not an independent
audit of every training example's provenance. No weights or publisher corpus are
redistributed by this change. A future distribution needs applicable license,
attribution and modification notices, plus a documented provenance review.

Primary evidence:

- [Pinned source model card](https://huggingface.co/cybersectony/phishing-email-detection-distilbert_v2.4.1/blob/6724dec17360ab1f6450553f1d950404f66a5dde/README.md)
- [Pinned dataset label definitions](https://huggingface.co/datasets/cybersectony/PhishingEmailDetectionv2.0/blob/afba38f4a7ffbe89259ab68ae7c6426f944496cc/README.md)
- [Base model license](https://huggingface.co/distilbert/distilbert-base-uncased/blob/12040accade4e8a0f71eabdb258fecc2e7e948be/LICENSE)
- [Pinned ONNX conversion](https://huggingface.co/onnx-community/phishing-email-detection-distilbert_v2.4.1-ONNX/tree/8b488fdbc432690dd9323f5fd8119c5f3290b46b)

## Resource results

Actual CPU inference in a Docker Linux arm64 container on Apple M5, batch one,
one ONNX intra/inter-op thread. Docker NanoCPUs=1,000,000,000; cgroup cpu.max
`100000 100000`; memory.max=1,073,741,824; memory.swap.max=0. Neither run had an OOM
event or OOM kill. Network access was disabled and the filesystem was read-only.
No assistant model or external API was involved.

| Measurement | INT8 | FP32 reference |
|---|---:|---:|
| Peak cgroup RAM, including page cache | 259.2 MiB | 465.4 MiB |
| Model-process startup, including imports/hash checks | 0.28 s | 0.91 s |
| Email/challenge median inference | 42.7 ms | 116.5 ms |
| Email/challenge p95 inference | 582.6 ms | 1,390.2 ms |
| Email/challenge maximum inference | 1,719.2 ms | 3,811.2 ms |
| Sequential email/challenge cases/minute | 481.4 | 204.4 |

Inference timings cover tokenization and every window, excluding process startup.
Email timing statistics use 276 email/challenge cases and exclude 200 easier URL
cases. RAM covers the entire 476-case run. Startup is a single observation, not a
cold-cache distribution. This verifies the requested quota on this host; it does
not predict the speed of an arbitrary x86 cloud vCPU or concurrent Thura workload.

## Detection results

| Subset | INT8 | FP32 reference |
|---|---|---|
| 32 authored legitimate emails | 9 incorrectly flagged (28.1%) | 9 incorrectly flagged (28.1%) |
| 32 authored phishing emails | 30 detected (93.75%) | 30 detected (93.75%) |
| 100 publisher legitimate emails | 6 incorrectly flagged (6%) | 5 incorrectly flagged (5%) |
| 100 publisher phishing emails | 98 detected (98%) | 98 detected (98%) |
| 100 publisher legitimate URLs | 0 incorrectly flagged | 0 incorrectly flagged |
| 100 publisher phishing URLs | 99 detected | 99 detected |
| 8 phishing emails with embedded override instructions | 8 detected | 8 detected |
| 4 phishing requests after long benign text | 0 detected | 0 detected |

Examples of errors include legitimate account-security reminders and receipts
being flagged, and a request to email work passwords being missed. The long-tail
cases were processed in overlapping 510-content-token windows: tail tokens were
included, not silently truncated. The pooling strategy is not claimed to have
been trained for this use, and it did not provide reliable long-email detection.

The two models agree on 473/476 binary decisions (99.37%). They differ on two
authored legitimate emails and one publisher legitimate email. Identical error
counts do not mean identical errors. The major quality failure persists in FP32;
it cannot be attributed solely to quantization.

Predeclared gates were >=95% phishing recall, <=2% legitimate-email false
positives, zero inference errors, p95 <=5 seconds, and >=5 sequential cases/minute.
Resource/performance gates pass; false-positive and authored/challenge detection
gates fail. Scores near 1.0 occurred on false positives, so confidence thresholds
must not be described as calibrated safety probabilities.

The authored cases are a small smoke test, not a representative population
estimate. Publisher data is its own test split and is not independently held out
by Thura. We deliberately report email and URL results separately. These results
are sufficient to reject this candidate, not to certify any production model.

## Reproduction and next requirement

`scripts/mail-classifier-benchmark/README.md` documents pinned artifact preparation,
bounded no-network CPU evaluation, corpus selection, FP32 comparison and unit
checks. `docs/benchmarks/mail-classifier-2026-10-19.json` records aggregate metrics,
hashes, image identities, exact limits, cgroup counters and acceptance failures.
Raw publisher emails and model weights remain outside git.

Before integration, another free model or a corrected locally trained classifier
must pass these gates and a larger independent email corpus, including legitimate
security/financial notifications, modern phishing and long messages. Integration
must retain a dedicated private service, the workspace toggle, bounded retries
and quarantine on required-check failures, with no assistant fallback and no AI
override of antivirus findings.
