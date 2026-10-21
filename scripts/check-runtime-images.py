#!/usr/bin/env python3
"""Verify distributed immutable runtime pins support both deployment CPUs.

Read real registry indexes, not tags or a checked-in architecture claim. A child
manifest is rejected even when the local machine could run it under emulation.
"""
import json
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    executable = shutil.which('docker-buildx')
    command = [executable] if executable else ['docker', 'buildx']
    pins = set()
    for name in ['Dockerfile', 'deploy/compact/mail.Dockerfile', 'deploy/compact/office.Dockerfile']:
        pins.update(re.findall(r'^FROM ([^\s]+@sha256:[a-f0-9]{64})(?:\s|$)', (ROOT/name).read_text(), re.M))
    required = {'collabora/code', 'rspamd/rspamd', 'livekit/livekit-server'}
    if not required <= {pin.split('@')[0] for pin in pins}:
        raise RuntimeError('A required runtime image no longer has an immutable pin')
    for pin in sorted(pins):
        result = subprocess.run(command+['imagetools', 'inspect', '--raw', pin], check=True, capture_output=True, text=True, timeout=90)
        index = json.loads(result.stdout)
        platforms = {row.get('platform', {}).get('architecture') for row in index.get('manifests', []) if row.get('platform', {}).get('os') == 'linux'}
        if not {'amd64', 'arm64'} <= platforms:
            raise RuntimeError(f'{pin}: requires a multi-architecture index with linux/amd64 and linux/arm64; found {sorted(platforms)}')
        print(f'{pin}: linux/amd64 and linux/arm64 verified')


if __name__ == '__main__':
    main()
