# Llama CPU accuracy recovery — research checkpoint remains disabled

The large export regression is fixed on the existing 96-case diagnostic corpus.
The unchanged trained adapters now detect **42/44 phishing cases (95.45%)** in a
**2-vCPU / 2-GiB / no-swap CPU service**, compared with the original Q4 result of
22/44. All existing numerical, challenge, resource and performance pilot checks
pass. **This is a pilot pass, not production approval: the model remains disabled.**
No retraining, data changes, threshold tuning or relaxed acceptance targets were
used. No paid service was used and no weights are redistributed.

## What caused the loss and what changed

Six diagnostic cases compared the same native token sequences across the Mac,
unquantized CPU export and Q4/Q8 exports. The unquantized CPU model and Q8 matched
the Mac in all six, including three phishing messages Q4 incorrectly called
legitimate. A second controlled Q4 run with the same stateless runtime settings
still missed all three. This isolates the observed loss to Q4 precision rather
than the LoRA fusion, the runtime's weight-repacking option or the date difference.
The existing MLX base is already 4-bit; Q4 export introduces another lossy
quantization after fusion. Preserve the original artifact and its failed results.

The replacement research artifact uses **Q8_0**, exported from the exact same
fused FP16 GGUF. Its size is **1,321,082,592 bytes (1.23 GiB)**;
SHA-256 `711f55bb11f39cb4175491ee902fa10ffac13141f324c5819652f731be63b521`. Separate provenance prevents overwriting Q4.

The comparison also found two serving-contract problems. The chat template inserts
a runtime-dependent date, and Go's JSON encoder escapes HTML/control characters
inside the evidence differently from an ordinary Python JSON dump. The research
profile freezes the date to **19 Oct 2026**, matching this v2 checkpoint, and the
native comparator uses the actual Go-equivalent encoding. All **96/96 token
sequences** matched the CPU server. This date is checkpoint-specific, not a new
application-wide current date. Actual CPU predictions still use the official
Go Līdza LLM pack; raw server endpoints are used only for token diagnostics.

Finally, llama.cpp's default RAM prompt cache is **8192 MiB**. The first Q8 run
passed quality but reached the 2-GiB memory limit and recorded 161,018 reclaim-limit
events, so it was rejected. The final stateless profile explicitly disables that
cross-message cache and prompt reuse, disables weight repacking and uses batch 32.
Thura's separate application-level AI response cache is unaffected.

## Full unchanged regression

| Measurement | Phishing detected | Legitimate falsely flagged | Spam correct | Resource gate |
|---|---:|---:|---:|---|
| Original Q4 CPU | 22/44 | 0/32 | 20/20 | Fail |
| Native Mac, exact serving inputs | 41/44 | 0/32 | 20/20 | Not a deployment test |
| Q8 with default RAM cache | 42/44 | 0/32 | 20/20 | Fail |
| **Q8, stateless final profile** | **42/44** | **0/32** | **20/20** | **Pass** |

Final instruction-injection detection was **8/8**, long-tail detection **4/4**;
there were **zero errors/invalid outputs** and zero spam messages called phishing.
The two remaining misses are authored phishing cases 14 and 25. Q8 agrees with
the native Mac on **95/96** decisions; it additionally detects the tail-00 case.
Do not call this bit-identical floating-point inference or claim all phishing is
caught. The unchanged gates require ≥95% phishing/challenge recall, ≤2% legitimate
false positives, ≥90% spam recall, ≤5% spam called phishing and no inference errors.

## Final deployment measurements

- Enforced CPU quota: `cpu.max=200000 100000`; 2 inference threads, one slot, no GPU.
- RAM limit **2,147,483,648 bytes**, swap **0**; peak **1,579,884,544 bytes (1.47 GiB)**.
- Memory-limit/reclaim events **0**, OOM events **0**, OOM kills **0**.
- Median **2.004s**, p95 **19.982s**, slowest **45.051s**.
- Sequential throughput **12.63 messages/minute** on this corpus.
- Full 4096-token context, no context shifting or silent truncation; 120-second bounded requests.

Docker ran on the local Mac with its application active. Native comparisons and
repository checks also ran on the host during parts of evaluation; timings describe
that load rather than dedicated bare-metal performance. The larger unquantized
precision probe had a separate **4-GiB diagnostic allowance** and does not count
as passing the required 2-GiB deployment test.

## Validation and release status

Strict Līdza verification passed on an isolated staged checkout, including the
full Go suite, generated files, static checks and the existing-test protection
gate. All **12 Python tests** passed. New regression checks cover Go/native JSON
encoding, frozen dates, bounded runtime profiles and rejection of RAM-cache
settings that exceed the stateless contract. Existing assertions remain intact.

The regression corpus is exposed and its 32 legitimate controls are synthetic.
Zero false positives here does not establish a real-inbox ≤2% false-positive rate.
A fresh, representative, properly permitted independent evaluation is still needed
before integration. **The dedicated classifier stays disabled and separate from
the AI assistant; the Mail screening toggle remains intact.**

[Measurements, per-case outcomes and provenance](benchmarks/llama-cpu-parity.json).
Reproduction: `scripts/mail-llama-training/README.md`, `compare_serving.py` and
`decision-v2-serving.json`. Original failed Q4 results remain in
`docs/llama-classification-v2.md`.
