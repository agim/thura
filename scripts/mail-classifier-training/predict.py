"""CPU-only, no-network loading of app-trained artifacts; no pickle or remote model code."""
import hashlib
import json
from pathlib import Path

from common import LABELS, VERSION, windows


def sha256(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


class Predictor:
    def __init__(self, directory, kind):
        import numpy as np
        self.np, self.kind, self.directory = np, kind, Path(directory)
        self.manifest = json.loads((self.directory / ('training.json' if kind == 'baseline' else 'manifest.json')).read_text())
        if self.manifest['version'] != VERSION or self.manifest['labels'] != LABELS:
            raise ValueError('artifact version/labels mismatch')
        filename = 'model_int8.onnx' if kind == 'int8' else 'model.onnx'
        names = list(self.manifest['files']) if kind == 'baseline' else [filename, 'tokenizer.json', 'config.json']
        for name in names:
            if Path(name).name != name:
                raise ValueError('invalid artifact path')
            info = self.manifest['files'][name]
            target = self.directory / name
            if target.stat().st_size != info['bytes'] or sha256(target) != info['sha256']:
                raise ValueError('artifact checksum mismatch')
        if kind == 'baseline':
            from sklearn.feature_extraction.text import TfidfVectorizer
            self.vectorizers = []
            for name, kwargs in [('word', {'ngram_range': (1, 2)}), ('char', {'analyzer': 'char_wb', 'ngram_range': (3, 5)})]:
                vocabulary = json.loads((self.directory / (name + '-vocabulary.json')).read_text())
                vectorizer = TfidfVectorizer(vocabulary=vocabulary, sublinear_tf=True, dtype=np.float32, **kwargs)
                vectorizer.fit([''])
                vectorizer.idf_ = np.load(self.directory / (name + '-idf.npy'), allow_pickle=False)
                self.vectorizers.append(vectorizer)
            self.coefficients = np.load(self.directory / 'coefficients.npy', allow_pickle=False)
            self.intercept = np.load(self.directory / 'intercept.npy', allow_pickle=False)
        else:
            import onnxruntime as ort
            from tokenizers import Tokenizer
            if self.manifest['preprocessing_sha256'] != sha256(Path(__file__).with_name('common.py')):
                raise ValueError('preprocessing implementation changed')
            options = ort.SessionOptions()
            options.intra_op_num_threads = 1
            options.inter_op_num_threads = 1
            options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
            self.session = ort.InferenceSession(str(self.directory / filename), sess_options=options, providers=['CPUExecutionProvider'])
            self.tokenizer = Tokenizer.from_file(str(self.directory / 'tokenizer.json'))
            self.tokenizer.no_truncation()
            self.tokenizer.no_padding()

    def scores(self, text):
        np = self.np
        if not text.strip() or len(text.encode()) > 12000:
            raise ValueError('insufficient or oversized evidence; manual review required')
        if self.kind == 'baseline':
            from scipy.sparse import hstack
            features = hstack([vectorizer.transform([text]) for vectorizer in self.vectorizers], format='csr')
            pooled = np.asarray(features @ self.coefficients.T + self.intercept)[0]
            chunks, tokens = 1, None
        else:
            ids = self.tokenizer.encode(text, add_special_tokens=False).ids
            content = windows(ids)
            if len(content) > 32:
                raise ValueError('input exceeds window budget')
            logits = []
            for chunk in content:
                values = np.array([[101] + chunk + [102] + [0] * (510 - len(chunk))], dtype=np.int64)
                logits.append(self.session.run(None, {'input_ids': values, 'attention_mask': (values != 0).astype(np.int64)})[0][0])
            values = np.stack(logits)
            pooled = np.array([values[:, 0].min(), values[:, 1].max(), values[:, 2].max()], dtype=np.float64)
            chunks, tokens = len(content), len(ids)
        if pooled.shape != (3,) or not np.isfinite(pooled).all():
            raise ValueError('invalid classifier output')
        probability = np.exp(pooled - pooled.max())
        probability /= probability.sum()
        return probability.tolist(), chunks, tokens
