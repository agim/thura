"""Add original training-only contrasts after split, with a cross-partition duplicate fence."""
import argparse
import collections
import hashlib
import json
from pathlib import Path

from common import canonical, deidentify, digest, shingles
from prepare import sha256


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--dataset', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    source, output = Path(args.dataset), Path(args.output)
    manifest = json.loads((source / 'dataset.json').read_text())
    rows = {name: [json.loads(line) for line in (source / (name + '.jsonl')).read_text().splitlines()]
            for name in ['train', 'validation', 'test']}
    for name in rows:
        if sha256(source / (name + '.jsonl')) != manifest['splits'][name]['sha256']:
            raise ValueError('source dataset changed')
    contrast_file = Path(__file__).with_name('contrasts.json')
    contrasts = json.loads(contrast_file.read_text())
    added = []
    names = ['Morgan', 'Taylor', 'Jordan', 'Riley', 'Casey', 'Avery', 'Robin', 'Jamie']
    for index, (subject, legitimate, phishing) in enumerate(contrasts['pairs']):
        for name in names:
            for label, body in [('legitimate', legitimate), ('phishing', phishing)]:
                # Both classes have identical format and greeting variations; no label-specific sender/URL cue.
                text = deidentify(f'Subject: {subject}\nHello {name}, {body}\nRegards, the support team.')
                added.append({'id': digest('thura-contrast:' + text), 'source': 'thura-original-contrasts',
                              'source_id': f'{index}-{name}-{label}', 'label': label, 'text': text,
                              'forced_split': 'train', 'content_sha256': digest(canonical(text)),
                              'group': 'original-contrast-' + str(index), 'split': 'train'})
    heldout = [(row['content_sha256'], shingles(row['text'])) for name in ['validation', 'test'] for row in rows[name]]
    for row in added:
        tokens = shingles(row['text'])
        if any(identity == row['content_sha256'] or len(tokens & other) / len(tokens | other) >= .8 for identity, other in heldout):
            raise ValueError('training contrast overlaps held-out content')
    rows['train'].extend(added)
    manifest['augmentation'] = {'source_sha256': hashlib.sha256(contrast_file.read_bytes()).hexdigest(),
                              'license': contrasts['license'], 'creator': contrasts['creator'],
                              'training_only': True, 'rows': len(added), 'families': len(contrasts['pairs']),
                              'parent_splits': manifest['splits']}
    manifest['license_map']['thura-original-contrasts'] = 'Apache-2.0'
    manifest['splits'] = {}
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    for name, partition in rows.items():
        partition.sort(key=lambda row: row['id'])
        target = output / (name + '.jsonl')
        target.write_text(''.join(json.dumps(row) + '\n' for row in partition))
        target.chmod(0o600)
        manifest['splits'][name] = {'count': len(partition), 'labels': dict(collections.Counter(row['label'] for row in partition)), 'sha256': sha256(target)}
    (output / 'dataset.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest['splits']), flush=True)


if __name__ == '__main__':
    main()
