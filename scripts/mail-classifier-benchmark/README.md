# Pretrained mail-classifier evaluation

Research tooling, deliberately **not** wired into mail intake. A fitting model is
not necessarily an adequate phishing classifier. Do not enable this candidate
because of the publisher's accuracy claim or a successful container startup.

The pinned candidate is cybersectony/phishing-email-detection-distilbert_v2.4.1,
converted by onnx-community. `artifacts.json` pins revisions, hashes, sizes,
the publisher's license declaration, and the dataset's semantic label mapping.
The model card example misnames class 1; the dataset documents 0=legitimate
email, 1=phishing email, 2=legitimate URL, 3=phishing URL. Both odd classes are
malicious. Softmax scores are not calibrated confidence guarantees.

The source model and DistilBERT base declare Apache-2.0. The conversion identifies
that source but has no separate license declaration/file. The dataset card has
no explicit license declaration. No model weights, tokenizer or upstream data
are committed or distributed in Thura's image. Any future redistribution needs
the applicable license, attribution and modification notices; source metadata
does not establish rights to every underlying training example.

## Reproduce

Use Python 3.12 for the optional PyArrow preprocessing dependency. Preparation
requires network access; the actual inference containers cannot access a network.
Downloaded files belong under `.lidza/`, never source control.

```sh
python3 scripts/mail-classifier-benchmark/prepare.py --models .lidza/classifier/models
docker build -t thura-mail-classifier-benchmark:local scripts/mail-classifier-benchmark
python3 scripts/mail-classifier-benchmark/benchmark.py \
  --models .lidza/classifier/models --output .lidza/classifier/int8-authored.json

# Optional: publisher test split; do not redistribute the downloaded corpus.
python3.12 -m venv .lidza/classifier/preparation
.lidza/classifier/preparation/bin/pip install pyarrow==23.0.1
.lidza/classifier/preparation/bin/python scripts/mail-classifier-benchmark/prepare_publisher.py \
  --output-directory .lidza/classifier/corpus
python3 scripts/mail-classifier-benchmark/benchmark.py \
  --models .lidza/classifier/models --dataset .lidza/classifier/corpus/all-cases.json \
  --precision int8 --output .lidza/classifier/int8.json
python3 scripts/mail-classifier-benchmark/benchmark.py \
  --models .lidza/classifier/models --dataset .lidza/classifier/corpus/all-cases.json \
  --precision fp32 --output .lidza/classifier/fp32.json
python3 -m unittest discover -s scripts/mail-classifier-benchmark -v
```

Exit 2 means rejected/failed, not success. Reports contain IDs, labels, scores,
hashes, timings and cgroup measurements, not email text. Every run creates and
removes its own uniquely named container; existing deployment containers are
untouched. Docker and cgroup v2 must both confirm 1 CPU, 1 GiB, zero swap, and no
OOM. CPU inference uses one ONNX Runtime intra/inter-op thread, batch one, no GPU.
Benchmark latency excludes download, Docker startup and model loading; separate
startup timing includes Python imports, artifact hashing and model initialization.
RAM measurements include cgroup-accounted page cache and process initialization.

## Quality gate and limitations

`gates.json` was defined before the first inference: at least 95% phishing recall,
at most 2% benign false positives, no errors, p95 <=5 seconds and >=5 sequential
cases/minute. Email, URL and instruction/long-tail challenge results are separate;
easy URLs cannot inflate email performance. No threshold was tuned after seeing
the test results. The binary rule is malicious class sum >=0.5 in any window.

The authored fixture has 32 benign emails, 32 phishing emails, 8 instruction
challenges and 4 long-tail challenges. It is a deliberately small smoke test,
not a representative estimate of production accuracy. Publisher samples are the
first 100 unique nonempty <=12 KB texts per class sorted by SHA-256 from the
pinned test split. They are not independently held out by Thura and may overlap
training data; raw dataset values `empty` are excluded. Inference uses overlapping
510-token windows with CLS/SEP, never silent tail truncation. This pooling method
is an evaluation strategy, not claimed to be the model's trained long-email mode.

See `docs/mail-classifier-evaluation.md` for the measured acceptance decision.
