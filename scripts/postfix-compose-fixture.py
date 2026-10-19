#!/usr/bin/env python3
"""Exercise the real Compose wizard, Postfix ingress and both outgoing paths.

All recipients use reserved .example domains and every outbound SMTP message is
captured by a private synthetic sink. No external email/provider calls occur.
Requires built Compose images; run the documented compose build first.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import smtplib
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
PROJECT = 'thura-postfix-acceptance'
PASSWORD = 'PUBLIC-compose fixture river maple 8429'
EMAIL = 'operator@outside.example'
ADDRESS = 'inbox@team.example'

SINK = '''import json, pathlib, socketserver
root=pathlib.Path('/fixture')
class SMTP(socketserver.StreamRequestHandler):
 def handle(self):
  self.wfile.write(b'220 private fixture sink\\r\\n')
  while line:=self.rfile.readline(4096):
   command=line.split(b' ',1)[0].strip().upper()
   if command==b'EHLO': self.wfile.write(b'250-fixture\\r\\n250 SIZE 16777216\\r\\n')
   elif command in (b'MAIL',b'RCPT',b'RSET',b'NOOP'): self.wfile.write(b'250 OK\\r\\n')
   elif command==b'DATA':
    self.wfile.write(b'354 end with dot\\r\\n'); body=bytearray()
    while chunk:=self.rfile.readline(16777217):
     if chunk==b'.\\r\\n': break
     body+=chunk
     if len(body)>16777216: return
    with (root/'outgoing.jsonl').open('a') as stream: stream.write(json.dumps(body.decode('utf-8','replace'))+'\\n')
    self.wfile.write(b'250 captured\\r\\n')
   elif command==b'QUIT': self.wfile.write(b'221 bye\\r\\n'); return
   else: self.wfile.write(b'250 OK\\r\\n')
class Server(socketserver.ThreadingTCPServer): allow_reuse_address=True
Server(('127.0.0.1',2526),SMTP).serve_forever()
'''


def eventually(fn, label, seconds=90):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError, urllib.error.URLError, smtplib.SMTPException):
            pass
        time.sleep(0.5)
    raise AssertionError(label + ' timed out')


def check_retired_queue(compose):
    """A removed mailbox must remain locally deferred, including after restart."""
    assert compose[-1] == PROJECT, 'only the isolated fixture project is allowed'
    retired = 'retired@retired.example'

    def command(*args, check=True):
        return subprocess.run(compose+list(args), cwd=ROOT, check=check,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30).stdout.decode()

    command('exec','-T','postgres','psql','-U','thura','-d','thura','-Atqc',
            "INSERT INTO mailbox(workspace_id,name,address,config_prefix) SELECT id,'Retired queue fixture','retired@retired.example','' FROM workspace WHERE name='Compose acceptance'")

    def mapped():
        with smtplib.SMTP('127.0.0.1',2528,timeout=5,local_hostname='fixture.example') as connection:
            connection.ehlo()
            connection.mail('sender@outside.example')
            return connection.rcpt(retired)[0] == 250

    eventually(mapped, 'new mailbox mapping')
    command('pause','thura')
    try:
        with smtplib.SMTP('127.0.0.1',2528,timeout=5,local_hostname='fixture.example') as connection:
            connection.sendmail('sender@outside.example',[retired],
                                'Subject: Retain removed mailbox queue\r\n\r\nSynthetic queued bytes\r\n')
        command('exec','-T','postgres','psql','-U','thura','-d','thura','-Atqc',
                "DELETE FROM mailbox WHERE address='retired@retired.example'")
    finally:
        command('unpause','thura')
    eventually(lambda: not command('exec','-T','postfix','postmap','-q',retired,
                                   'hash:/etc/postfix/relay_recipients',check=False).strip(), 'recipient removal')
    command('restart','postfix')
    eventually(lambda: command('exec','-T','postfix','postmap','-q',retired,
                              'hash:/etc/postfix/thura_transport',check=False).strip() == 'thura:', 'persistent local transport')
    def refused():
        with smtplib.SMTP('127.0.0.1',2528,timeout=5,local_hostname='fixture.example') as connection:
            connection.ehlo()
            connection.mail('sender@outside.example')
            return connection.rcpt(retired)[0] >= 500
    eventually(refused, 'removed address refusal after restart')
    command('exec','-T','postfix','postqueue','-f')
    eventually(lambda: retired in command('exec','-T','postfix','postqueue','-j'), 'retained queue entry')
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compose', default='docker compose')
    parser.add_argument('--keep', action='store_true', help='keep the isolated stack for visual review')
    args = parser.parse_args()
    root = ROOT / '.lidza/postfix-compose-fixture'
    root.mkdir(mode=0o700, exist_ok=True)
    env = root / 'compose.env'
    if not env.exists():
        env.write_text('\n'.join(name+'='+secrets.token_hex(32) for name in ('POSTGRES_PASSWORD','AUTH_SECRET','LIDZA_MASTER_KEY','THURA_SETUP_TOKEN','THURA_POSTFIX_TOKEN'))+'\nTHURA_HTTP_PORT=8082\nTHURA_SMTP_BIND=127.0.0.1\nTHURA_SMTP_PORT=2528\nSMTP_TLS_DIR=./private/smtp\n')
        env.chmod(0o600)
    values = dict(line.split('=',1) for line in env.read_text().splitlines() if '=' in line)
    (root / 'sink.py').write_text(SINK)
    (root / 'override.json').write_text(json.dumps({'services': {
        'postfix': {'environment': {'POSTFIX_RELAYHOST': '[127.0.0.1]:2526'}},
        'sink': {'image': PROJECT+'-postfix', 'network_mode': 'service:thura', 'depends_on': {'thura': {'condition': 'service_started', 'restart': True}},
                 'entrypoint': ['python3','/fixture/sink.py'], 'volumes': [str(root)+':/fixture']}
    }}))
    compose = shlex.split(args.compose) + ['--env-file',str(env),'-f',str(ROOT/'deploy/compose.yaml'),'-f',str(root/'override.json'),'-p',PROJECT]

    def run(*cmd):
        subprocess.run(compose+list(cmd),cwd=ROOT,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)

    base = 'http://127.0.0.1:8082'
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, data=None, form=False):
        encoded = urllib.parse.urlencode(data).encode() if form else (json.dumps(data).encode() if data is not None else None)
        req = urllib.request.Request(base+path, data=encoded, headers={'Content-Type': 'application/x-www-form-urlencoded' if form else 'application/json', 'Sec-Fetch-Site': 'same-origin'})
        with client.open(req,timeout=20) as response:
            body=response.read().decode()
            if form:
                assert 'error=' not in response.url, 'wizard action refused'
                return body
            return json.loads(body)

    # Query admin pages without POST (form=True is only the response decoder).
    def page(path):
        with client.open(base+path,timeout=15) as response:
            return response.read().decode()

    def rev():
        return re.search(r'name="revision" value="([0-9]+)"',page('/admin/setup?step=mail')).group(1)

    defaults = {
        'server': {'APP_URL':'https://compose.example','THURA_WORKSPACE_NAME':'Compose acceptance','THURA_OPERATOR_EMAIL':EMAIL,'THURA_TIMEZONE':'UTC','THURA_BACKUP_PLAN':'Synthetic isolated fixture; destroyed after tests'},
        'mail': {'THURA_MAIL_INBOUND':'postfix','THURA_MAIL_OUTBOUND':'postfix','THURA_POSTFIX_HOSTNAME':'mail.team.example','THURA_MAIL_ADDRESS':ADDRESS,'MAIL_FROM':'notifications@team.example','MAIL_PROVIDER':'smtp','MAIL_REGION':'us','MAIL_SMTP_PORT':'587','MAIL_SMTP_SECURITY':'starttls'},
        'storage': {'STORAGE_PROVIDER':'local','STORAGE_DIR':'storage','STORAGE_ENDPOINT':'https://s3.amazonaws.com','STORAGE_REGION':'us-east-1'},
        'policy': {'DRIVE_QUOTA_BYTES':'1073741824','THURA_MAX_MEMBERS':'100','THURA_ADMIN_INVITES':'true','THURA_ALLOW_SHARES':'true','THURA_ALLOW_ANONYMOUS_SHARES':'false','MAIL_MAX_RECIPIENTS':'50'},
        'integrations': {'SMIP_ENABLED':'false'},
    }

    def save(step, changes=None):
        request('/admin/setup/save',dict(defaults[step], **(changes or {}), revision=rev(),step=step,back='?step='+step),form=True)

    def checks():
        review=page('/admin/setup?step=review')
        for kind,confirm in [('mail','send'),('storage','check')]:
            check_id=re.search('action="setup/check-'+kind+'".*?name="check_id" value="([^"]+)"',review).group(1)
            request('/admin/setup/check-'+kind,{'revision':rev(),'check_id':check_id,'confirm':confirm,'back':'?step=review'},form=True)
        assert 'Not ready to publish:' not in page('/admin/setup?step=review'), 'provider checks failed'

    def publish():
        request('/admin/setup/publish',{'revision':rev(),'confirm':'publish','back':'?step=review'},form=True)
        run('restart','thura')
        eventually(lambda: request('/api/v1/setup/status').get('active'), 'activation')

    def received(subject):
        workspaces=request('/api/v1/workspaces')['items']
        workspace=next(item['id'] for item in workspaces if item['name']=='Compose acceptance')
        mailboxes=request('/api/v1/workspaces/'+workspace+'/mailboxes')['items']
        mailbox=next(item['id'] for item in mailboxes if item['address']==ADDRESS)
        prefix='/api/v1/workspaces/'+workspace+'/mailboxes/'+mailbox+'/messages'
        result=request(prefix+'?folder=inbox')
        return any(item['subject']==subject for item in result['items'])

    def smtp(subject):
        with smtplib.SMTP('127.0.0.1',2528,timeout=5,local_hostname='fixture.example') as connection:
            connection.sendmail('sender@outside.example',[ADDRESS],f'From: sender@outside.example\r\nTo: {ADDRESS}\r\nSubject: {subject}\r\n\r\nActual Compose SMTP bytes\r\n')

    try:
        run('up','-d','--no-build')
        eventually(lambda: request('/api/v1/setup/status').get('open'),'bootstrap')
        request('/api/v1/setup/claim',{'token':values['THURA_SETUP_TOKEN'],'email':EMAIL,'name':'Compose operator','password':PASSWORD})
        request('/api/v1/auth/login',{'email':EMAIL,'password':PASSWORD})
        # Real wizard DOM checks use these same fields; forms are persisted here.
        assert 'Receive email through' in page('/admin/setup?step=mail')
        for step in defaults:
            save(step)
        checks()
        publish()
        eventually(lambda: smtp('Postfix both directions') or True,'SMTP readiness')
        eventually(lambda: received('Postfix both directions'),'Postfix to persisted inbox')
        # Public receiving port must not be an open relay, even for localhost.
        with smtplib.SMTP('127.0.0.1',2528,timeout=5,local_hostname='fixture.example') as connection:
            try:
                connection.sendmail('sender@outside.example',['victim@outside.example'],'Subject: must refuse\r\n\r\nrefuse')
                raise AssertionError('public SMTP port relayed arbitrary outgoing mail')
            except smtplib.SMTPRecipientsRefused:
                pass
        # Choose a separate outgoing SMTP provider; receiving remains Postfix.
        save('mail',{'THURA_MAIL_OUTBOUND':'provider','MAIL_SMTP_HOST':'127.0.0.1','MAIL_SMTP_PORT':'2526','MAIL_SMTP_SECURITY':'none'})
        checks()
        publish()
        eventually(lambda: smtp('Hybrid incoming') or True,'hybrid SMTP readiness')
        eventually(lambda: received('Hybrid incoming'),'hybrid persisted inbox')
        assert check_retired_queue(compose)
        captures=(root/'outgoing.jsonl').read_text().splitlines()
        assert len(captures)==2, 'two explicit outgoing checks must reach private sink'
        assert all('Thura server setup: email check' in json.loads(raw) for raw in captures)
        print(json.dumps({'status':'ok','composeBootstrap':True,'postfixSendingAndReceiving':True,'independentSMTPProvider':True,'hybridIncoming':True,'publicRelayRefused':True,'outgoingCaptured':len(captures),'removedMailboxQueueRetained':True}))
    finally:
        if not args.keep:
            run('down','-v')


if __name__ == '__main__':
    main()
