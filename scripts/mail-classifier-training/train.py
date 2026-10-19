"""Local supervised DistilBERT training. Test data is never opened here."""
import argparse
import collections
import hashlib
import json
import math
import os
from pathlib import Path
import random
import time

os.environ['HF_HUB_OFFLINE'] = '1'
os.environ['TOKENIZERS_PARALLELISM'] = 'false'
os.environ['PYTORCH_ENABLE_MPS_FALLBACK'] = '0'

from common import LABELS, VERSION, acceptance, classify, metrics, windows


def load_partition(path, expected):
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != expected:
        raise ValueError('partition checksum changed')
    return [json.loads(line) for line in data.splitlines() if line.strip()]


def calibrated(rows):
    best = None
    for phishing in [.5, .6, .7, .8, .9, .95, .98, .99]:
        for spam in [.5, .6, .7, .8, .9, .95, .98, .99]:
            predictions = [{**row, 'prediction': classify(row['scores'], phishing, spam)} for row in rows]
            measured = metrics(predictions)
            utility = (measured['phishing_recall'] + measured['spam_recall']
                       - 5 * measured['legitimate_false_positive_rate'] - measured['spam_as_phishing_rate'])
            rank = (acceptance(measured), utility, measured['accuracy'])
            candidate = {'phishing_threshold': phishing, 'spam_threshold': spam, 'metrics': measured, 'rank': rank}
            if best is None or rank > best['rank']:
                best = candidate
    return best


