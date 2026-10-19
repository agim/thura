"""Separate binary AntiSpamInstitute spam-model comparison, not a three-class filter."""
import argparse
import hashlib
import json
from pathlib import Path
import time
import sys

REPOSITORY = 'AntiSpamInstitute/spam-detector-bert-MoE-v2.2'
REVISION = '9527f5d20e700a38e500895c8439fc7d7eb721e7'
WEIGHTS_SHA256 = '5b56b515173c54d053bd8fb7af27ad17a56c4716a048ad243c1511dcb4b16303'


def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def prepare(directory, candidate='spam', quantize=False):
    import torch
    from transformers import AutoModelForSequenceClassification
    torch.set_num_threads(2)
    pinned = json.loads(Path(__file__).with_name('antispam.json' if candidate == 'spam' else 'antispam-phishing.json').read_text())
    for name, info in pinned['files'].items():
        if sha(directory / name) != info['sha256'] or (directory / name).stat().st_size != info['bytes']:
            raise ValueError('candidate input differs from pinned revision')
    model = AutoModelForSequenceClassification.from_pretrained(str(directory), local_files_only=True,
                    trust_remote_code=False, attn_implementation='eager').cpu().eval()
    hidden, layers = (128, 2) if candidate == 'spam' else (1024, 24)
    if model.config.num_labels != 2 or model.config.hidden_size != hidden or model.config.num_hidden_layers != layers:
        raise ValueError('candidate architecture changed')
    if candidate == 'phishing' and model.config.id2label != {0: 'benign', 1: 'phishing'}:
        raise ValueError('phishing label mapping changed')

    class Logits(torch.nn.Module):
        def __init__(self):
            super().__init__()
            self.model = model
        def forward(self, input_ids, attention_mask, token_type_ids):
            return self.model(input_ids=input_ids, attention_mask=attention_mask, token_type_ids=token_type_ids).logits

    ids = torch.ones((1, 512), dtype=torch.long)
    dynamic = {name: {0: 'batch', 1: 'sequence'} for name in ['input_ids', 'attention_mask', 'token_type_ids']} if candidate == 'phishing' else None
    torch.onnx.export(Logits(), (ids, ids, torch.zeros_like(ids)), str(directory / 'model.onnx'),
                      input_names=['input_ids', 'attention_mask', 'token_type_ids'], output_names=['logits'],
                      dynamic_axes=dynamic, opset_version=17, dynamo=False)
    del model
    import gc
    gc.collect()
    names = ['model.onnx', 'model.safetensors', 'config.json', 'tokenizer.json', 'README.md']
    if quantize:
        from onnxruntime.quantization import QuantType, quantize_dynamic
        quantize_dynamic(str(directory / 'model.onnx'), str(directory / 'model_int8.onnx'),
                         weight_type=QuantType.QInt8, op_types_to_quantize=['MatMul', 'Gemm'],
                         per_channel=False, reduce_range=True, extra_options={'MatMulConstBOnly': True})
        names.append('model_int8.onnx')
    (directory / 'comparison.json').write_text(json.dumps({'repository': pinned['repository'], 'revision': pinned['revision'],
        'candidate': candidate, 'dynamic_padding': candidate == 'phishing',
        'labels': ['Not Spam', 'Spam'] if candidate == 'spam' else ['benign', 'phishing'], 'threshold': .5,
        'pooling': 'maximum positive-class probability across every overlapping window',
        'files': {name: {'bytes': (directory / name).stat().st_size, 'sha256': sha(directory / name)} for name in names}}, indent=2))


def evaluate(directory, corpus, kind='fp32', phase='validation'):
    started = time.monotonic()
    limits = {name: Path('/sys/fs/cgroup/' + name).read_text().strip() for name in ['memory.max', 'memory.swap.max', 'cpu.max']}
    print(json.dumps({'phase': 'limits', 'cgroup': limits}), file=sys.stderr, flush=True)
    import numpy as np
    import onnxruntime as ort
    from tokenizers import Tokenizer
    from common import windows
    manifest = json.loads((directory / 'comparison.json').read_text())
    filename = 'model_int8.onnx' if kind == 'int8' else 'model.onnx'
    for name in [filename, 'config.json', 'tokenizer.json', 'README.md']:
        if sha(directory / name) != manifest['files'][name]['sha256']:
            raise ValueError('candidate artifact mismatch')
    options = ort.SessionOptions()
    options.intra_op_num_threads = options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(directory / filename), sess_options=options, providers=['CPUExecutionProvider'])
    tokenizer = Tokenizer.from_file(str(directory / 'tokenizer.json'))
    tokenizer.no_truncation()
    tokenizer.no_padding()
    startup = time.monotonic() - started
    rows = [json.loads(line) for line in corpus.read_text().splitlines()]
    results = []
    flag = 'flagged_as_phishing' if manifest.get('candidate') == 'phishing' else 'flagged_as_spam'
    for row in rows:
        begin = time.monotonic()
        chunks = windows(tokenizer.encode(row['text'], add_special_tokens=False).ids)
        if len(chunks) > 32:
            raise ValueError('window budget exceeded; no silent truncation')
        scores = []
        for chunk in chunks:
            padding = [] if manifest.get('dynamic_padding') else [0] * (510 - len(chunk))
            ids = np.array([[101] + chunk + [102] + padding], dtype=np.int64)
            logits = session.run(None, {'input_ids': ids, 'attention_mask': (ids != 0).astype(np.int64), 'token_type_ids': np.zeros_like(ids)})[0][0]
            if logits.shape != (2,) or not np.isfinite(logits).all():
                raise ValueError('invalid classifier output')
            probs = np.exp(logits - logits.max())
            scores.append(float(probs[1] / probs.sum()))
        results.append({'id': row['id'], 'source': row.get('source', ''), 'label': row['label'], flag: max(scores) >= .5,
                        'positive_score': max(scores),
                        'millis': (time.monotonic() - begin) * 1000, 'chunks': len(chunks)})
        if len(results) % 25 == 0:
            print(json.dumps({'phase': 'inference', 'completed': len(results), 'total': len(rows)}), file=sys.stderr, flush=True)
    cohorts = {}
    for label in ['legitimate', 'spam', 'phishing']:
        current = [r for r in results if r['label'] == label]
        cohorts[label] = {'count': len(current), flag: sum(r[flag] for r in current)}
    print(json.dumps({'candidate': manifest, 'kind': kind, 'corpus_sha256': sha(corpus), 'phase': phase,
        'startup_seconds': startup, 'cohorts': cohorts, 'results': results,
        'cgroup': {name: Path('/sys/fs/cgroup/' + name).read_text().strip() for name in
                   ['memory.max', 'memory.peak', 'memory.swap.max', 'memory.events', 'cpu.max']}}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--models', required=True)
    parser.add_argument('--prepare', action='store_true')
    parser.add_argument('--corpus')
    parser.add_argument('--candidate', choices=['spam', 'phishing'], default='spam')
    parser.add_argument('--quantize', action='store_true')
    parser.add_argument('--kind', choices=['fp32', 'int8'], default='fp32')
    parser.add_argument('--phase', default='validation')
    args = parser.parse_args()
    if args.prepare:
        prepare(Path(args.models), args.candidate, args.quantize)
    elif args.corpus:
        evaluate(Path(args.models), Path(args.corpus), args.kind, args.phase)
    else:
        parser.error('choose --prepare or --corpus')
