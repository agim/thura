"""Compare actual Go-equivalent requests and tokens against native class scores.

The diagnostic /apply-template and /tokenize APIs do not run model inference.
Actual CPU predictions remain the separate official-LLM-pack benchmark.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time

from decision import CONTRACT, LABELS, class_scores
from serving import evidence, load_profile


def post(container, endpoint, data):
    response = subprocess.run(['docker', 'exec', '-i', container, 'curl', '-fsS', '--max-time', '120',
                               'http://127.0.0.1:8080/' + endpoint, '-H', 'Content-Type: application/json', '-d', '@-'],
                              input=json.dumps(data), capture_output=True, text=True, check=True, timeout=130)
    return json.loads(response.stdout)


def main():
    parser = argparse.ArgumentParser()
    for name in ['model', 'adapters', 'corpus', 'container', 'serving-profile', 'output']:
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args()
    profile, _ = load_profile(args.serving_profile)
    import mlx.core as mx
    from mlx_lm import load
    model, tokenizer = load(args.model, adapter_path=args.adapters)
    model.eval()
    corpus = Path(args.corpus)
    rows = json.loads(corpus.read_text())
    started = time.monotonic()
    results = []
    for index, row in enumerate(rows):
        messages = [{'role': 'system', 'content': CONTRACT['system']}, {'role': 'user', 'content': evidence(row['text'])}]
        tokens = tokenizer.apply_chat_template(messages, add_generation_prompt=True, return_dict=False, date_string=profile['template_date'])
        if len(tokens) > 4096:
            raise ValueError('overlong request: never truncate')
        server = post(args.container, 'apply-template', {'messages': messages})['prompt']
        actual = post(args.container, 'tokenize', {'content': server, 'add_special': True})['tokens']
        if actual != tokens:
            raise ValueError(f'native/server token mismatch at row {index}; do not compare predictions')
        # The expected label never enters forward input or the HTTP request.
        scores = class_scores(model, mx.array([tokens]), mx.array([len(tokens)]))
        mx.eval(scores)
        results.append({'id': row['id'], 'label': row['label'], 'source': row['source'],
                        'prediction': LABELS[mx.argmax(scores[0]).item()], 'class_logits': scores[0].tolist(),
                        'tokens': len(tokens), 'tokens_sha256': hashlib.sha256(json.dumps(tokens).encode()).hexdigest(),
                        'template_tokens_match': True})
        report = {'kind': 'paired diagnostic on exposed regression; not a blind release test',
                  'corpus_sha256': hashlib.sha256(corpus.read_bytes()).hexdigest(), 'serving_profile': profile,
                  'seconds': time.monotonic() - started, 'results': results}
        Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
        print(f'paired {index + 1}/{len(rows)}', flush=True)


if __name__ == '__main__':
    main()