def main():
    import torch
    from transformers import AutoModelForSequenceClassification, AutoTokenizer
    parser = argparse.ArgumentParser()
    parser.add_argument('--base', required=True)
    parser.add_argument('--dataset', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--epochs', type=int, default=3)
    parser.add_argument('--batch-size', type=int, default=4)
    args = parser.parse_args()
    if args.epochs < 1 or args.epochs > 3 or args.batch_size not in [1, 2, 4, 8]:
        raise ValueError('training budget must be explicit and bounded')
    random.seed(42)
    torch.manual_seed(42)
    torch.set_num_threads(4)
    device = torch.device('mps' if torch.backends.mps.is_available() else 'cpu')
    base, directory, output = Path(args.base), Path(args.dataset), Path(args.output)
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    pinned_base = json.loads(Path(__file__).with_name('base.json').read_text())
    for name, info in pinned_base['files'].items():
        path = base / name
        if path.stat().st_size != info['bytes'] or hashlib.sha256(path.read_bytes()).hexdigest() != info['sha256']:
            raise ValueError('base artifact hash mismatch')
    manifest = json.loads((directory / 'dataset.json').read_text())
    if manifest['version'] != VERSION or manifest['labels'] != LABELS:
        raise ValueError('unsupported dataset contract')
    train = load_partition(directory / 'train.jsonl', manifest['splits']['train']['sha256'])
    validation = load_partition(directory / 'validation.jsonl', manifest['splits']['validation']['sha256'])
    if {r['group'] for r in train} & {r['group'] for r in validation}:
        raise ValueError('campaign leakage between training and validation')
    tokenizer = AutoTokenizer.from_pretrained(str(base), local_files_only=True, trust_remote_code=False)
    model = AutoModelForSequenceClassification.from_pretrained(str(base), local_files_only=True,
                trust_remote_code=False, num_labels=3, id2label=dict(enumerate(LABELS)),
                label2id={label: index for index, label in enumerate(LABELS)}, attn_implementation='eager').to(device)
    # All windows contribute to the document loss; no silent first-512-token truncation.
    for partition in [train, validation]:
        for row in partition:
            row['chunks'] = windows(tokenizer.encode(row['text'], add_special_tokens=False, truncation=False))
            if len(row['chunks']) > 32:
                raise ValueError('email exceeds explicit window budget')
    counts = collections.Counter(row['label'] for row in train)
    class_weights = torch.tensor([len(train) / (3 * counts[label]) for label in LABELS], device=device)
    optimizer = torch.optim.AdamW(model.parameters(), lr=2e-5, weight_decay=.01)
    loss_function = torch.nn.CrossEntropyLoss(weight=class_weights)
    steps = math.ceil(len(train) / args.batch_size) * args.epochs
    started, step, best_rank = time.monotonic(), 0, None
    history = []

    def documents(rows, gradients):
        chunks, ranges = [], []
        for row in rows:
            start = len(chunks)
            chunks.extend(row['chunks'])
            ranges.append((start, len(chunks)))
        logits = []
        # Bound each forward pass to four windows; retain the document graph for pooling.
        for offset in range(0, len(chunks), 4):
            current = chunks[offset:offset + 4]
            ids = torch.tensor([[101] + chunk + [102] + [0] * (510 - len(chunk)) for chunk in current], device=device)
            mask = (ids != 0).long()
            with torch.set_grad_enabled(gradients):
                logits.append(model(input_ids=ids, attention_mask=mask).logits)
        values = torch.cat(logits)
        return torch.stack([torch.stack([values[start:end, 0].min(), values[start:end, 1].max(),
                                        values[start:end, 2].max()]) for start, end in ranges])

    for epoch in range(args.epochs):
        order = list(range(len(train)))
        random.Random(42 + epoch).shuffle(order)
        model.train()
        for offset in range(0, len(order), args.batch_size):
            batch = [train[index] for index in order[offset:offset + args.batch_size]]
            optimizer.zero_grad(set_to_none=True)
            logits = documents(batch, True)
            labels = torch.tensor([LABELS.index(row['label']) for row in batch], device=device)
            loss = loss_function(logits, labels)
            loss.backward()
            total_loss = float(loss.detach().cpu())
            torch.nn.utils.clip_grad_norm_(model.parameters(), 1.0)
            step += 1
            progress = step / steps
            rate = min(1, progress / .1) * max(.01, 1 - progress)
            for group in optimizer.param_groups:
                group['lr'] = 2e-5 * rate
            optimizer.step()
            if step % 25 == 0:
                print(json.dumps({'phase': 'training', 'device': str(device), 'epoch': epoch + 1,
                                  'step': step, 'steps': steps, 'loss': total_loss,
                                  'elapsed_seconds': time.monotonic() - started}), flush=True)
        model.eval()
        scores = []
        with torch.no_grad():
            for row in validation:
                probabilities = torch.softmax(documents([row], False)[0], dim=-1).cpu().tolist()
                scores.append({'id': row['id'], 'label': row['label'], 'scores': probabilities})
        selected = calibrated(scores)
        history.append({'epoch': epoch + 1, **selected})
        print(json.dumps({'phase': 'validation', 'epoch': epoch + 1, **selected}), flush=True)
        if best_rank is None or tuple(selected['rank']) > best_rank:
            best_rank = tuple(selected['rank'])
            model.save_pretrained(str(output / 'checkpoint'), safe_serialization=True)
            tokenizer.save_pretrained(str(output / 'checkpoint'))
            (output / 'selected.json').write_text(json.dumps({'epoch': epoch + 1, **selected}, indent=2) + '\n')
    metadata = {'version': VERSION, 'labels': LABELS, 'base': pinned_base, 'dataset': manifest,
                'device': str(device), 'torch': torch.__version__, 'epochs': args.epochs, 'seed': 42,
                'training_seconds': time.monotonic() - started, 'history': history,
                'pooling': 'minimum legitimate logit, maximum spam/phishing logits over every overlapping window',
                'test_data_read': False}
    (output / 'training.json').write_text(json.dumps(metadata, indent=2) + '\n')
    print(json.dumps({'phase': 'complete', 'device': str(device), 'training_seconds': metadata['training_seconds']}), flush=True)


if __name__ == '__main__':
    main()
