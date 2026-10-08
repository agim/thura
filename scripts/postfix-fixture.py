#!/usr/bin/env python3
"""Prove local SMTP ingress, retries, rejection and official-pack outbound mail.

Requires the synthetic *_test database, existing e2e seed, built .lidza/e2e-app,
and thura-postfix:test image. No internet recipients or real credentials.
"""
import argparse
from email import policy
from email.parser import BytesParser
import http.cookiejar
import http.server
import importlib.util
import json
import os
from pathlib import Path
import smtplib
import socketserver
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

spec = importlib.util.spec_from_file_location('backup', Path(__file__).with_name('backup.py'))
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)
SECRET = 'thura-postfix-public-synthetic-secret-184729'
WORKSPACE = 'a0000000-0000-4000-8000-000000000001'
MAILBOX = 'a0000000-0000-4000-8000-000000000050'
ADDRESS = 'inbox@tenant.example'
BASE = 'http://127.0.0.1:3002'


def eventually(check, description, timeout=60):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = check()
            if value:
                return value
        except (OSError, ValueError, urllib.error.URLError, smtplib.SMTPException):
            pass
        time.sleep(0.5)
    raise RuntimeError(description + ' did not complete')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('.lidza/postfix-test'))
    parser.add_argument('--pg-container')
    args = parser.parse_args()
    if not urllib.parse.urlsplit(os.environ.get('DATABASE_URL', '')).path.endswith('_test'):
        raise ValueError('Only a synthetic *_test database is permitted')
    args.root = args.root.resolve()
    args.root.mkdir(parents=True, exist_ok=True)
    runroot = args.root / ('run-' + uuid.uuid4().hex)
    runroot.mkdir(mode=0o700)
    config = args.root / 'relay.json'
    config.write_text(json.dumps({'baseUrl': 'http://127.0.0.1:3003', 'mailboxes': {ADDRESS: {'id': MAILBOX, 'secret': SECRET}}}))
    config.chmod(0o600)
    # Distinct from all browser-test mailboxes; synthetic metadata only.
    backup.pg(args, ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-c',
        f"INSERT INTO mailbox(id,workspace_id,name,address,config_prefix) VALUES ('{MAILBOX}','{WORKSPACE}','Postfix fixture','{ADDRESS}','POSTFIX_') ON CONFLICT(id) DO UPDATE SET config_prefix='POSTFIX_';"])
    received = []

    class Sink(socketserver.StreamRequestHandler):
        def handle(self):
            self.connection.settimeout(20)
            self.wfile.write(b'220 local synthetic SMTP sink\r\n')
            while line := self.rfile.readline(4096):
                command = line.split(b' ', 1)[0].strip().upper()
                if command in (b'EHLO', b'HELO'):
                    self.wfile.write(b'250-local fixture\r\n250 SIZE 12582912\r\n')
                elif command in (b'MAIL', b'RCPT', b'RSET', b'NOOP'):
                    self.wfile.write(b'250 OK\r\n')
                elif command == b'DATA':
                    self.wfile.write(b'354 End with dot\r\n')
                    body = bytearray()
                    while chunk := self.rfile.readline(12582913):
                        if chunk == b'.\r\n':
                            break
                        body += chunk[1:] if chunk.startswith(b'..') else chunk
                        if len(body) > 12582912:
                            raise RuntimeError('Synthetic sink size limit exceeded')
                    received.append(bytes(body))
                    self.wfile.write(b'250 captured synthetic fixture\r\n')
                elif command == b'QUIT':
                    self.wfile.write(b'221 Bye\r\n')
                    return
                else:
                    self.wfile.write(b'500 Unsupported fixture command\r\n')

    sink = socketserver.ThreadingTCPServer(('127.0.0.1', 2526), Sink)
    sink.daemon_threads = True
    thread = threading.Thread(target=sink.serve_forever, daemon=True)
    thread.start()
    lost_ack = [True]
    forwarded = []

    class Bridge(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            raw = self.rfile.read(int(self.headers['Content-Length']))
            headers = {name: self.headers[name] for name in ('X-Thura-Timestamp', 'X-Thura-Delivery', 'X-Thura-Signature')}
            request = urllib.request.Request(BASE + self.path, data=raw, headers=headers)
            try:
                with urllib.request.urlopen(request, timeout=10) as response:
                    body = response.read()
                    forwarded.append(headers['X-Thura-Delivery'])
                    # Persisted in Thura, but the MTA loses its first successful
                    # acknowledgment. Retry must not create a second item.
                    if lost_ack[0]:
                        lost_ack[0] = False
                        self.send_response(503)
                    else:
                        self.send_response(response.status)
                    self.end_headers()
                    self.wfile.write(body)
            except urllib.error.URLError:
                self.send_response(503)
                self.end_headers()

        def log_message(self, *_args):
            pass

    bridge = http.server.ThreadingHTTPServer(('127.0.0.1', 3003), Bridge)
    bridge_thread = threading.Thread(target=bridge.serve_forever, daemon=True)
    bridge_thread.start()
    app = None
    log = (args.root / 'app.log').open('ab')
    cookie = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie))

    def api(path, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(BASE + path, data=data, headers={'Content-Type': 'application/json'})
        with client.open(request, timeout=10) as response:
            return json.load(response)

    prefix = f'/api/v1/workspaces/{WORKSPACE}/mailboxes/{MAILBOX}/messages'

    def start_app():
        environment = os.environ.copy()
        environment.update(LIDZA_MODE='test', LIDZA_ADDR='127.0.0.1:3002',
            POSTFIX_MAIL_PROVIDER='smtp', POSTFIX_MAIL_SMTP_HOST='127.0.0.1',
            POSTFIX_MAIL_SMTP_PORT='2525', POSTFIX_MAIL_SMTP_SECURITY='none',
            POSTFIX_MAIL_INBOUND_SECRET=SECRET)
        process = subprocess.Popen(['.lidza/e2e-app'], env=environment, stdout=log, stderr=log)
        eventually(lambda: urllib.request.urlopen(BASE + '/readyz', timeout=2).status == 200, 'test app readiness')
        if process.poll() is not None:
            raise RuntimeError('Owned test app exited (port may already be in use)')
        api('/api/v1/auth/login', {'email': 'owner-e2e@example.com', 'password': 'thura fixture maple lantern 4829'})
        return process

    def smtp(raw, recipient=ADDRESS, source='127.0.0.2'):
        with smtplib.SMTP('127.0.0.1', 2525, timeout=10, source_address=(source, 0)) as connection:
            return connection.sendmail('sender@example.net', [recipient], raw)

    def queue():
        output = subprocess.check_output(['docker', 'exec', 'thura-postfix-test', 'postqueue', '-j'])
        return [json.loads(line) for line in output.splitlines()]

    def stop_app(process):
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()

    subprocess.run(['docker', 'rm', '-f', 'thura-postfix-test'], capture_output=True)
    try:
        command = ['docker', 'run', '-d', '--name', 'thura-postfix-test', '--network', 'host',
                   '-e', 'MAIL_HOSTNAME=mail.tenant.example', '-e', 'POSTFIX_RELAYHOST=[127.0.0.1]:2526',
                   '-v', f'{config}:/run/secrets/thura-relay.json:ro',
                   '-v', f'{runroot / "queue"}:/var/spool/postfix', 'thura-postfix:test']
        subprocess.run(command, check=True, capture_output=True)

        def ready():
            with smtplib.SMTP('127.0.0.1', 2525, timeout=2) as connection:
                return connection.noop()[0] == 250

        eventually(ready, 'Postfix SMTP readiness')
        app = start_app()
        subject = 'Postfix retry fixture ' + uuid.uuid4().hex
        raw = f'From: sender@example.net\r\nTo: {ADDRESS}\r\nSubject: {subject}\r\nMessage-ID: <{uuid.uuid4().hex}@example.net>\r\n\r\nPrivate local incoming bytes\r\n'.encode()
        smtp(raw)
        inbound = eventually(lambda: next((item for item in api(prefix)['items'] if item['subject'] == subject), None), 'SMTP-to-Thura delivery')
        assert inbound['textBody'].strip() == 'Private local incoming bytes'
        eventually(lambda: any(row['queue_name'] == 'deferred' for row in queue()), 'lost acknowledgment stays queued')
        subprocess.run(['docker', 'exec', 'thura-postfix-test', 'postqueue', '-f'], check=True, capture_output=True)
        eventually(lambda: not queue(), 'duplicate queue acknowledgment')
        assert len(forwarded) >= 2 and forwarded[0] == forwarded[1]
        assert len([item for item in api(prefix)['items'] if item['subject'] == subject]) == 1
        for recipient in ('unknown@tenant.example', 'sink@outside.example'):
            try:
                smtp(raw, recipient)
                raise AssertionError('Unknown recipient/open relay was accepted')
            except smtplib.SMTPRecipientsRefused as error:
                assert error.recipients[recipient][0] >= 500
        # Accepted SMTP mail must remain queued while the app is stopped.
        stop_app(app)
        app = None
        pending_subject = 'Postfix recovery fixture ' + uuid.uuid4().hex
        pending = raw.replace(subject.encode(), pending_subject.encode()).replace(b'Private local incoming bytes', b'Recovered queued bytes')
        smtp(pending)
        eventually(lambda: any(row['queue_name'] == 'deferred' for row in queue()), 'durable deferred delivery')
        subprocess.run(['docker', 'restart', 'thura-postfix-test'], check=True, capture_output=True)
        eventually(ready, 'Postfix restart readiness')
        assert queue(), 'Postfix restart lost accepted queued mail'
        app = start_app()
        subprocess.run(['docker', 'exec', 'thura-postfix-test', 'postqueue', '-f'], check=True, capture_output=True)
        restored = eventually(lambda: next((item for item in api(prefix)['items'] if item['subject'] == pending_subject), None), 'delivery after app recovery')
        assert restored['textBody'].strip() == 'Recovered queued bytes'
        eventually(lambda: not queue(), 'recovered queue acknowledgment')
        # Actual Līdza SMTP provider -> Postfix -> isolated SMTP sink.
        outgoing = 'Official SMTP fixture ' + uuid.uuid4().hex
        draft = api(prefix, {'to': 'sink@outside.example', 'subject': outgoing, 'text': 'Actual official pack SMTP bytes'})
        api(prefix + '/' + draft['id'] + '/send', {})
        def sent():
            item = api(prefix + '/' + draft['id'])['item']
            return item if item['status'] == 'sent' else None

        item = eventually(sent, 'official-pack SMTP acceptance', 90)
        assert item['status'] == 'sent'
        message = eventually(lambda: next((BytesParser(policy=policy.default).parsebytes(body) for body in received if outgoing.encode() in body), None), 'Postfix outbound transport')
        assert message['From'] == ADDRESS
        assert message['To'] == 'sink@outside.example'
        assert 'Actual official pack SMTP bytes' in message.get_body(preferencelist=('plain',)).get_content()
        version = subprocess.check_output(['docker', 'exec', 'thura-postfix-test', 'postconf', '-h', 'mail_version'], text=True).strip()
        print(json.dumps({'status': 'ok', 'postfixVersion': version, 'inbound': True, 'deduplicated': True, 'rejectUnknown': True, 'rejectOpenRelay': True, 'deferredRecovery': True, 'mtaRestartRecovery': True, 'officialPackOutbound': True}))
    finally:
        if app:
            stop_app(app)
        subprocess.run(['docker', 'stop', 'thura-postfix-test'], capture_output=True)
        sink.shutdown()
        sink.server_close()
        thread.join()
        bridge.shutdown()
        bridge.server_close()
        bridge_thread.join()
        log.close()
        backup.pg(args, ['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-c',
            f"DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id='{MAILBOX}'); DELETE FROM mail_item WHERE mailbox_id='{MAILBOX}'; DELETE FROM mailbox WHERE id='{MAILBOX}';"])


if __name__ == '__main__':
    main()
