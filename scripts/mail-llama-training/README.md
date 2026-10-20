# Local Llama mail training

Research-only Mac QLoRA training. Nothing in this directory activates a production
filter, modifies AI-assistant settings, sends mail, or provisions AWS resources.

The user selected **Llama 3.2 1B**, and CPU deployment evaluation is fixed at
**2 vCPU / 2 GiB with no swap**. Training uses the existing Apple Silicon GPU;
the serving budget is not a training-memory limit. No paid service is used.

## Classification objective v2

`decision.py` replaces completion-token loss with **one three-way class decision
per example**. The frozen codebook is A=legitimate, B=spam, C=phishing, each exactly
one tokenizer token. The only loss is cross-entropy over those three logits at
the prompt-end position. JSON syntax and end-of-turn tokens have zero loss weight.
Gold completions are replaced by padding before the forward pass; only the loss
reads the expected class. Gradient tests verify that no other position or
vocabulary entry contributes. An actual base-model probe confirmed that projecting
only the selected hidden state matches the normal full forward scores exactly.

This also avoids constructing sequence-length by whole-vocabulary logits during
training. It does not change the frozen base model, email evidence, source labels,
partitions or class resampling. `prepare_decision.py` verifies parent hashes and
one-token targets. The rewritten prompt and codebook are in
`decisionbench/prompt.json`. Each retained message is still checked against 4096
tokens, with no silent truncation. Run guards reject a data/objective mismatch.

The Go decision client uses the official Līdza LLM pack and a private server with
the exact grammar `root ::= "A" | "B" | "C"`. The server restricts output to the
same three tokens; the client strictly maps them back to the three named classes.
The runtime does not need to learn JSON or write an explanation. Invalid output,
timeouts and malformed requests do not become safe classifications. A separate
one-request engineering smoke check verified this protocol using an older model;
that smoke check is not counted as a quality measurement for the new model.

Training uses `decision-train.yaml`: 1200 fixed steps, batch size 2, rank 16,
scale 8, learning rate 0.00002, all 16 layers and seed 42, fresh from the same
4-bit base. Checkpoints cannot be chosen or tuned by regression scores. The
100-step batch-2 timing pilot took 131.27 seconds and 2.45 GiB peak MLX allocation;
a 50-step batch-4 probe took 254.26 seconds and 8.27 GiB. Batch 2 was chosen for
its measured efficiency and memory use. Pilot development predictions were
29/32 legitimate, 32/32 spam and 25/32 phishing correct: evidence of learning
separate classes, but still below the acceptance thresholds.

Reproduce the v2 experiment in the existing pinned native environment:

```sh
python scripts/mail-llama-training/prepare_decision.py --source .lidza/llama-training/data --model .lidza/llama-training/base --output .lidza/llama-decision/data
python -m unittest discover -s scripts/mail-llama-training -v
python scripts/mail-llama-training/run.py --objective class-decision --config scripts/mail-llama-training/decision-pilot.yaml --output .lidza/llama-decision/pilot.json
python scripts/mail-llama-training/run.py --objective class-decision --config scripts/mail-llama-training/decision-train.yaml --output .lidza/llama-decision/training.json
python scripts/mail-llama-training/evaluate_decision.py --model .lidza/llama-training/base --adapters .lidza/llama-decision/trained-adapters --data .lidza/llama-decision/data/valid.jsonl --per-class 32 --output .lidza/llama-decision/final-validation.json
python scripts/mail-llama-training/export.py --root .lidza/llama-decision --base .lidza/llama-training/base --converter-source .lidza/llama-training/llama.cpp --expected-steps 1200 --converter-python <cpu-conversion-python>
```

