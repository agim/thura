# Generic Llama 1B email pilot — October 19, 2026

**Decision: do not integrate this configuration.** The tested generic model is
not a useful mail detector with these prompts. This is not a measurement of the
AWS fine-tuned detector: AWS publishes its training recipe, not downloadable
email-specialized weights. No Llama fine-tuning has run in Thura. No candidate
was activated, assistant settings were not used, and the Mail toggle is retained.

## Model and actual setup

Meta Llama 3.2 1B Instruct, Q4_K_M, from the
[official Ollama tag](https://ollama.com/library/llama3.2:1b-instruct-q4_K_M).
The registry manifest SHA-256 is
`22bc6b92eb0160c4629782fca05a9032c59d306ab2058b7dacc8a4644fbafa02`;
the 807,694,144-byte GGUF SHA-256 is
`84fffd9ce0ecc7e0a36ec7a3c3aa21bb888213f8bf3db9408d5520a26412f06e`.
Metadata identifies Llama 3.2 1B Instruct and includes its Llama chat template,
with the expected role/header tokens. The server uses that Jinja template.
Model/license blobs remain local, outside git. Meta's license/AUP are separate
from AWS's MIT example code; no model redistribution clearance is claimed.

Actual Linux arm64 Docker CPU inference on Apple M5, one CPU quota, two GiB RAM,
no additional swap, no network or exposed ports, read-only/nonroot, CPU only,
one inference/prompt thread and one model slot. Context is 8192, fit disabled,
context shifting disabled, batch/ubatch 128. Maximum measured prompt length is
3,580 tokens. Inputs are not silently truncated.

The benchmark uses a private official Lidza LLM-pack compatible client and
explicit local endpoint. Expected labels, source IDs and case IDs never enter
requests. Fixed versioned system prompt, temperature zero, seed 42, JSON Schema
for legitimate/spam/phishing/unknown, maximum 32 output tokens, one attempt and
no tools. Normal sequential prefix-cache reuse is enabled. Timings include
request/validation inside the container and exclude Docker-exec overhead.

## Fixed 96-case pilot

The unchanged 76 authored regression/challenge cases plus 20 ordinary-spam
messages from the locked validation partition, shuffled with seed 42. Corpus
SHA-256: `b132a9b8d7c4f9200f163c6c62cadd185795cc0a85fa6e94b58d054ad70a083f`.
This is known regression/pilot data, not independent population accuracy. No
final-test data is opened by this pilot's preparation. Unknown answers count as
required-review failures, never as successful safe classifications.

| Actual label | Correct | Other output |
| --- | ---: | --- |
| 32 legitimate | 24 legitimate | 8 unknown |
| 20 ordinary spam | 0 spam | 19 legitimate, 1 unknown |
| 44 phishing | 0 phishing | 36 legitimate, 2 spam, 6 unknown |

All 15 failures are valid `unknown` answers, not transport crashes or malformed
JSON. No legitimate case was explicitly called spam/phishing, but 8/32 would be
held for review. The conservative operational false-positive/hold rate is 25%;
it must not be described as eight malicious-label false alarms. Two phishing
cases would be held as spam, six as unknown, and 36 would be incorrectly allowed
by the model alone. None of eight instruction or four tail challenges receives
the correct phishing label. All failures remain in the metric denominators.

These results reject this fixed configuration. They do not prove that every
prompt or a separately fine-tuned Llama model must perform identically.

## Resource results

- Startup: 2.30 seconds.
- Inference p50: 0.77 seconds; p95: 25.09 seconds; maximum: 83.41 seconds.
- Sequential throughput: 11.77 emails/minute on this pilot.
- Actual memory.max: 2,147,483,648; memory.swap.max: 0; cpu.max: `100000 100000`.
- Peak memory counter: 2,147,487,744 bytes (cap plus 4 KiB accounting transient).
- No OOM or OOM kill. The memory-limit event counter is 3,984: reclamation occurs
  at the cap. The conservative headroom gate fails; hard isolation is verified.

Do not equate memory-limit reclamation with an OOM kill. This configuration is
under pressure at two GiB and fails the predeclared five-second p95 gate, while
passing the five-emails/minute throughput gate. Host concurrency and CPU
architecture matter; the quota does not guarantee speed on every cloud vCPU.

An initial identical run completed but lost its classification report when the
headroom validator raised before writing results. That is a harness bug, not a
model quality result. The corrected driver saves raw measurements before gates,
and reran the same model/prompt/cases/limits. Only the retained rerun is reported.
The gate was not loosened. No output was discarded to improve metrics.

## Prompt sanity checks

After the fixed pilot, nine controlled probes checked three simple original
emails (meeting reminder, SEO promotion, direct bank-password/MFA theft) with
the fixed JSON prompt, a shorter JSON prompt and a short plain-label prompt.
Both JSON prompts call the first two legitimate and the theft spam. The plain
prompt calls all three legitimate. This small diagnostic did not repair the
failure and is not a second population benchmark. Probe source and exact
outputs are retained; no prompt was retuned on the final test set.

## What training the AWS-recommended model would involve

The [AWS example](https://github.com/aws-samples/sample-fine-tuned-llama-phishing-classifier-optimized-for-on-device)
starts from Llama 3.2 1B Instruct and uses LoRA, rather than training all model
knowledge from scratch. Its SageMaker tuning example can launch up to 20 paid
training jobs. AWS/S3 are unnecessary for a local adaptation.

For Thura, replace the example's label rules: its
[processor](https://github.com/aws-samples/sample-fine-tuned-llama-phishing-classifier-optimized-for-on-device/blob/9e53b98240177b5ca47fdf23b8bd0f722d0ade26/utils/data_processor_.py)
merges spam into phishing, substring-matches “not spam” as phishing, and treats
unknown labels as safe. Use explicit source-specific labels, conflicting-label
rejection, campaign-separated splits, training-only augmentation and strict
error accounting. More permitted real legitimate mail and ordinary spam are
needed; the current synthetic/narrow corpus already shows generalization gaps.

[MLX-LM supports Llama LoRA/QLoRA](https://github.com/ml-explore/mlx-lm/blob/main/mlx_lm/LORA.md)
on Apple silicon. This Mac has 16 GiB and is a plausible free training host.
Initial engineering budget: roughly 6–12 GiB working memory and 15–20 GB disk,
using a small batch, gradient checkpointing and a bounded sequence/context
policy that preserves evidence. These are planning estimates, not measured 1B
training requirements. Time a short 100-step pilot before extrapolating; expect
hours for experimentation, not a promised completion time.

Access the training checkpoint under Meta's applicable license, train the small
adapter with reviewed labels, choose checkpoints using validation only, merge
and export/quantize, then retest the deployed artifact for quality and resources.
Training memory is separate from the two-GiB inference target. Cloud/API charges
can be zero on the existing Mac; hardware/power and the model license still
apply. No fine-tuning or paid account/service is launched by this report.

Reproduction: `scripts/mail-llama-benchmark/README.md`. Full aggregate evidence is
in `docs/benchmarks/llama-1b-2026-10-19.json`; email bodies and model weights are
not included. The results do not support activation or any “phishing-proof” claim.
