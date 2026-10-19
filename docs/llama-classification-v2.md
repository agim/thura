# Llama mail classification v2: rejected after CPU evaluation

The class-only retraining completed. **Do not integrate or activate this model.**
The change fixed the formatting-dominated training objective, but the deployed
CPU artifact still misses too much phishing. Preserve the Mail screening toggle
and keep this checkpoint separate from the ordinary AI assistant.

## What changed

Each example now supervises exactly one three-way decision: A legitimate,
B ordinary spam, C phishing. Cross-entropy covers only these three logits at the
prompt-end position. JSON syntax and end-of-turn tokens contribute no loss.
Gold completions are removed from forward input; gradient and actual-model
projection probes validate this boundary. The Go serving client uses the same
frozen prompt/codebook and a grammar permitting only A, B or C.

Evidence, source mappings, partitions and class resampling stayed fixed to isolate
the objective. The four suggested Kaggle datasets were reviewed but not silently
added; see [candidate review](mail-dataset-candidate-review.md). Source IDs,
expected labels and train/test metadata never enter the serving request.

The Apple M5 run trained fresh LoRA adapters for 1,200 fixed steps, batch 2,
rank 16, learning rate 0.00002, seed 42 and a 4096-token limit. It took
**1,714.27 seconds (28m 34s)**. Peak MLX allocation was **4.77 GiB**; peak child
RSS was 1.16 GiB. These are different accounting views and must not be summed as
an exact physical-memory figure. Objective/source hashes did not change during
training. No paid API or cloud training was used.

## Measured results

| Measurement | Legitimate correct | Ordinary spam correct | Phishing detected |
|---|---:|---:|---:|
| Balanced native development panel | 32/32 | 31/32 | 32/32 |
| Fixed exposed regression, native adapters (diagnostic) | 32/32 | 20/20 | 41/44 |
| Same regression, exported Q4 CPU service | 32/32 | 20/20 | **22/44** |

The authoritative serving run enforced **2 vCPU, 2 GiB RAM, no swap, no GPU and
no network**, using the pinned llama.cpp image and the official Go Līdza LLM pack.
All 96 replies parsed correctly; there were no failed/unknown outputs. CPU
legitimate false positives were 0/32 and ordinary spam called phishing was 0/20.
CPU instruction-injection phishing recall was **2/8**, and long-tail recall
**3/4**. The ≥95% phishing and challenge targets failed. Aggregate accuracy
77.08% does not excuse those misses.

Latency: median **0.421 s**, p95 **28.670 s**, slowest **58.565 s**;
sequential throughput **15.02 messages/minute**. Concurrent application checks
ran on the host, so these are measurements under that load. Longer screening is
permitted by the user; speed is not the reason for rejection.

The cgroup enforced `cpu.max=200000 100000`, `memory.max=2147483648`,
`memory.swap.max=0`. Recorded peak was **2147487744 bytes** (nominal limit +4 KiB),
with **2,895 memory-max/reclaim events**, **zero OOM events** and **zero OOM kills**.
This failed the unchanged conservative headroom check. Do not call this an OOM,
or silently increase the limit or waive the gate.

## Export discrepancy and interpretation

The native 41/44 phishing score is below the required ≥42/44 for this fixture,
and it must not replace the actual CPU score. After seeing the discrepancy, a
six-case diagnostic compared the trained MLX base+adapters with the fused FP16
model. All six class predictions agreed; maximum selected-logit differences were
0.00390625–0.0078125. Three sampled native-correct/Q4-wrong phishing cases remained
correct after fusion. Fusion alone did not explain their loss.

The quantized export/CPU serving path therefore has an unresolved discrepancy;
this experiment does not conclusively separate quantization error from
GGUF/template/runtime differences. Do not claim that the native model's detection
rate survives deployment. No further model selection, threshold tuning or
training used the exposed regression scores.

Even a later numerical pass would require a representative, properly permitted
real legitimate-mail holdout; held-out legitimate data here is synthetic and the
regression cases have been exposed. The next useful work is artifact/runtime
parity and better independent evaluation, rather than more JSON formatting loss
or assuming a longer answer improves classification.

## Artifacts and reproduction

[Measurements and provenance](benchmarks/llama-classification-v2.json) retain all
per-case CPU outcomes, fixed gates, hashes and resource counters without email
text. [Training instructions](../scripts/mail-llama-training/README.md) describe
the objective, pilots, exact commands, dependencies and license conditions.
The prompt is `scripts/mail-llama-training/decisionbench/prompt.json`.

The CPU GGUF SHA-256 is
`fe060d434c75fb735ea3535bf9af724450d2d56458639d9290fde23c95b0727b`,
807,694,048 bytes. Weights and raw corpora remain local and are not redistributed.
The Meta Llama 3.2 Community License/AUP applies; this is not Apache/MIT software.
