"""Separate binary AntiSpamInstitute spam-model comparison, not a three-class filter."""
import argparse
import hashlib
import json
from pathlib import Path
import time

REPOSITORY = 'AntiSpamInstitute/spam-detector-bert-MoE-v2.2'
REVISION = '9527f5d20e700a38e500895c8439fc7d7eb721e7'
WEIGHTS_SHA256 = '5b56b515173c54d053bd8fb7af27ad17a56c4716a048ad243c1511dcb4b16303'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def prepare(directory):
    import torch
    from transformers import AutoModelForSequenceClassification
    if sha(directory / 'model.safetensors') != WEIGHTS_SHA256:
        raise ValueError('candidate weights differ from inspected revision')
    pinned = json.loads(Path(__file__).with_name('antispam.json').read_text())
    for name, info in pinned['files'].items():
        if sha(directory / name) != info['sha256'] or (directory / name).stat().st_size != info['bytes']:
            raise ValueError('candidate input differs from pinned revision')
    model = AutoModelForSequenceClassification.from_pretrained(str(directory), local_files_only=True,
                    trust_remote_code=False, attn_implementation='eager').cpu().eval()
    if model.config.num_labels != 2 or model.config.hidden_size != 128 or model.config.num_hidden_layers != 2:
        raise ValueError('candidate architecture changed')

    class Logits(torch.nn.Module):
        def __init__(self):
            super().__init__()
            self.model = model
        def forward(self, input_ids, attention_mask, token_type_ids):
            return self.model(input_ids=input_ids, attention_mask=attention_mask, token_type_ids=token_type_ids).logits

    ids = torch.ones((1, 512), dtype=torch.long)
    torch.onnx.export(Logits(), (ids, ids, torch.zeros_like(ids)), str(directory / 'model.onnx'),
                      input_names=['input_ids', 'attention_mask', 'token_type_ids'], output_names=['logits'],
                      opset_version=17, dynamo=False)
    names = ['model.onnx', 'model.safetensors', 'config.json', 'tokenizer.json', 'README.md']
    (directory / 'comparison.json').write_text(json.dumps({'repository': REPOSITORY, 'revision': REVISION,
        'labels_from_model_card': ['Not Spam', 'Spam'], 'threshold': .5,
        'pooling': 'maximum spam probability across every overlapping window',
        'files': {name: {'bytes': (directory / name).stat().st_size, 'sha256': sha(directory / name)} for name in names}}, indent=2))


def evaluate(directory, corpus):
    started = time.monotonic()
    import numpy as np
    import onnxruntime as ort
    from tokenizers import Tokenizer
    from common import windows
    manifest = json.loads((directory / 'comparison.json').read_text())
    for name in ['model.onnx', 'config.json', 'tokenizer.json', 'README.md']:
        if sha(directory / name) != manifest['files'][name]['sha256']:
            raise ValueError('candidate artifact mismatch')
    options = ort.SessionOptions()
    options.intra_op_num_threads = options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(directory / 'model.onnx'), sess_options=options, providers=['CPUExecutionProvider'])
    tokenizer = Tokenizer.from_file(str(directory / 'tokenizer.json'))
    tokenizer.no_truncation()
    tokenizer.no_padding()
    startup = time.monotonic() - started
    rows = [json.loads(line) for line in corpus.read_text().splitlines()]
    results = []
    for row in rows:
        begin = time.monotonic()
        chunks = windows(tokenizer.encode(row['text'], add_special_tokens=False).ids)
        scores = []
        for chunk in chunks:
            ids = np.array([[101] + chunk + [102] + [0] * (510 - len(chunk))], dtype=np.int64)
            logits = session.run(None, {'input_ids': ids, 'attention_mask': (ids != 0).astype(np.int64), 'token_type_ids': np.zeros_like(ids)})[0][0]
            probs = np.exp(logits - logits.max())
            scores.append(float(probs[1] / probs.sum()))
        results.append({'id': row['id'], 'label': row['label'], 'flagged_as_spam': max(scores) >= .5,
                        'millis': (time.monotonic() - begin) * 1000, 'chunks': len(chunks)})
    cohorts = {}
    for label in ['legitimate', 'spam', 'phishing']:
        current = [r for r in results if r['label'] == label]
        cohorts[label] = {'count': len(current), 'flagged_as_spam': sum(r['flagged_as_spam'] for r in current)}
    print(json.dumps({'candidate': manifest, 'corpus_sha256': sha(corpus), 'phase': 'validation',
        'startup_seconds': startup, 'cohorts': cohorts, 'results': results,
        'cgroup': {name: Path('/sys/fs/cgroup/' + name).read_text().strip() for name in
                   ['memory.max', 'memory.peak', 'memory.swap.max', 'memory.events', 'cpu.max']}}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--models', required=True)
    parser.add_argument('--prepare', action='store_true')
    parser.add_argument('--corpus')
    args = parser.parse_args()
    if args.prepare:
        prepare(Path(args.models))
    elif args.corpus:
        evaluate(Path(args.models), Path(args.corpus))
    else:
        parser.error('choose --prepare or --corpus')
