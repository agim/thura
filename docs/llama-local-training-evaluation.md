# Local Llama email training evaluation

**Decision: reject this checkpoint and do not integrate it.** The locally trained
Llama 3.2 1B caught 44/44 phishing regression cases but incorrectly called all
32/32 legitimate emails phishing. Extra screening time does not remedy that
false-positive failure. The dedicated-filter and AI-assistant configuration are
unchanged by this experiment; no model is activated or bundled.

## Training and timing pilot

The user explicitly selected Llama 3.2 **1B** after clarifying that the text family
has no 2B release. Training ran locally on an Apple M5 Mac with 16 GiB unified
memory and 10 logical CPUs, using MLX QLoRA. No AWS resources, paid APIs, or cloud
training were used.

| Measurement | Corrected timing pilot | Final training run |
|---|---:|---:|
| Optimizer steps | 100 | 600 |
| Wall time | 64.82 seconds | 473.67 seconds, or 7 min 54 sec |
| Peak MLX GPU allocation | 3,891,442,552 bytes, 3.62 GiB | 3,989,698,716 bytes, 3.72 GiB |
| Peak child process RSS | 1,206,878,208 bytes | 1,189,052,416 bytes |

RSS and GPU allocation are different accounting views of unified memory; their
sum is not a precise physical-memory peak. An earlier 76.29-second timing-only
probe preceded the JSON-encoding parity correction and is retained separately.
The corrected pilot predicted roughly eight minutes for 600 steps, consistent
with the final run.

