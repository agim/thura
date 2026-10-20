"""Run Postfix and Rspamd together; restart the mail container if either fails."""
import json
import os
from pathlib import Path
import pwd
import re
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request


def configure_secret(secret, path=Path('/tmp/controller-secret.inc')):
    if not re.fullmatch(r'[A-Za-z0-9_-]{32,256}',secret):
        raise ValueError('Configure a private Rspamd controller password of 32–256 safe characters')
    account=pwd.getpwnam('_rspamd')
    fd,temporary=tempfile.mkstemp(prefix='.thura-rspamd-',dir=path.parent)
    staged=Path(temporary)
    try:
        with os.fdopen(fd,'w') as stream:
            stream.write(f'password = "{secret}";\nenable_password = "{secret}";\n')
            os.fchown(stream.fileno(),account.pw_uid,account.pw_gid)
        staged.replace(path)
    finally:
        staged.unlink(missing_ok=True)


def health():
    status=json.loads(Path('/tmp/thura-mail-processes.json').read_text())
    for pid in status.values():
        os.kill(pid,0)
    request=urllib.request.Request('http://127.0.0.1:11334/ready',headers={'Password':os.environ['MAIL_RSPAMD_PASSWORD']})
    with urllib.request.urlopen(request,timeout=3) as response:
        if response.status!=200 or json.load(response).get('success') is not True:
            raise RuntimeError('Local spam filter is not ready')


def main():
    configure_secret(os.environ.get('MAIL_RSPAMD_PASSWORD',''))
    stopping=False
    children=[]

    def stop(_signum,_frame):
        nonlocal stopping
        stopping=True

    signal.signal(signal.SIGTERM,stop)
    signal.signal(signal.SIGINT,stop)
    try:
        children.append(subprocess.Popen(['rspamd','-f','-u','_rspamd','-g','_rspamd']))
        children.append(subprocess.Popen([sys.executable,'/opt/thura/configure.py']))
        Path('/tmp/thura-mail-processes.json').write_text(json.dumps({'rspamd':children[0].pid,'postfix_supervisor':children[1].pid}))
        while not stopping:
            if any(child.poll() is not None for child in children):
                raise RuntimeError('A mail service exited; restarting the combined container')
            time.sleep(.25)
    finally:
        for child in children:
            if child.poll() is None:child.terminate()
        for child in children:
            try:child.wait(timeout=25)
            except subprocess.TimeoutExpired:child.kill();child.wait(timeout=5)


if __name__=='__main__':
    try:
        if '--health' in sys.argv:health()
        else:main()
    except (OSError,ValueError,RuntimeError,subprocess.SubprocessError):
        print('Thura mail services are unavailable; see service logs',file=sys.stderr)
        sys.exit(1)
