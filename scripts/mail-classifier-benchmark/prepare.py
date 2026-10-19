"""Download pinned public research artifacts. Never fetch private mail or execute model code."""
import argparse
import hashlib
import json
from pathlib import Path
import urllib.request


def download(url, target, size, digest):
    if target.exists() and target.stat().st_size == size and sha256(target) == digest:
        return
    temporary = target.with_suffix(target.suffix + '.partial')
    try:
        with urllib.request.urlopen(url, timeout=60) as response, temporary.open('wb') as output:
            total = 0
            while chunk := response.read(1024 * 1024):
                total += len(chunk)
                if total > size:
                    raise ValueError('artifact exceeds pinned size')
                output.write(chunk)
        if total != size or sha256(temporary) != digest:
            raise ValueError('artifact size/hash mismatch')
        temporary.chmod(0o644)
        temporary.replace(target)
    finally:
        temporary.unlink(missing_ok=True)


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--models', required=True)
    args = parser.parse_args()
    directory = Path(args.models)
    directory.mkdir(parents=True, exist_ok=True)
    manifest = json.loads(Path(__file__).with_name('artifacts.json').read_text())
    for name, item in manifest['files'].items():
        url = f"https://huggingface.co/{manifest['repository']}/resolve/{manifest['revision']}/{item['remote']}"
        download(url, directory / name, item['bytes'], item['sha256'])
        print(name, item['sha256'])
    (directory / 'artifacts.json').write_text(json.dumps(manifest, indent=2) + '\n')
    (directory / 'artifacts.json').chmod(0o644)


if __name__ == '__main__':
    main()
