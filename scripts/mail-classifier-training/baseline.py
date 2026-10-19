"""Fast locally trained comparator, selected on validation only. No pickle artifacts."""
import argparse
import hashlib
import json
from pathlib import Path
import time

from common import LABELS, VERSION
from train import calibrated, load_partition


def main():
    import numpy as np
    from scipy.sparse import hstack
    from sklearn.feature_extraction.text import TfidfVectorizer
    from sklearn.linear_model import LogisticRegression
    parser = argparse.ArgumentParser()
    parser.add_argument('--dataset', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    directory, output = Path(args.dataset), Path(args.output)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    manifest = json.loads((directory / 'dataset.json').read_text())
    train = load_partition(directory / 'train.jsonl', manifest['splits']['train']['sha256'])
    validation = load_partition(directory / 'validation.jsonl', manifest['splits']['validation']['sha256'])
    if {r['group'] for r in train} & {r['group'] for r in validation}:
        raise ValueError('campaign leakage')
    started = time.monotonic()
    word = TfidfVectorizer(ngram_range=(1, 2), max_features=20000, min_df=2, sublinear_tf=True, dtype=np.float32)
    char = TfidfVectorizer(analyzer='char_wb', ngram_range=(3, 5), max_features=60000, min_df=2, sublinear_tf=True, dtype=np.float32)
    text = [row['text'] for row in train]
    features = hstack([word.fit_transform(text), char.fit_transform(text)], format='csr')
    model = LogisticRegression(C=2, class_weight='balanced', max_iter=1000, solver='lbfgs')
    model.fit(features, [LABELS.index(row['label']) for row in train])
    values = hstack([word.transform([row['text'] for row in validation]), char.transform([row['text'] for row in validation])], format='csr')
    probabilities = model.predict_proba(values)
    scores = [{'id': row['id'], 'label': row['label'], 'scores': probability.tolist()} for row, probability in zip(validation, probabilities)]
    selected = calibrated(scores)
    metadata = {'version': VERSION, 'labels': LABELS, 'kind': 'word-char-tfidf-logistic',
                'dataset': manifest, 'training_seconds': time.monotonic() - started,
                'selected': selected, 'test_data_read': False}
    for name, vectorizer in [('word', word), ('char', char)]:
        (output / (name + '-vocabulary.json')).write_text(json.dumps({key: int(value) for key, value in vectorizer.vocabulary_.items()}))
        np.save(output / (name + '-idf.npy'), vectorizer.idf_, allow_pickle=False)
    np.save(output / 'coefficients.npy', model.coef_, allow_pickle=False)
    np.save(output / 'intercept.npy', model.intercept_, allow_pickle=False)
    metadata['files'] = {path.name: {'bytes': path.stat().st_size, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
                         for path in output.iterdir() if path.name.endswith(('.npy', '-vocabulary.json'))}
    (output / 'training.json').write_text(json.dumps(metadata, indent=2) + '\n')
    print(json.dumps({'training_seconds': metadata['training_seconds'], 'selected': selected}), flush=True)


if __name__ == '__main__':
    main()
