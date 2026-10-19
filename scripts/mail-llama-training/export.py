"""Fuse local QLoRA adapters, convert with pinned llama.cpp and quantize for CPU."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

IMAGE = 'ghcr.io/ggml-org/llama.cpp:server@sha256:143926008cbbbab8fae8fd87809875af3e42d3d676c30c11c55c0f3f683a5a07'
REVISION = '31288c9c66e59377f7788d920678a565bb89dd37'


def sha(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    parser.add_argument('--converter-python', required=True)
    parser.add_argument('--base')
    parser.add_argument('--converter-source')
    parser.add_argument('--expected-steps', type=int, default=600)
    args = parser.parse_args()
    root = Path(args.root).resolve()
    training = json.loads((root / 'training.json').read_text())
    if training['exit_code'] != 0 or training['steps'] != args.expected_steps or training.get('objective_code_changed_during_run'):
        raise ValueError('the predeclared training schedule did not complete')
    data_manifest = json.loads((root / 'data/manifest.json').read_text())
    if sha(root / 'data/manifest.json') != training['data_manifest_sha256']:
        raise ValueError('training data provenance changed')
    source = Path(args.converter_source).resolve() if args.converter_source else root / 'llama.cpp'
    base = Path(args.base).resolve() if args.base else root / 'base'
    if subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip() != REVISION:
        raise ValueError('llama.cpp converter revision mismatch')
    subprocess.run([sys.executable, '-m', 'mlx_lm', 'fuse', '--model', str(base),
                    '--adapter-path', str(root / 'trained-adapters'), '--save-path', str(root / 'fused'),
                    '--dequantize'], check=True)
    # Official converter preserves the Llama 3 rope scaling and chat template.
    # MLX's legacy GGUF writer does not cover every current metadata field.
    subprocess.run([args.converter_python, str(source / 'convert_hf_to_gguf.py'), str(root / 'fused'),
                    '--outtype', 'f16', '--outfile', str(root / 'trained-f16.gguf'),
                    '--model-name', 'Llama-Thura-mail-1B'], check=True)
    subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--cpus', '2', '--memory', '4g',
                    '--memory-swap', '4g', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
                    '--mount', f'type=bind,src={root},dst=/models', '--entrypoint', '/app/llama', IMAGE,
                    'quantize', '/models/trained-f16.gguf', '/models/trained-q4.gguf', 'Q4_K_M', '2'], check=True)
    provenance = {'kind': 'locally-fine-tuned', 'name': 'Llama-Thura-mail-1B',
                  'model_sha256': sha(root / 'trained-q4.gguf'),
                  'model_bytes': (root / 'trained-q4.gguf').stat().st_size,
                  'adapter_sha256': sha(root / 'trained-adapters/adapters.safetensors'),
                  'data_manifest_sha256': sha(root / 'data/manifest.json'),
                  'acceptance_sha256': sha(root / 'acceptance-before-test.json'),
                  'training_report': training,
                  'base_manifest': json.loads(Path(__file__).with_name('base.json').read_text()),
                  'converter_revision': REVISION, 'runtime_image': IMAGE,
                  'quantization': 'QLoRA trained on MLX 4bit; dequantized fusion then Q4_K_M',
                  'license': 'Llama-3.2 Community License and Acceptable Use Policy',
                  'production_enabled': False}
    provenance['objective'] = training.get('objective', 'json-token')
    if provenance['objective'] == 'class-decision':
        prompt_path = Path(__file__).with_name('decisionbench') / 'prompt.json'
        if sha(prompt_path) != data_manifest['prompt_sha256']:
            raise ValueError('serving prompt differs from training contract')
        provenance['prompt_sha256'] = sha(prompt_path)
        provenance['decision_contract'] = json.loads(prompt_path.read_text())
    (root / 'trained-provenance.json').write_text(json.dumps(provenance, indent=2) + '\n')
    print(json.dumps({'model_bytes': provenance['model_bytes'], 'model_sha256': provenance['model_sha256']}))


if __name__ == '__main__':
    main()
