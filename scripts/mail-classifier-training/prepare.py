"""Build traceable licensed three-class splits; later phishing stays held out."""
import argparse
import collections
import csv
from email import policy
from email.parser import BytesParser
import hashlib
import io
import json
import mailbox
from pathlib import Path, PurePosixPath
import re

from common import LABELS, VERSION, canonical, deidentify, digest, email_text, shingles

PROMOTION = re.compile(r'\b(?:backlinks?|seo|search engine optimi[sz]ation|web design|website design|digital marketing|guest posts?|content writing|business leads|lead generation|translation services)\b', re.I)
AMBIGUOUS = re.compile(r'\b(?:password|credential|verify|verification|account suspended|sign.in|log.in|bank|wire transfer|gift cards?|seed phrase|recovery codes?|bitcoin|crypto|wallet|inheritance|lottery|investment|payment|credit card|social security|enable macros|remote access|security alert|reward|prize|winner|loyalty|receipt|invoice|refund|costco|walmart|amazon|million|partnership proposal|funds|miracle|ice hack|renewal by)\b', re.I)


def sha256(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def verify_sources(directory):
    manifest = json.loads(Path(__file__).with_name('sources.json').read_text())
    for name, info in manifest.items():
        target = directory / name
        if target.stat().st_size != info['bytes'] or sha256(target) != info['sha256']:
            raise ValueError('source hash mismatch: ' + name)
    return manifest


def source_row(source, path, label, text, forced=''):
    if label not in LABELS or not text.strip() or len(text.encode()) > 12000:
        raise ValueError('invalid prepared row')
    return {'id': digest(source + ':' + path), 'source': source, 'source_id': path,
            'label': label, 'text': text, 'forced_split': forced, 'content_sha256': digest(canonical(text))}


def parse(directory):
    import py7zr
    from py7zr.io import BytesIOFactory
    rows, excluded = [], collections.Counter()
    with (directory / 'GPT_Phishing_Email_dataset.csv').open(encoding='utf-8-sig', newline='') as source:
        for index, row in enumerate(csv.DictReader(source)):
            if row['label'] not in ['0', '1']:
                raise ValueError('unknown source label')
            text = deidentify(row['content'])
            rows.append(source_row('llmgen-english', str(index), 'legitimate' if row['label'] == '0' else 'phishing', text))
    for year in [2022, 2023, 2024, 2025]:
        forced = 'train' if year < 2024 else ('validation' if year == 2024 else 'test')
        messages = mailbox.mbox(directory / f'phishing-{year}', factory=lambda stream: BytesParser(policy=policy.default).parse(stream), create=False)
        for index, message in enumerate(messages):
            try:
                text = email_text(message)
                rows.append(source_row('nazario-' + str(year), str(index), 'phishing', text, forced))
            except (ValueError, TypeError, LookupError, UnicodeError):
                excluded['nazario_insufficient_or_oversized_evidence'] += 1
        messages.close()
    # Bounded in-memory extraction; archive paths are never written to disk.
    for period in ['2022', '2023', '2024', '2025', '2026-01', '2026-02', '2026-03', '2026-04']:
        forced = 'train' if period < '2024' else ('validation' if period == '2024' else 'test')
        with py7zr.SevenZipFile(directory / f'spam-{period}.7z') as archive:
            files = archive.list()
            if len(files) > 20000 or sum(f.uncompressed or 0 for f in files) > 512 * 1024 ** 2:
                raise ValueError('spam archive exceeds extraction budget')
            targets = []
            for file in files:
                path = PurePosixPath(file.filename)
                if path.is_absolute() or '..' in path.parts:
                    raise ValueError('unsafe archive path')
                if not file.is_directory and 0 < (file.uncompressed or 0) <= 2 * 1024 ** 2:
                    targets.append(file.filename)
            factory = BytesIOFactory(limit=2 * 1024 ** 2)
            archive.extract(targets=targets, factory=factory)
            for name in sorted(targets):
                try:
                    stream = factory.get(name)
                    stream.seek(0)
                    message = BytesParser(policy=policy.default).parsebytes(stream.read())
                    text = email_text(message)
                    if not PROMOTION.search(text) or AMBIGUOUS.search(text):
                        excluded['spam_ambiguous_or_not_explicit_promotion'] += 1
                        continue
                    rows.append(source_row('guenter-spam-' + period, name, 'spam', text, forced))
                except (ValueError, TypeError, LookupError, UnicodeError):
                    excluded['spam_insufficient_or_oversized_evidence'] += 1
    return rows, dict(excluded)


def split(rows):
    from datasketch import MinHash, MinHashLSH
    # Global exact/near-duplicate grouping across all sources and years, label-independent.
    parent = list(range(len(rows)))
    def find(index):
        while parent[index] != index:
            parent[index] = parent[parent[index]]
            index = parent[index]
        return index
    def union(a, b):
        parent[find(a)] = find(b)
    exact, subject = {}, {}
    index = MinHashLSH(threshold=.8, num_perm=64)
    signatures = []
    for i, row in enumerate(rows):
        identity = row['content_sha256']
        if identity in exact:
            union(i, exact[identity])
        exact[identity] = i
        # Group repeated subject templates even if body wording differs.
        match = re.search(r'\bsubject:\s*(.*?)(?:\bfrom:|\breply-to:|$)', row['text'], re.I)
        if match:
            key = canonical(match[1])
            if len(key.split()) >= 4:
                if key in subject:
                    union(i, subject[key])
                subject[key] = i
        tokens = shingles(row['text'])
        signature = MinHash(num_perm=64, seed=42)
        signature.update_batch([token.encode() for token in sorted(tokens)])
        for candidate in index.query(signature):
            j = int(candidate)
            other = signatures[j]
            similarity = len(tokens & other) / len(tokens | other)
            if similarity >= .8:
                union(i, j)
        signatures.append(tokens)
        index.insert(str(i), signature)
    groups = collections.defaultdict(list)
    for i, row in enumerate(rows):
        groups[find(i)].append(row)
    result = {'train': [], 'validation': [], 'test': []}
    excluded = collections.Counter()
    for members in groups.values():
        # Exact identical messages with conflicting labels are excluded, never guessed.
        labels_by_identity = collections.defaultdict(set)
        for row in members:
            labels_by_identity[row['content_sha256']].add(row['label'])
        if any(len(labels) > 1 for labels in labels_by_identity.values()):
            excluded['conflicting_exact_label_group'] += len(members)
            continue
        group = min(row['content_sha256'] for row in members)
        forced = {row['forced_split'] for row in members if row['forced_split']}
        destination = 'test' if 'test' in forced else ('validation' if 'validation' in forced else ('train' if forced else ''))
        if not destination:
            portion = int(digest('thura-split-42:' + group)[:8], 16) % 100
            destination = 'train' if portion < 70 else ('validation' if portion < 85 else 'test')
        seen = set()
        for row in sorted(members, key=lambda item: item['id']):
            if row['forced_split'] and row['forced_split'] != destination:
                excluded['older_campaign_overlaps_later_partition'] += 1
                continue
            if row['content_sha256'] in seen:
                excluded['exact_duplicate'] += 1
                continue
            seen.add(row['content_sha256'])
            result[destination].append({**row, 'group': group, 'split': destination})
    return result, dict(excluded)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--raw', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    raw, output = Path(args.raw), Path(args.output)
    sources = verify_sources(raw)
    records, exclusions = parse(raw)
    print(json.dumps({'parsed': len(records), 'excluded': exclusions}), flush=True)
    partitions, duplicates = split(records)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    manifest = {'version': VERSION, 'labels': LABELS, 'sources': sources,
                'exclusions': {**exclusions, **duplicates}, 'splits': {},
                'license_map': {'llmgen-english': 'Apache-2.0', 'nazario': 'CC-BY-4.0', 'guenter-spam': 'LicenseRef-Guenter-Unrestricted'},
                'limitations': 'Legitimate mail is synthetic, and ordinary spam is conservatively filtered. Grouping detects exact and high-similarity copies, not all semantic campaigns. No raw redistribution.'}
    for name, rows in partitions.items():
        target = output / (name + '.jsonl')
        rows.sort(key=lambda row: row['id'])
        target.write_text(''.join(json.dumps(row) + '\n' for row in rows))
        target.chmod(0o600)
        counts = dict(collections.Counter(row['label'] for row in rows))
        manifest['splits'][name] = {'count': len(rows), 'labels': counts, 'sha256': sha256(target)}
        if any(counts.get(label, 0) < (100 if name == 'test' else 50) for label in LABELS):
            raise ValueError('insufficient independent examples in a partition: ' + name)
    (output / 'dataset.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest['splits']), flush=True)


if __name__ == '__main__':
    main()
