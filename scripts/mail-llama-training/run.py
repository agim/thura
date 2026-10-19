"""Time local MLX training and retain failures and native process memory."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import resource
import subprocess
import sys
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    config, output = Path(args.config), Path(args.output)
    import yaml
    settings = yaml.safe_load(config.read_text())
    base = json.loads(Path(__file__).with_name('base.json').read_text())
    for name, expected in base['files'].items():
        path = Path(settings['model']) / name
        if hashlib.sha256(path.read_bytes()).hexdigest() != expected['sha256']:
            raise ValueError('checkpoint differs from frozen base')
    data = Path(settings['data'])
    manifest = json.loads((data / 'manifest.json').read_text())
    for partition, name in [('train', 'train'), ('validation', 'valid')]:
        if hashlib.sha256((data / (name + '.jsonl')).read_bytes()).hexdigest() != manifest['splits'][partition]['sha256']:
            raise ValueError('training partition differs from frozen preparation')
    if not settings.get('mask_prompt') or not settings.get('grad_checkpoint'):
        raise ValueError('completion masking and bounded memory are required')
    started = time.monotonic()
    environment = dict(os.environ, HF_HUB_OFFLINE='1', TRANSFORMERS_OFFLINE='1', PYTHONUNBUFFERED='1')
    with output.with_suffix('.log').open('w') as log:
        bootstrap = ('import json, mlx.core as mx; from mlx_lm.lora import main; '
                     'main(); print("THURA_MLX_MEMORY=" + json.dumps({"peak_metal_bytes": mx.get_peak_memory()}))')
        result = subprocess.run([sys.executable, '-c', bootstrap, '--config', str(config)],
                                stdout=log, stderr=subprocess.STDOUT, env=environment)
    report = {'exit_code': result.returncode, 'wall_seconds': time.monotonic() - started,
              'max_child_rss_bytes': resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss,
              'config_sha256': hashlib.sha256(config.read_bytes()).hexdigest(),
              'log_sha256': hashlib.sha256(output.with_suffix('.log').read_bytes()).hexdigest(),
              'platform': sys.platform, 'production_enabled': False}
    report['base_revision'] = base['revision']
    report['data_manifest_sha256'] = hashlib.sha256((data / 'manifest.json').read_bytes()).hexdigest()
    report['steps'] = settings['iters']
    for line in output.with_suffix('.log').read_text().splitlines():
        if line.startswith('THURA_MLX_MEMORY='):
            report.update(json.loads(line.split('=', 1)[1]))
    output.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
    raise SystemExit(result.returncode)


if __name__ == '__main__':
    main()
