"""Optional research corpus preparation. Requires pyarrow; never bundle the upstream data."""
import argparse
import hashlib
import json
from pathlib import Path
from prepare import download

DATASET_REVISION = 'afba38f4a7ffbe89259ab68ae7c6426f944496cc'
DATASET_SHA256 = 'ddbedae0c45d52b0c950c9831a8a6b6db13de27ea1cd3e5e3c09f32e692214dc'


def main():
    import pyarrow.parquet as pq
    parser = argparse.ArgumentParser()
    parser.add_argument('--output-directory', required=True)
    args = parser.parse_args()
    directory = Path(args.output_directory)
    directory.mkdir(parents=True, exist_ok=True)
    parquet = directory / 'publisher-test.parquet'
    url = f'https://huggingface.co/datasets/cybersectony/PhishingEmailDetectionv2.0/resolve/{DATASET_REVISION}/data/test-00000-of-00001.parquet'
    download(url, parquet, 9212615, DATASET_SHA256)
    selected = []
    rows = pq.read_table(parquet).to_pylist()
    for label in range(4):
        unique = {row['content'] for row in rows if row['label'] == label
                  and row['content'].strip() not in ['', 'empty'] and len(row['content'].encode()) <= 12000}
        for text in sorted(unique, key=lambda value: hashlib.sha256(value.encode()).hexdigest())[:100]:
            digest = hashlib.sha256(text.encode()).hexdigest()
            selected.append({'id': f'publisher-test-{label}-{digest[:16]}', 'label': label,
                             'expected': 'phishing' if label in [1, 3] else 'safe', 'text': text})
    authored = json.loads(Path(__file__).with_name('cases.json').read_text())
    (directory / 'all-cases.json').write_text(json.dumps(authored + selected, indent=2) + '\n')
    print(json.dumps({'publisher_cases': len(selected), 'authored_cases': len(authored),
                      'dataset_revision': DATASET_REVISION, 'dataset_sha256': DATASET_SHA256}))


if __name__ == '__main__':
    main()
