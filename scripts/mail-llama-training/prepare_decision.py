"""Change only the objective/contract; preserve the previous split and evidence."""
import argparse
import collections
import hashlib
import json
from pathlib import Path
from decision import CONTRACT, CODES, PROMPT, TOKEN_IDS, validate_example


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', required=True)
    parser.add_argument('--model', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    from transformers import AutoTokenizer
    tokenizer = AutoTokenizer.from_pretrained(args.model, local_files_only=True, trust_remote_code=False)
    if [tokenizer.encode(CODES[label], add_special_tokens=False) for label in ['legitimate', 'spam', 'phishing']] != [[x] for x in TOKEN_IDS]:
        raise ValueError('tokenizer differs from one-token codebook')
    source, output = Path(args.source), Path(args.output)
    parent = json.loads((source / 'manifest.json').read_text())
    output.mkdir(parents=True, exist_ok=True)
    manifest = {'objective': 'three-way-class-cross-entropy-at-prompt-end-v2',
                'parent_manifest_sha256': sha(source / 'manifest.json'),
                'prompt_sha256': sha(PROMPT), 'codebook': CODES, 'class_token_ids': TOKEN_IDS,
                'supervised_decisions_per_example': 1, 'syntax_loss_weight': 0,
                'real_legitimate_holdout_available': False, 'splits': {}}
    for name, filename in [('train', 'train'), ('validation', 'valid')]:
        path = source / (filename + '.jsonl')
        if sha(path) != parent['splits'][name]['sha256']:
            raise ValueError('source split hash mismatch')
        rows, counts = [], collections.Counter()
        for line in path.read_text().splitlines():
            item = json.loads(line)
            label = json.loads(item['messages'][-1]['content'])['label']
            if label not in CODES:
                raise ValueError('unknown source class')
            chat = [{'role': 'system', 'content': CONTRACT['system']},
                    item['messages'][1], {'role': 'assistant', 'content': CODES[label]}]
            tokens, offset = validate_example(chat, tokenizer)
            rows.append({'messages': chat})
            counts[label] += 1
        target = output / (filename + '.jsonl')
        target.write_text(''.join(json.dumps(r) + '\n' for r in rows))
        manifest['splits'][name] = {'rows': len(rows), 'labels': dict(counts), 'sha256': sha(target)}
    (output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest['splits']))


if __name__ == '__main__':
    main()
