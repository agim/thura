# Generic Llama 1B email pilot

This is an experiment, not the AWS fine-tuned detector and not an activated mail
filter. It uses Meta Llama 3.2 1B Instruct Q4_K_M from the official Ollama registry,
pinned at the manifest/blob hashes in `artifacts.json`. The 807,694,144-byte GGUF
is generic instruction-tuned, not trained on Thura's email labels. Model and
license blobs stay outside git. Meta's Llama 3.2 license/AUP apply independently
of AWS's MIT example code; this tool does not clear model redistribution.

Prepare the locked validation partition using `scripts/mail-classifier-training`.
Then, from the repository root:

```sh
run=.lidza/local-mail-training
python3 scripts/mail-llama-benchmark/prepare.py --root "$run"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o "$run/llama1b-client" ./scripts/mail-llama-benchmark
chmod 755 "$run/llama1b-client"
python3 scripts/mail-llama-benchmark/benchmark.py --root "$run" --corpus "$run/llama-pilot.json" --output "$run/llama1b-pilot.json"
```

The Go client uses a private official Lidza LLM-pack compatible client. It reads
no assistant configuration and never uses an assistant fallback. Only the email
text is forwarded; source IDs and expected labels stay in the evaluator. A fixed
system prompt defines legitimate/spam/phishing/unknown. JSON Schema constrains
output, temperature is zero, seed 42, one model slot and at most 32 output tokens.
Unknown, truncated, invalid, refused or failed output counts as an error, never
as safe. There is one attempt per case, no automatic retry or tools.

The model server and Go client share a single container with 1 CPU, 2 GiB RAM,
zero extra swap, no network/ports, read-only mounts/root, no capabilities and a
nonroot user. The server uses CPU only, one inference/prompt thread, batch 128,
context 8192, fit disabled and no context shifting. Oversized prompts must fail,
not silently lose tail evidence. Normal sequential prefix-cache reuse is left
enabled; timings include the official-pack request/validation and exclude
Docker-exec overhead. Cgroup memory includes the server, client and file cache.
The measured quota is not a guarantee of speed on every cloud vCPU.

The pilot contains the unchanged 76 authored regression/challenge cases plus
20 ordinary-spam examples from the locked validation partition, shuffled with
seed 42. The same 96 cases are used for the AntiSpamInstitute phishing comparison.
It contains 32 legitimate, 20 ordinary spam and 44 phishing cases, of which eight
contain override instructions and four bury phishing after long benign text.
Maximum evidence is 8,330 bytes. No final-test data is opened by this preparation.

This known pilot can reject a candidate, but cannot certify a population false-
positive rate or production readiness. Even `pilot_pass_only` never authorizes
integration. Predeclared gates remain >=95% phishing recall, <=2% legitimate
false positives, >=90% ordinary-spam recall, <=5% spam called phishing, no errors,
each instruction/tail cohort >=95%, p95 <=5 seconds, >=5 sequential emails/minute,
enforced resource limits and no OOM. A conservative resource headroom check
also rejects a run whose cgroup peak touches the cap; do not silently loosen it.

Reports include model/client/prompt/corpus hashes, image identity, actual cgroups,
per-case predictions and token counts, cohort errors, latency and acceptance.
Raw result JSON and progress diagnostics are written before evaluating gates so
rejected runs retain evidence. No email bodies are included. An initial run
exposed a missing-report bug at the resource gate and was discarded; the fixed
driver reruns the identical prompt, model, cases and limits. The raw response is
also saved beside the final report for auditability.

Unit checks: `go test ./scripts/mail-llama-benchmark`.