Before export, freeze `acceptance-before-test.json` in the run directory, including
the schedule, quality thresholds and resource limits. Build the isolated client
with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o .lidza/llama-decision/decision-client ./scripts/mail-llama-training/decisionbench`.
Run `benchmark.py` with the usual model/provenance/corpus/output flags plus
`--class-decision --client .lidza/llama-decision/decision-client` to enforce the
matching contract. The 2-vCPU/2-GiB/no-swap CPU result is authoritative for serving;
native development scores are not substituted for it. Nothing enables integration.

The following original JSON-completion procedure remains available to reproduce
the earlier failed checkpoint; do not mix its data, prompt or objective with v2.

## CPU export parity investigation

Measured results and remaining release conditions: [CPU parity report](../../docs/llama-cpu-parity.md).

The original Q4 artifact is retained. For the unchanged v2 adapters, export a
separate higher-precision artifact without overwriting its provenance:

```sh
python scripts/mail-llama-training/export.py --root .lidza/llama-decision --base .lidza/llama-training/base --converter-source .lidza/llama-training/llama.cpp --converter-python .lidza/local-mail-training/venv/bin/python --expected-steps 1200 --quantization Q8_0
python scripts/mail-llama-training/benchmark.py --root .lidza/llama-decision --model .lidza/llama-decision/trained-q8.gguf --provenance .lidza/llama-decision/trained-q8-provenance.json --class-decision --client .lidza/llama-decision/decision-client --serving-profile scripts/mail-llama-training/decision-v2-serving.json --corpus .lidza/local-mail-training/llama-pilot.json --output .lidza/llama-parity/cpu-q8.json
```

Q8 conversion still has a separate 4-GiB allowance. The deployment benchmark
retains **2 vCPU / 2 GiB / no swap / no GPU / no network**, unchanged quality and
challenge gates, and bounded 120-second requests. The serving profile records a
32-token processing batch, disables weight repacking to avoid a second resident
weight copy, and freezes the template date to `19 Oct 2026`, matching this
checkpoint's training-day prompt. Version 2 also sets `--cache-ram 0`,
`--no-cache-prompt` and `--no-cache-idle-slots`: the runtime's default 8192-MiB
RAM prompt cache is inappropriate for this 2-GiB stateless classifier. This
does not disable Thura's separate application-level AI response cache. This profile belongs to the v2 research
checkpoint; do not silently apply that date to another training run.

While the private benchmark container is running, use `compare_serving.py` with
`--model`, `--adapters`, `--corpus`, `--container`, `--serving-profile` and `--output`
to verify every runtime token sequence against the Mac tokenizer and measure
native predictions on exactly the same Go-encoded evidence. It uses the raw
server's diagnostic template/tokenization endpoints only; actual CPU predictions
still pass through the official Go LLM pack. IDs, labels and source metadata do
not enter requests. The shared encoding test covers Go's HTML/control-character
escaping so fake chat headers cannot silently change this comparison.

No adapter selection, training, threshold adjustment or acceptance relaxation is
part of this precision comparison. The exposed regression is diagnostic, and a
numerical pilot pass is never automatic production approval.

## Corrected AWS approach

The AWS sample at revision `9e53b98240177b5ca47fdf23b8bd0f722d0ade26`
uses substring label matching, so `not spam` matches `spam`; unknown labels become
safe, ordinary spam becomes phishing, conflicting duplicates only warn, and the
evaluator computes quality over successful predictions only. This replacement
uses explicit source mappings, three classes, rejects conflicting content/group
leakage, and retains failed/unknown outputs in the denominator. It reports
class-specific confusion matrices rather than deriving ROC AUC from hard labels.
This is an independent local adaptation, not a patch submitted to AWS.

`prepare.py` uses the licensed, grouped/time-separated corpus documented in
`../mail-classifier-training/README.md`. Original Apache-2.0 training-only hard
negatives add receipts, invoices, genuine security notices and requested marketing;
ordinary unsolicited promotions remain separate from deceptive payment/access
requests. Class-balanced training resamples the small spam class; it does not
create additional independent messages. All source hashes are checked.

Important limitations: legitimate held-out mail is synthetic, ordinary spam is
narrowly curated, and the previously reported test/challenge partitions are now
**exposed regression data**, not a fresh blind release test. Synthetic additions
do not repair that evidence gap. No private inbox or unlicensed mirror is used.

Training uses the exact serving system prompt and Go-compatible JSON evidence
encoding. Control-token delimiters remain quoted evidence. The final assistant
completion alone contributes to loss (`mask_prompt: true`). Every retained sample
is tokenized before training; overlong rows are explicitly logged and excluded,
never truncated. Evaluation requests that exceed serving context must count as
failures/holds, not disappear from quality denominators.

## Reproduce

Use native ARM Python 3.12 on Apple Silicon. Install the pinned environment from
`requirements.lock` into a private venv. Weights and raw data stay under `.lidza/`.

1. `python scripts/mail-llama-training/fetch.py --output .lidza/llama-training/base`
2. Prepare the previously pinned licensed corpus, then:
   `python scripts/mail-llama-training/prepare.py --dataset .lidza/local-mail-training/dataset-locked --model .lidza/llama-training/base --output .lidza/llama-training/data`
3. Start with a 100-step copy of `train.yaml`, using a separate pilot adapter path.
   Run via `run.py --config <pilot.yaml> --output <pilot.json>`. It retains wall
   time, child RSS, and MLX peak GPU allocation. RSS and GPU allocation are different
   accounting views and must not be summed as a precise physical-memory total.
4. Only after the pilot fits, run:
   `python scripts/mail-llama-training/run.py --config scripts/mail-llama-training/train.yaml --output .lidza/llama-training/training.json`
   The fixed 600-step schedule starts fresh; it does not resume pilot weights.
   This is a bounded training experiment, not an exhaustive training of all
   resampled rows or a hyperparameter search. Final weights are chosen by the
   predeclared schedule, never by the regression test score.
5. Run `probe.py` on the final adapters. Fuse with MLX and convert using official
   llama.cpp revision `31288c9c66e59377f7788d920678a565bb89dd37` (matching the
   pinned runtime image). Install its `gguf-py` into a separate CPU conversion
   environment with PyTorch. `export.py --root .lidza/llama-training --converter-python <python>`
   retains adapter, data, base and final GGUF hashes. Fusion dequantizes the MLX
   4-bit base; final Q4_K_M conversion is an additional lossy quantization step.
   Conversion has its own 4-GiB memory allowance; it is not the inference benchmark.
6. `benchmark.py --root .lidza/local-mail-training --model .lidza/llama-training/trained-q4.gguf --provenance .lidza/llama-training/trained-provenance.json --corpus .lidza/local-mail-training/llama-pilot.json --output .lidza/llama-training/cpu-pilot.json`
   This uses the unchanged private Go Līdza LLM-pack client from
   `../mail-llama-benchmark/`, a 4096-token context, two CPU threads and one slot.
   Raw measurements are saved before acceptance checks, including rejected runs.

## Acceptance

Phishing recall ≥95%; legitimate-mail false-positive/hold rate ≤2%; ordinary spam
recall ≥90%; ordinary spam called phishing ≤5%; no failed/unknown outputs;
instruction and long-tail challenge recall each ≥95%; enforced 2-vCPU/2-GiB/no-swap limits with headroom and
no OOM. Passing the regression pilot alone cannot authorize production: a fresh,
properly permitted representative legitimate-mail holdout is also required.
Never silently change gates after looking at results. Never substitute assistant models
or settings. The workspace Mail screening toggle remains in place.

The user explicitly allowed 10–30 seconds during the first CPU run, replacing the
previous 5-second p95 limit. That run's original 5-second assessment is preserved;
the report separately assesses its unchanged timings against the authorized
30-second budget. The user subsequently clarified that accuracy takes priority:
longer screening is acceptable when it measurably improves results. Latency and
the previous 5-emails/minute target are now reported planning measurements, not
reasons to accept a bad classifier or veto an accurate one without assessing the
under-1000-emails/day queue. This does not change any quality gate. Always report the
slowest request too; p95 is not a promise that every request finishes in 30 seconds.

## Licensing

The checkpoint is a pinned public MLX conversion of Meta Llama 3.2 1B Instruct,
under the **Llama 3.2 Community License and Acceptable Use Policy**, not Apache/MIT.
Keep the agreement/AUP and Meta notice with any later distributed derivative,
use a model name beginning with Llama and the required Built with Llama attribution.
This experiment does not publish weights or adapters. Checkpoint card/provenance
is frozen in `base.json`; dataset grants remain separate and are not relicensed
by training. The AWS reference is MIT-0; see its repository LICENSE. New original
training examples explicitly declare Apache-2.0 in `hard-negatives.json`.
