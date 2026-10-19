"""Fetch the explicitly selected public MLX checkpoint at frozen hashes."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    manifest = json.loads(Path(__file__).with_name('base.json').read_text())
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    for name, expected in manifest['files'].items():
        target = output / name
        if not target.exists():
            url = f"https://huggingface.co/{manifest['repository']}/resolve/{manifest['revision']}/{name}"
            temporary = target.with_suffix(target.suffix + '.partial')
            with urllib.request.urlopen(url, timeout=120) as response, temporary.open('wb') as stream:
                while chunk := response.read(1024 * 1024):
                    stream.write(chunk)
            temporary.replace(target)
        if target.stat().st_size != expected['bytes'] or hashlib.sha256(target.read_bytes()).hexdigest() != expected['sha256']:
            raise ValueError('checkpoint hash mismatch: ' + name)
    print('Verified pinned Llama 3.2 1B MLX checkpoint; weights remain local.')


if __name__ == '__main__':
    main()