The pinned public MLX checkpoint is
[`mlx-community/Llama-3.2-1B-Instruct-4bit`](https://huggingface.co/mlx-community/Llama-3.2-1B-Instruct-4bit/tree/08231374eeacb049a0eade7922910865b8fce912),
revision `08231374eeacb049a0eade7922910865b8fce912`. It contains approximately
1.236 billion parameters; 11.272 million adapter parameters were trainable.
The run used all 16 layers, rank 16, scale 32, learning rate 0.0001, batch size 1,
gradient checkpointing, seed 42 and a 4096-token maximum. Final weights were
chosen by the fixed 600-step schedule, not by test scores. The pilot weights were
not resumed. This is one bounded training experiment, not an exhaustive search
or an epoch over all 7,497 resampled training rows.

Final sampled validation completion loss was 0.177, over eight batches. That is
not classification accuracy: most completion tokens are predictable JSON syntax.
Native smoke probes returned valid JSON but classified a meeting reminder, an
SEO sales pitch and credential theft all as phishing. The independent CPU
regression run below establishes the rejection.

## AWS corrections and training data

The local replacement corrects the
[AWS processor](https://github.com/aws-samples/sample-fine-tuned-llama-phishing-classifier-optimized-for-on-device/blob/9e53b98240177b5ca47fdf23b8bd0f722d0ade26/utils/data_processor_.py):
exact source-specific mappings prevent `not spam` matching `spam`, unknown labels
are rejected, ordinary spam and phishing remain separate, and conflicting
duplicates/campaign leakage cannot silently enter separate partitions. Unlike
the [sample evaluator](https://github.com/aws-samples/sample-fine-tuned-llama-phishing-classifier-optimized-for-on-device/blob/9e53b98240177b5ca47fdf23b8bd0f722d0ade26/utils/evaluate_llm.py),
failed and unknown predictions remain in quality denominators. These are local
corrections; no upstream AWS patch or published AWS checkpoint is claimed.

The licensed parent corpus and source hashes are documented in
`local-mail-training-evaluation.md` and `scripts/mail-classifier-training/README.md`.
It combines publisher-Apache-2.0 synthetic English legitimate/phishing mail,
CC-BY-4.0 Nazario phishing and Guenter's research/development-permitted spam.
No private inbox or unlicensed SpamAssassin mirror was used.

This run added **32 original Apache-2.0 training examples**: 16 legitimate hard
negatives, eight ordinary promotions and eight phishing examples. They cover
genuine security notices, invoices, receipts, requested marketing and deceptive
payment/access requests. After explicitly excluding overlong input, there were
**3,671 unique training rows**: 1,075 legitimate, 97 spam and 2,499 phishing.
Resampling balanced these to 2,499 rows per class, 7,497 total; it did not create
additional independent spam. The validation partition retained 970 rows.

Nine training and six validation phishing examples exceeded the token limit;
their exclusions are recorded. No retained message was truncated. Parent hashes,
global duplicate/group checks, actual token lengths and completion masks are
checked before training. Go-compatible JSON encoding escapes model delimiters
inside email evidence. Only the final assistant JSON plus end-of-turn token is
supervised; the actual MLX dataset mask was checked directly.

Legitimate held-out mail is still synthetic, and ordinary spam is narrowly
curated. The earlier final test and the regression fixture have already been
reported, so they are exposed regression data rather than a new blind release
test. A representative, properly permitted real legitimate-mail holdout remains
necessary to substantiate a production false-positive target. The synthetic
additions do not establish that evidence.

## CPU result at 2 vCPU and 2 GiB

The final adapter was fused, exported to FP16 using official llama.cpp revision
`31288c9c66e59377f7788d920678a565bb89dd37`, and quantized to Q4_K_M. The exported
GGUF is **807,694,048 bytes**, SHA-256
`664969993a8808c4bf6985775962dfed00f9b1a73acc2e118f14d7cd40623746`.
The final adapter SHA-256 is
`7a885675995ada395d9c8e940bd6ec2f5a8db9a6a637eb70977a6ac9b3cdbc34`.
The MLX 4-bit base was dequantized for fusion, then quantized again; the CPU
measurements assess that actual final artifact, not assumed native parity.

The private nonroot, network-isolated container used two CPU threads, no GPU,
one sequential slot and a 4096-token context. The existing official Go Līdza LLM
client supplied only email evidence; gold labels and source identities stayed
outside requests. All 96 unchanged pilot cases completed, with no invalid JSON,
unknown output or timeout. They include 32 legitimate controls, 20 validation
spam, 32 ordinary authored phishing cases, eight instruction-injection cases and
four long-tail phishing cases. All phishing subgroups were detected, but that
does not compensate for flagging every legitimate control.

| Actual class | Predicted legitimate | Predicted spam | Predicted phishing | Failed |
|---|---:|---:|---:|---:|
| Legitimate, 32 | 0 | 0 | 32 | 0 |
| Ordinary spam, 20 | 0 | 9 | 11 | 0 |
| Phishing, 44 | 0 | 0 | 44 | 0 |

| Quality measure | Result | Required |
|---|---:|---:|
| Phishing recall | 44/44, 100% | ≥95% |
| Legitimate false-positive rate | 32/32, **100%** | ≤2% |
| Ordinary spam recall | 9/20, **45%** | ≥90% |
| Ordinary spam called phishing | 11/20, **55%** | ≤5% |
| Failed or unknown replies | 0/96 | 0 |

## Screening speed and memory

| End-to-end measurement | Result |
|---|---:|
| Startup to healthy | 2.24 seconds |
| Median email | 0.470 seconds |
| 95th percentile | 15.381 seconds |
| Slowest email | 39.916 seconds |
| Sequential throughput | 19.90 emails/minute |

These are warm sequential fixture timings on this Mac with normal prefix-cache
reuse, not a production burst-load guarantee. They include client request time.
During this run the user explicitly accepted 10–30 seconds and then clarified
that longer delays are acceptable when they measurably improve detection.
The original five-second performance assessment remains in the raw report;
the unchanged measured p95 meets the newer 30-second preference. The slowest
request exceeds 30 seconds. Latency is not the reason this checkpoint is rejected.

The cgroup verified CPU quota `200000 100000`, `memory.max=2147483648` and
`memory.swap.max=0`. All requests finished with **zero OOM events/kills**. Recorded
peak was 2,147,487,744 bytes, 2 GiB plus a 4 KiB accounting transient; 2,783
memory-limit events show reclaim pressure. This is not an OOM, but it fails the
existing conservative headroom gate. The model was not given extra memory or
swap to finish. Conversion had a separate 4-GiB allowance and was not part of
the constrained serving run.

## Disposition and next experiment

Do not ship this checkpoint. Keep the Mail screening toggle and separation from
the AI assistant. Required pending screening must not expose unassessed mail;
timeouts or scanner failures remain held, and AI cannot override antivirus.

A subsequent training experiment should score the actual class decision more
directly, rather than relying on average JSON-token loss, and use more diverse
independently reviewed legitimate mail and spam. Longer inference or an
evidence-extraction second pass is a separate experiment; adding output tokens
to this classification-only checkpoint has not been shown to fix its errors.
Any such change needs validation before a fresh, locked release test. This one
run does not establish that every Llama 1B fine-tune must fail.

## Prompt and reproduction

The exact system prompt and its version are in
`scripts/mail-llama-benchmark/prompt.json`, SHA-256
`16dfe50d8bbe6bf29bf74b575ad6d1c23da9dcc3c377493ed475f8cd6d6ca1d3`.
Each user message is a JSON `text` field containing untrusted email evidence.
Response schema permits exactly one `label` property, with legitimate, spam,
phishing or unknown. Temperature is zero and output is limited to 32 tokens.
No private reasoning transcript or tools are requested.

Reproduction commands and pinned environments are in
`scripts/mail-llama-training/README.md`. The complete numerical record is
`docs/benchmarks/llama-local-training-2026-10-19.json`; it retains the original
assessment, user-authorized latency update, per-case predictions, artifact hashes,
training/data provenance, timings and cgroup counters. The final constrained run
was not rerun or tuned after its rejection. A broader final test was not consumed
after these decisive regression failures.

Artifacts remain local under `.lidza/llama-training/`, approximately 7.2 GiB
including the base, adapters, merged weights, FP16/Q4 files, source and environment.
Meta weights/adapters and raw third-party mail are not committed or redistributed.
The Llama 3.2 Community License and AUP apply separately from dataset grants and
the AWS MIT-0 code license; the model is not relabeled Apache/MIT.
