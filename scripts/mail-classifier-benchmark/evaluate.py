"""Research-only inference process. No HTTP listener, model downloads or application wiring."""
import time
PROCESS_STARTED = time.monotonic()

import argparse
import hashlib
import json
from pathlib import Path


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def windows(ids):
    # 510 content tokens + CLS/SEP, with overlap; never silently discard the tail.
    return [ids[offset:offset + 510] for offset in range(0, max(1, len(ids)), 446)]


def main():
    import numpy as np
    import onnxruntime as ort
    from tokenizers import Tokenizer
    parser = argparse.ArgumentParser()
    parser.add_argument('--precision', choices=['int8', 'fp32'], required=True)
    args = parser.parse_args()
    directory = Path('/models')
    manifest = json.loads((directory / 'artifacts.json').read_text())
    filename = 'model_int8.onnx' if args.precision == 'int8' else 'model.onnx'
    for name in [filename, 'tokenizer.json', 'config.json']:
        info = manifest['files'][name]
        if (directory / name).stat().st_size != info['bytes'] or sha256(directory / name) != info['sha256']:
            raise ValueError('pinned artifact verification failed')
    config = json.loads((directory / 'config.json').read_text())
    if config['max_position_embeddings'] != 512 or set(config['id2label']) != {'0', '1', '2', '3'}:
        raise ValueError('model architecture or class count changed')
    options = ort.SessionOptions()
    options.intra_op_num_threads = 1
    options.inter_op_num_threads = 1
    options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
    session = ort.InferenceSession(str(directory / filename), sess_options=options, providers=['CPUExecutionProvider'])
    tokenizer = Tokenizer.from_file(str(directory / 'tokenizer.json'))
    tokenizer.no_truncation()
    tokenizer.no_padding()
    if tokenizer.token_to_id('[CLS]') != 101 or tokenizer.token_to_id('[SEP]') != 102:
        raise ValueError('special token contract changed')
    startup = time.monotonic() - PROCESS_STARTED
    cases = Path('/data/cases.json')
    if cases.stat().st_size > 8 * 1024 * 1024:
        raise ValueError('benchmark corpus exceeds size limit')
    rows = json.loads(cases.read_text())
    if not 1 <= len(rows) <= 1000 or len({row['id'] for row in rows}) != len(rows):
        raise ValueError('invalid benchmark size or duplicate IDs')
    results = []
    for row in rows:
        if row['expected'] not in ['safe', 'phishing'] or not row['text'].strip() or len(row['text'].encode()) > 12000:
            raise ValueError('invalid benchmark case')
        before = time.monotonic()
        ids = tokenizer.encode(row['text'], add_special_tokens=False).ids
        chunks = windows(ids)
        if len(chunks) > 32:
            raise ValueError('tokenized input exceeds bounded window budget')
        risk = 0.0
        classes = []
        for chunk in chunks:
            input_ids = np.array([[101] + chunk + [102]], dtype=np.int64)
            logits = session.run(None, {'input_ids': input_ids, 'attention_mask': np.ones_like(input_ids)})[0][0].astype(np.float64)
            if logits.shape != (4,) or not np.isfinite(logits).all():
                raise ValueError('invalid model output')
            probabilities = np.exp(logits - logits.max())
            probabilities /= probabilities.sum()
            # Both malicious classes count. These are scores, not calibrated safety guarantees.
            risk = max(risk, float(probabilities[1] + probabilities[3]))
            classes.append(int(np.argmax(probabilities)))
        results.append({'id': row['id'], 'expected': row['expected'],
                        'prediction': 'phishing' if risk >= 0.5 else 'safe',
                        'classes': classes, 'score': risk, 'tokens': len(ids), 'chunks': len(chunks),
                        'millis': (time.monotonic() - before) * 1000})
    report = {'precision': args.precision, 'startup_seconds': startup,
              'model_sha256': manifest['files'][filename]['sha256'],
              'dataset_sha256': sha256(cases), 'results': results,
              'cgroup': {name: Path('/sys/fs/cgroup/' + name).read_text().strip() for name in
                         ['memory.max', 'memory.peak', 'memory.swap.max', 'memory.events', 'cpu.max', 'cpu.stat']}}
    print(json.dumps(report), flush=True)


if __name__ == '__main__':
    main()
