"""Actual inference and validation-only calibration inside a no-network constrained container."""
import time
STARTED = time.monotonic()
import argparse
import hashlib
import json
from pathlib import Path

from common import acceptance, classify, metrics
from predict import Predictor, sha256
from train import calibrated


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--kind', choices=['baseline', 'int8', 'fp32'], required=True)
    parser.add_argument('--phase', choices=['validation', 'test', 'regression'], required=True)
    args = parser.parse_args()
    model = Predictor('/models', args.kind)
    startup = time.monotonic() - STARTED
    manifest = model.manifest
    dataset = manifest['dataset'] if args.kind == 'baseline' else manifest['training']['dataset']
    artifact = sha256(Path('/models') / ('training.json' if args.kind == 'baseline' else 'manifest.json'))
    fingerprint = hashlib.sha256((artifact + ':' + args.kind).encode()).hexdigest()
    corpus = Path('/data/corpus.jsonl')
    if corpus.stat().st_size > 32 * 1024 ** 2:
        raise ValueError('corpus size exceeds evaluation budget')
    if args.phase != 'regression' and sha256(corpus) != dataset['splits'][args.phase]['sha256']:
        raise ValueError('corpus does not match locked partition')
    rows = [json.loads(line) for line in corpus.read_text().splitlines() if line.strip()]
    if not 1 <= len(rows) <= 4096 or len({r['id'] for r in rows}) != len(rows):
        raise ValueError('invalid or duplicate corpus IDs')
    results = []
    for row in rows:
        before = time.monotonic()
        try:
            values, chunks, tokens = model.scores(row['text'])
            result = {'id': row['id'], 'source': row.get('source', ''), 'label': row['label'],
                      'scores': values, 'chunks': chunks, 'tokens': tokens}
        except (ValueError, RuntimeError) as error:
            result = {'id': row['id'], 'source': row.get('source', ''), 'label': row['label'], 'error': type(error).__name__}
        result['millis'] = (time.monotonic() - before) * 1000
        results.append(result)
    if args.phase == 'validation':
        if any('error' in row for row in results):
            raise ValueError('validation failed; cannot select thresholds')
        selection = calibrated(results)
        selection.update(model_fingerprint=fingerprint, validation_sha256=sha256(corpus), kind=args.kind)
    else:
        selection = json.loads(Path('/selection/selection.json').read_text())
        if selection['model_fingerprint'] != fingerprint or selection['validation_sha256'] != dataset['splits']['validation']['sha256']:
            raise ValueError('threshold/model provenance mismatch')
    for row in results:
        row['prediction'] = classify(row['scores'], selection['phishing_threshold'], selection['spam_threshold']) if 'scores' in row else 'error'
    measured = metrics(results)
    cohorts = {source: metrics([row for row in results if row['source'] == source]) for source in sorted({r['source'] for r in results})}
    report = {'phase': args.phase, 'kind': args.kind, 'model_fingerprint': fingerprint,
              'startup_seconds': startup, 'dataset_sha256': sha256(corpus), 'selection': selection,
              'metrics': measured, 'cohorts': cohorts, 'numerical_acceptance': acceptance(measured),
              'results': results, 'cgroup': {name: Path('/sys/fs/cgroup/' + name).read_text().strip() for name in
                    ['memory.max', 'memory.peak', 'memory.swap.max', 'memory.events', 'cpu.max', 'cpu.stat']}}
    print(json.dumps(report), flush=True)


if __name__ == '__main__':
    main()
