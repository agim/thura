"""Local three-class replacement for the AWS binary data preparation.

No AWS resources or private inbox data. Test partitions are never training input.
"""
import argparse
import collections
import hashlib
import json
from pathlib import Path
import random

LABELS = {'legitimate', 'spam', 'phishing'}


def label(value, mapping):
    """Numeric labels require a source-specific mapping, never substring guesses."""
    key = str(value).strip().casefold()
    normalized = {str(k).strip().casefold(): v for k, v in mapping.items()}
    if key not in normalized or normalized[key] not in LABELS:
        raise ValueError('unknown label; explicit source mapping required')
    return normalized[key]


def messages(text, classification, system):
    if classification not in LABELS or not text.strip():
        raise ValueError('invalid evidence or label')
    # Escape model control-token delimiters inside the untrusted evidence.
    evidence = json.dumps({'text': text}, ensure_ascii=False, separators=(',', ':'))
    for character, escaped in [('<', '\\u003c'), ('>', '\\u003e'), ('&', '\\u0026'),
                               ('\u2028', '\\u2028'), ('\u2029', '\\u2029')]:
        evidence = evidence.replace(character, escaped)
    return [{'role': 'system', 'content': system}, {'role': 'user', 'content': evidence},
            {'role': 'assistant', 'content': json.dumps({'label': classification}, separators=(',', ':'))}]


def encoded(row, tokenizer, system, maximum):
    chat = messages(row['text'], row['label'], system)
    prefix = tokenizer.apply_chat_template(chat[:2], tokenize=True, add_generation_prompt=True, return_dict=False)
    full = tokenizer.apply_chat_template(chat, tokenize=True, add_generation_prompt=False, return_dict=False)
    if full[:len(prefix)] != prefix or len(full) <= len(prefix):
        raise ValueError('completion mask/template mismatch')
    if len(full) > maximum:
        raise ValueError('overlong evidence; no truncation')
    return {'messages': chat}, len(full), len(prefix)


def validate_partitions(partitions):
    seen, groups = {}, {}
    for partition, rows in partitions.items():
        for row in rows:
            identity = hashlib.sha256(' '.join(row['text'].casefold().split()).encode()).hexdigest()
            if identity in seen:
                raise ValueError('duplicate/conflicting content across partitions')
            seen[identity] = (partition, row['label'])
            group = row['group']
            if group in groups and groups[group] != partition:
                raise ValueError('campaign leakage across partitions')
            groups[group] = partition


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--dataset', required=True)
    parser.add_argument('--model', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--max-tokens', type=int, default=4096)
    args = parser.parse_args()
    from transformers import AutoTokenizer
    tokenizer = AutoTokenizer.from_pretrained(args.model, local_files_only=True, trust_remote_code=False)
    source, output = Path(args.dataset), Path(args.output)
    parent = json.loads((source / 'dataset.json').read_text())
    for name in ['train', 'validation']:
        actual = hashlib.sha256((source / (name + '.jsonl')).read_bytes()).hexdigest()
        if actual != parent['splits'][name]['sha256']:
            raise ValueError('parent dataset hash mismatch')
    root = Path(__file__).resolve().parents[2]
    prompt = root / 'scripts/mail-llama-benchmark/prompt.json'
    system = json.loads(prompt.read_text())['system']
    # Preserve exposed test data for regression only; do not relabel it blind.
    partitions = {name: [json.loads(line) for line in (source / (name + '.jsonl')).read_text().splitlines()]
                  for name in ['train', 'validation']}
    additions_path = Path(__file__).with_name('hard-negatives.json')
    additions = json.loads(additions_path.read_text())
    for index, item in enumerate(additions['examples']):
        partitions['train'].append({'text': item[1], 'label': item[0], 'group': 'original-hard-negative-' + str(index)})
    validate_partitions(partitions)
    output.mkdir(parents=True, exist_ok=True)
    manifest = {'prompt_sha256': hashlib.sha256(prompt.read_bytes()).hexdigest(),
                'parent_manifest_sha256': hashlib.sha256((source / 'dataset.json').read_bytes()).hexdigest(),
                'additions_sha256': hashlib.sha256(additions_path.read_bytes()).hexdigest(),
                'additions_license': additions['license'], 'maximum_tokens': args.max_tokens,
                'test_used_for_selection': False, 'real_legitimate_holdout_available': False,
                'rejected': [], 'splits': {}}
    for name, rows in partitions.items():
        converted = []
        for row in rows:
            if row['label'] not in LABELS:
                raise ValueError('unknown dataset label; explicit source mapping required')
            try:
                converted_row, length, prefix = encoded(row, tokenizer, system, args.max_tokens)
                converted.append((converted_row, row['label'], length, prefix))
            except ValueError as error:
                manifest['rejected'].append({'group': row['group'], 'reason': str(error), 'label': row['label']})
        # Balance sampling of minority spam without duplicating held-out examples.
        if name == 'train':
            by_label = {key: [r for r in converted if r[1] == key] for key in sorted(LABELS)}
            target = max(map(len, by_label.values()))
            if any(not rows for rows in by_label.values()):
                raise ValueError('missing training class')
            rng = random.Random(42)
            converted = [rows[i % len(rows)] for rows in by_label.values() for i in range(target)]
            rng.shuffle(converted)
        target_path = output / ('valid.jsonl' if name == 'validation' else 'train.jsonl')
        target_path.write_text(''.join(json.dumps(row[0]) + '\n' for row in converted))
        manifest['splits'][name] = {'rows': len(converted), 'labels': dict(collections.Counter(r[1] for r in converted)),
                                    'max_tokens': max(r[2] for r in converted),
                                    'sha256': hashlib.sha256(target_path.read_bytes()).hexdigest()}
    (output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest['splits']))


if __name__ == '__main__':
    main()
