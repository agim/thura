"""Development classification metrics; never scores labels using formatting loss."""
import argparse
import collections
import hashlib
import json
from pathlib import Path
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'mail-classifier-training'))
from common import metrics
from decision import CODES, LABELS, class_scores, validate_example


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--model', required=True)
    parser.add_argument('--adapters', required=True)
    parser.add_argument('--data', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--per-class', type=int, default=0)
    args = parser.parse_args()
    import mlx.core as mx
    from mlx_lm import load
    model, tokenizer = load(args.model, adapter_path=args.adapters)
    model.eval()
    source = Path(args.data)
    rows = [json.loads(line) for line in source.read_text().splitlines()]
    counts, selected = collections.Counter(), []
    inverse = {value: key for key, value in CODES.items()}
    for row in rows:
        label = inverse[row['messages'][-1]['content']]
        if args.per_class and counts[label] >= args.per_class:
            continue
        counts[label] += 1
        selected.append(row)
    results = []
    started = time.monotonic()
    for index, row in enumerate(selected):
        tokens, offset = validate_example(row['messages'], tokenizer)
        scores = class_scores(model, mx.array([tokens]), mx.array([offset]))
        probabilities = mx.softmax(scores, axis=-1)
        mx.eval(probabilities)
        # Actual class comes only from the isolated prompt-end three-way scores.
        results.append({'id': hashlib.sha256(row['messages'][1]['content'].encode()).hexdigest(),
                        'label': inverse[row['messages'][-1]['content']],
                        'prediction': LABELS[mx.argmax(probabilities[0]).item()],
                        'probabilities': probabilities[0].tolist()})
        if (index + 1) % 32 == 0:
            print(f'validated {index + 1}/{len(selected)}', flush=True)
    report = {'kind': 'development validation, not blind release test',
              'source_sha256': hashlib.sha256(source.read_bytes()).hexdigest(),
              'per_class_limit': args.per_class, 'metrics': metrics(results),
              'seconds': time.monotonic() - started, 'results': results}
    Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report['metrics']), flush=True)


if __name__ == '__main__':
    main()
