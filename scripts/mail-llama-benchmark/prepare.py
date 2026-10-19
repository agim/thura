"""Fetch pinned public model/license blobs and build a known regression pilot."""
import argparse
import hashlib
import json
from pathlib import Path
import random
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts' / 'mail-classifier-training'))
from fetch import fetch


def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    args = parser.parse_args()
    root = Path(args.root)
    pinned = json.loads(Path(__file__).with_name('artifacts.json').read_text())
    directory = root / 'llama1b'
    directory.mkdir(parents=True, exist_ok=True)
    fetch(pinned['registry_manifest']['url'], directory / 'registry-manifest.json', pinned['registry_manifest'])
    for layer in pinned['layers']:
        digest = layer['digest'].split(':')[1]
        name = 'model.gguf' if layer['mediaType'].endswith('.model') else digest + '.license.txt'
        target = directory / name
        fetch('https://registry.ollama.ai/v2/library/llama3.2/blobs/' + layer['digest'], target,
              {'sha256': digest, 'bytes': layer['size']})
        if name == 'model.gguf':
            target.chmod(0o644)
    cases = ROOT / 'scripts' / 'mail-classifier-benchmark' / 'cases.json'
    validation = root / 'dataset-locked' / 'validation.jsonl'
    if sha(cases) != pinned['authored_cases_sha256'] or sha(validation) != pinned['validation_sha256']:
        raise ValueError('pilot source hash changed')
    rows = []
    for row in json.loads(cases.read_text()):
        identity = row['id']
        source = 'instruction' if identity.startswith('challenge-instruction') else ('tail' if identity.startswith('challenge-tail') else 'authored')
        rows.append({'id': identity, 'source': source, 'label': 'legitimate' if row['expected'] == 'safe' else 'phishing', 'text': row['text']})
    spam = [json.loads(line) for line in validation.read_text().splitlines() if json.loads(line)['label'] == 'spam']
    rows.extend(spam[:20])
    random.Random(42).shuffle(rows)
    if len(rows) != 96 or any(len(row['text'].encode()) > 12000 for row in rows):
        raise ValueError('pilot contract changed')
    pilot = root / 'llama-pilot.json'
    pilot.write_text(json.dumps(rows))
    pilot.chmod(0o600)
    print(json.dumps({'cases': len(rows), 'pilot_sha256': sha(pilot), 'no_final_test_read': True}))
