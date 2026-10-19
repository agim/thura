"""Export only the validation-selected checkpoint; never read final test data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil

os.environ['HF_HUB_OFFLINE'] = '1'
from common import LABELS, VERSION


def sha256(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def main():
    import torch
    from transformers import AutoModelForSequenceClassification
    from onnxruntime.quantization import QuantType, quantize_dynamic
    parser = argparse.ArgumentParser()
    parser.add_argument('--training', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    training, output = Path(args.training), Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    metadata = json.loads((training / 'training.json').read_text())
    if metadata['version'] != VERSION or metadata['labels'] != LABELS or metadata['test_data_read']:
        raise ValueError('invalid training provenance')
    model = AutoModelForSequenceClassification.from_pretrained(str(training / 'checkpoint'), local_files_only=True,
                trust_remote_code=False, attn_implementation='eager').cpu().eval()
    if [model.config.id2label[i] for i in range(3)] != LABELS:
        raise ValueError('checkpoint label order changed')

    class Logits(torch.nn.Module):
        def __init__(self, wrapped):
            super().__init__()
            self.wrapped = wrapped
        def forward(self, input_ids, attention_mask):
            return self.wrapped(input_ids=input_ids, attention_mask=attention_mask).logits

    ids = torch.ones((1, 512), dtype=torch.long)
    torch.onnx.export(Logits(model), (ids, ids), str(output / 'model.onnx'),
        input_names=['input_ids', 'attention_mask'], output_names=['logits'],
        dynamic_axes={'input_ids': {0: 'batch', 1: 'sequence'}, 'attention_mask': {0: 'batch', 1: 'sequence'},
                      'logits': {0: 'batch'}}, opset_version=17, dynamo=False)
    quantize_dynamic(str(output / 'model.onnx'), str(output / 'model_int8.onnx'),
        weight_type=QuantType.QInt8, op_types_to_quantize=['MatMul', 'Gemm'],
        per_channel=False, reduce_range=True, extra_options={'MatMulConstBOnly': True})
    for name in ['tokenizer.json', 'tokenizer_config.json', 'config.json']:
        shutil.copyfile(training / 'checkpoint' / name, output / name)
    manifest = {'kind': 'thura-mail-three-class-finetuned', 'version': VERSION, 'labels': LABELS,
                'training': metadata, 'selection': json.loads((training / 'selected.json').read_text()),
                'preprocessing_sha256': sha256(Path(__file__).with_name('common.py')), 'files': {}}
    for name in ['model.onnx', 'model_int8.onnx', 'tokenizer.json', 'tokenizer_config.json', 'config.json']:
        path = output / name
        path.chmod(0o644)
        manifest['files'][name] = {'sha256': sha256(path), 'bytes': path.stat().st_size}
    (output / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps({name: info['bytes'] for name, info in manifest['files'].items()}))


if __name__ == '__main__':
    main()
