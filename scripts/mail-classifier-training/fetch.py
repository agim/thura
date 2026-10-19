"""Download only pinned public inputs. A changed upstream file requires review."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile
import urllib.request


def fetch(url, target, info):
    if not url.startswith('https://'):
        raise ValueError('HTTPS inputs required')
    if target.exists():
        if target.stat().st_size == info['bytes'] and hashlib.sha256(target.read_bytes()).hexdigest() == info['sha256']:
            return
        raise ValueError('existing input differs from pinned artifact: ' + target.name)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=target.parent, delete=False) as output:
            temporary = Path(output.name)
            digest, total = hashlib.sha256(), 0
            with urllib.request.urlopen(url, timeout=60) as response:
                if not response.url.startswith('https://'):
                    raise ValueError('non-HTTPS redirect rejected')
                while chunk := response.read(1024 * 1024):
                    total += len(chunk)
                    if total > info['bytes']:
                        raise ValueError('input exceeds pinned size')
                    digest.update(chunk)
                    output.write(chunk)
        if total != info['bytes'] or digest.hexdigest() != info['sha256']:
            raise ValueError('input hash changed; review source/license before repinning: ' + target.name)
        temporary.chmod(0o600)
        temporary.replace(target)
        temporary = None
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    parser.add_argument('--antispam', action='store_true', help='fetch the separate pinned binary comparison instead')
    args = parser.parse_args()
    root = Path(args.root)
    here = Path(__file__).parent
    if args.antispam:
        candidate = json.loads((here / 'antispam.json').read_text())
        for name, info in candidate['files'].items():
            destination = root / 'antispam-spam' / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            url = f"https://huggingface.co/{candidate['repository']}/resolve/{candidate['revision']}/{name}"
            fetch(url, destination, info)
        print('Pinned binary comparison fetched; redistribution has not been cleared.')
        return
    for name, info in json.loads((here / 'sources.json').read_text()).items():
        destination = root / 'raw' / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        fetch(info['url'], destination, info)
    base = json.loads((here / 'base.json').read_text())
    for name, info in base['files'].items():
        destination = root / 'base' / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        url = f"https://huggingface.co/{base['repository']}/resolve/{base['revision']}/{name}"
        fetch(url, destination, info)
    print('Pinned inputs verified. Raw messages and model weights must remain outside git.')


if __name__ == '__main__':
    main()
