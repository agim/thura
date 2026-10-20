"""Supervise the compact mail stack without dropping antivirus or learning."""
import json
import os
from pathlib import Path
import pwd
import signal
import socket
import subprocess
import sys
import time
import supervise


def as_user(name):
    account = pwd.getpwnam(name)
    return dict(user=account.pw_uid, group=account.pw_gid, extra_groups=[])


def ping_clamd():
    with socket.create_connection(('127.0.0.1', 3310), timeout=3) as stream:
        stream.sendall(b'zPING\0')
        if stream.recv(64) != b'PONG\0':
            raise RuntimeError('Antivirus is not ready')


def health():
    for pid in json.loads(Path('/tmp/thura-compact-mail.json').read_text()).values():
        os.kill(pid, 0)
    supervise.health()
    subprocess.run(['valkey-cli', 'ping'], check=True, stdout=subprocess.DEVNULL, timeout=3)
    ping_clamd()


def main():
    # Deploy creates empty named volumes without copying image directory
    # ownership. Existing Valkey volumes can also use another image's UID.
    # Preserve state while assigning each tree to its service account; never
    # follow links out of a persistent volume.
    for name, user in [('/var/lib/thura-valkey', 'valkey'), ('/var/lib/rspamd', '_rspamd')]:
        root = Path(name)
        root.mkdir(mode=0o700, exist_ok=True)
        account = pwd.getpwnam(user)
        for directory, _children, files in os.walk(root, followlinks=False):
            os.chown(directory, account.pw_uid, account.pw_gid, follow_symlinks=False)
            for filename in files:
                os.chown(Path(directory) / filename, account.pw_uid, account.pw_gid, follow_symlinks=False)
    for directory in ['/var/lib/clamav', '/run/clamav']:
        Path(directory).mkdir(exist_ok=True)
        account = pwd.getpwnam('clamav')
        os.chown(directory, account.pw_uid, account.pw_gid)
    stopping = False
    children = []
    def stop(_signal, _frame):
        nonlocal stopping
        stopping = True
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        children.append(subprocess.Popen(['valkey-server', '/etc/valkey/thura.conf'], **as_user('valkey')))
        # Do not start clamd with missing signatures. Freshclam itself is bounded
        # by the container limit; failures restart and retain required mail scans.
        if not any(Path('/var/lib/clamav').glob('daily.*')):
            subprocess.run(['freshclam', '--config-file=/etc/clamav/freshclam.conf'], check=True, timeout=300, **as_user('clamav'))
        children.append(subprocess.Popen(['freshclam', '--config-file=/etc/clamav/freshclam.conf', '--daemon', '--foreground=true'], **as_user('clamav')))
        children.append(subprocess.Popen(['clamd', '--config-file=/etc/clamav/clamd.conf']))
        children.append(subprocess.Popen([sys.executable, '/opt/thura/supervise.py']))
        Path('/tmp/thura-compact-mail.json').write_text(json.dumps({str(index): child.pid for index, child in enumerate(children)}))
        while not stopping:
            if any(child.poll() is not None for child in children):
                raise RuntimeError('A compact mail service exited')
            time.sleep(.25)
    finally:
        for child in children:
            if child.poll() is None:
                child.terminate()
        for child in children:
            try:
                child.wait(timeout=35)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)


if __name__ == '__main__':
    try:
        health() if '--health' in sys.argv else main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        print('Compact mail services unavailable; required scans remain held', file=sys.stderr)
        sys.exit(1)
