"""Private Compose sidecar: poll activated mappings; retain the durable MTA queue.

No Docker socket, database password or outgoing provider credentials are needed.
The listener and HTTP control plane share Thura's loopback network namespace.
"""
import json
import os
import signal
import subprocess
import time
import urllib.request

import configure
import smtp_tls
import mail_auth
from relay import NoRedirect

CONFIG_URL = 'http://127.0.0.1:3000/api/v1/setup/postfix'
MAX_CONFIG = 5 * 1024 * 1024


def fetch(token):
    request = urllib.request.Request(CONFIG_URL, headers={'Authorization': 'Bearer ' + token})
    with urllib.request.build_opener(NoRedirect).open(request, timeout=5) as response:
        raw = response.read(MAX_CONFIG + 1)
        if len(raw) > MAX_CONFIG:
            raise ValueError('configuration too large')
        return json.loads(raw)


def validate(payload):
    if not isinstance(payload.get('enabled'), bool):
        raise ValueError('enabled must be boolean')
    config = payload['config']
    # The app cannot turn this privileged sidecar into an arbitrary HTTP client.
    if config.get('baseUrl') != 'http://127.0.0.1:3000':
        raise ValueError('relay origin must be Thura loopback')
    if payload['listen'] not in ('25', '127.0.0.1:2525'):
        raise ValueError('unexpected managed listener')
    if payload['listen'] == '25' and not config.get('mailboxes'):
        raise ValueError('receiving requires exact mailbox mappings')
    configure.settings(config, payload['hostname'], payload['listen'], allow_empty=True)
    return payload


def main():
    token = os.environ.get('THURA_POSTFIX_TOKEN', '')
    if len(token) < 32:
        raise ValueError('THURA_POSTFIX_TOKEN must have at least 32 characters')
    process = None
    previous = None
    previous_tls = None
    stopping = False

    def stop(_signum, _frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        while not stopping:
            if process is not None and process.poll() is not None:
                raise RuntimeError('Postfix exited; container restart required')
            try:
                payload = validate(fetch(token))
                try:
                    _, tls_digest = smtp_tls.configuration(payload['hostname'])
                except ValueError:
                    if process is not None and previous_tls != 'invalid-certificate':
                        for option in ('smtpd_tls_security_level=none','smtpd_tls_cert_file=','smtpd_tls_key_file=','smtpd_tls_chain_files='):
                            subprocess.run(['postconf','-e',option],check=True)
                        subprocess.run(['postfix','reload'],check=True)
                    previous_tls = 'invalid-certificate'
                    raise
                if payload != previous or tls_digest != previous_tls:
                    if not payload['enabled']:
                        if process is not None:
                            subprocess.run(['postfix', 'stop'], check=True)
                            process.wait(timeout=20)
                            process = None
                    else:
                        config = dict(payload['config'], managed=True)
                        configure.configure(config, payload['hostname'], payload['listen'], managed=True)
                        if process is None:
                            process = subprocess.Popen(['postfix', 'start-fg'])
                        else:
                            subprocess.run(['postfix', 'reload'], check=True)
                    previous = payload
                    previous_tls = tls_digest
                    print('Postfix configuration applied; revision ' + str(payload.get('revision', 0)), flush=True)
                if payload['enabled']:
                    mail_auth.reports(payload['config'].get('reportDomain', ''))
            except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
                # Never log response bytes, signing keys, bearer credentials or URLs.
                # A read failure preserves the last running configuration and queue.
                print('Postfix configuration unavailable; retaining queue and last configuration', flush=True)
            for _ in range(20):
                if stopping:
                    break
                time.sleep(0.1)
    finally:
        if process is not None and process.poll() is None:
            subprocess.run(['postfix', 'stop'], check=False)
            try:
                process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                process.terminate()
                process.wait(timeout=5)


if __name__ == '__main__':
    main()
