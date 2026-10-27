#!/usr/bin/env python3
"""Real Postfix/Rspamd/Valkey authentication tests in a disposable, offline container."""
import argparse
import base64
import datetime
import gzip
from email.message import EmailMessage
from email import policy
from email.parser import BytesParser
from email.utils import format_datetime, make_msgid
import http.server
import json
import os
from pathlib import Path
import shlex
import smtplib
import socket
import socketserver
import ssl
import struct
import subprocess
import sys
import threading
import time


def eventually(call, timeout=25):
    deadline = time.monotonic() + timeout
    while True:
        try:
            return call()
        except (OSError, AssertionError, smtplib.SMTPException):
            if time.monotonic() >= deadline:
                raise
            time.sleep(.2)


def inside():
    sys.path.insert(0, '/opt/thura')
    import configure
    import smtp_tls
    import mail_auth
    received, reports, dns_queries = [], [], []
    keys = {}
    statuses = {'tenant-example.com': 'reject', 'valid-example.com': 'reject', 'spoof-example.com': 'reject',
                'quarantine-example.com': 'quarantine', 'monitor-example.com': 'none', 'unauthorized-example.com': 'reject'}

    class DNS(socketserver.BaseRequestHandler):
        def handle(self):
            data, stream = self.request
            pos, labels = 12, []
            while data[pos]:
                length = data[pos];pos += 1;labels.append(data[pos:pos+length].decode().lower());pos += length
            end = pos + 5
            name = '.'.join(labels);kind = struct.unpack('!H', data[pos+1:pos+3])[0]
            dns_queries.append(name)
            values = []
            code = 3
            if name == '_dmarc.temporary-example.com':
                code = 2
            elif kind == 16:
                if name == 'valid-example.com': values = ['v=spf1 ip4:8.8.8.8 -all']
                elif name in ('tenant-example.com', 'spoof-example.com', 'quarantine-example.com', 'monitor-example.com', 'unauthorized-example.com', 'temporary-example.com'):
                    values = ['v=spf1 -all']
                elif name.startswith('_dmarc.') and name[7:] in statuses:
                    domain = name[7:]
                    rua = 'not-authorized-example.com' if domain == 'unauthorized-example.com' else 'collector-example.com'
                    values = ['v=DMARC1; p=' + statuses[domain] + '; adkim=s; aspf=s; rua=mailto:reports@' + rua + ';']
                elif name.endswith('._report._dmarc.collector-example.com'):
                    values = ['v=DMARC1']
                elif name in keys: values = [keys[name]]
                if values: code = 0
            answers = b''
            for value in values:
                raw = value.encode();text = b''.join(bytes([len(raw[i:i+255])])+raw[i:i+255] for i in range(0,len(raw),255))
                answers += b'\xc0\x0c' + struct.pack('!HHIH',16,1,1,len(text)) + text
            response = data[:2]+struct.pack('!HHHHH',0x8180|code,1,len(values),0,0)+data[12:end]+answers
            stream.sendto(response,self.client_address)

    class Sink(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers['Content-Length']))
            if self.path == '/api/v1/setup/postfix/reports':
                reports.append((dict(self.headers),body));self.send_response(202)
            else:
                received.append((dict(self.headers),body));self.send_response(200)
            self.end_headers()
        def log_message(self,*_args): pass

    Path('/etc/resolv.conf').write_text('nameserver 127.0.0.1\n')
    dns = socketserver.UDPServer(('127.0.0.1',53),DNS)
    sink = http.server.ThreadingHTTPServer(('127.0.0.1',3000),Sink)
    for server in (dns,sink):threading.Thread(target=server.serve_forever,daemon=True).start()
    Path('/etc/rspamd/local.d/options.inc').write_text('dns {nameserver=["127.0.0.1:53"]; retransmits=1; timeout=1s;} task_timeout=15s;')
    Path('/etc/rspamd/local.d/worker-controller.inc').write_text('bind_socket="127.0.0.1:11334"; password="PUBLIC-fixture-password-1234567890"; enable_password="PUBLIC-fixture-password-1234567890";')
    Path('/etc/rspamd/local.d/redis.conf').write_text('servers="127.0.0.1:6379";')
    # Deterministic DNS tests should not wait on unrelated MX/rDNS/greylisting rules.
    Path('/etc/rspamd/local.d/logging.inc').write_text('level=\"debug\";debug_modules=[\"dkim_signing\",\"lua_maps\"];')
    Path('/etc/rspamd/local.d/greylist.conf').write_text('enabled=false;')
    Path('/etc/rspamd/local.d/settings.conf').write_text('fixture {priority=100; apply {symbols_disabled=["GREYLIST_CHECK","HFILTER_CHECK","MX_INVALID","MX_MISSING","ONCE_RECEIVED","MISSING_XM_UA"];}}')
    children=[]
    logs=open('/tmp/thura-auth-fixture.log','w')
    try:
        children.append(subprocess.Popen(['valkey-server','--bind','127.0.0.1','--save','','--appendonly','no'],stdout=logs,stderr=logs))
        rspamd = subprocess.Popen(['rspamd','-f','-u','_rspamd','-g','_rspamd'],stdout=logs,stderr=logs);children.append(rspamd)
        Path('/tmp/thura-mail-processes.json').write_text(json.dumps({'rspamd':rspamd.pid}))
        domains = ['tenant-example.com','valid-example.com']
        records = mail_auth.configure(domains,'mail.tenant-example.com','tenant-example.com')
        keys.update({r['name']:r['value'] for r in records})
        config={'managed':True,'baseUrl':'http://127.0.0.1:3000','reportDomain':'tenant-example.com',
                'mailboxes':{'inbox@tenant-example.com':{'id':'a0000000-0000-4000-8000-000000000010','secret':'PUBLIC-inbound-fixture-secret-at-least-32'}}}
        configure.configure(config,'mail.tenant-example.com','2527',managed=True)
        # Only inside this disconnected fixture: simulate the real peer IP with XCLIENT.
        subprocess.run(['postconf','-e','smtpd_authorized_xclient_hosts=127.0.0.1'],check=True)
        postfix=subprocess.Popen(['postfix','start-fg'],stdout=logs,stderr=logs);children.append(postfix)
        def ready():
            with smtplib.SMTP('127.0.0.1',2527,timeout=3) as s:
                s.ehlo('sender-example.com');assert not s.has_extn('starttls')
        eventually(ready)
        print('PASS missing certificate: STARTTLS is not advertised',flush=True)
        tls=Path('/run/secrets');tls.mkdir(exist_ok=True)
        subprocess.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','2','-subj','/CN=mail.tenant-example.com',
                        '-addext','subjectAltName=DNS:mail.tenant-example.com,IP:127.0.0.1',
                        '-keyout',str(tls/'smtp-key.pem'),'-out',str(tls/'smtp-cert.pem')],check=True,stdout=logs,stderr=logs)
        (tls/'smtp-key.pem').chmod(0o600)
        configure.configure(config,'mail.tenant-example.com','2527',managed=True)
        subprocess.run(['postfix','reload'],check=True,stdout=logs,stderr=logs)
        def handshake():
            with smtplib.SMTP('127.0.0.1',2527,timeout=3) as s:
                s.ehlo('sender-example.com');assert s.has_extn('starttls');s.starttls(context=ssl.create_default_context(cafile=str(tls/'smtp-cert.pem')))
                assert s.sock.version() in ('TLSv1.2','TLSv1.3')
        eventually(handshake)
        print('PASS installed certificate: trusted STARTTLS handshake succeeds',flush=True)
        deployed=Path('/var/lib/lidza-public-tls');deployed.mkdir(exist_ok=True)
        generation=deployed/'generation-one';generation.mkdir()
        bundle=generation/'bundle.pem';bundle.write_bytes((tls/'smtp-cert.pem').read_bytes()+(tls/'smtp-key.pem').read_bytes());bundle.chmod(0o400)
        (deployed/'current').symlink_to(generation,target_is_directory=True)
        os.environ['DEPLOY_PUBLIC_TLS_BUNDLE_FILE']=str(deployed/'current/bundle.pem')
        os.environ['DEPLOY_PUBLIC_TLS_HOSTNAME']='mail.tenant-example.com'
        configure.configure(config,'mail.tenant-example.com','2527',managed=True)
        subprocess.run(['postfix','reload'],check=True,stdout=logs,stderr=logs)
        eventually(handshake)
        actual=subprocess.check_output(['postconf','-h','smtpd_tls_cert_file'],text=True).strip()
        assert actual==str(bundle),(actual,str(bundle))
        print('PASS deployed service-scoped immutable bundle enables real STARTTLS',flush=True)
        def message(domain, fake=False):
            m=EmailMessage();m['From']='sender@'+domain;m['To']='inbox@tenant-example.com';m['Subject']='Ordinary project planning update'
            m['Date']=format_datetime(datetime.datetime.now(datetime.timezone.utc));m['Message-ID']=make_msgid(domain=domain)
            if fake:m['Authentication-Results']='mail.tenant-example.com; dmarc=pass header.from='+domain+'; spf=pass; dkim=pass'
            m.set_content('Here are the ordinary project planning notes for our next team meeting. Please review the schedule when you have time. Thank you for your help.')
            return m.as_bytes(policy=policy.SMTP)
        def submit(raw,sender,external=True):
            with smtplib.SMTP('127.0.0.1',2527 if external else 2525,timeout=25) as s:
                s.ehlo('mail.sender-example.com')
                if external:
                    code,_=s.docmd('XCLIENT','ADDR=8.8.8.8 NAME=mail.sender-example.com HELO=mail.sender-example.com')
                    assert code==220,code;s.ehlo('mail.sender-example.com')
                assert s.mail(sender)[0]==250
                assert s.rcpt('inbox@tenant-example.com')[0]==250
                return s.data(raw)
        def accepted(domain,sender=None):
            code,text=submit(message(domain),sender or 'sender@'+domain)
            assert code==250,(domain,code,text)
        eventually(lambda: accepted('valid-example.com'))
        print('PASS aligned SPF permits legitimate unsigned email',flush=True)
        for name,fake,sender in [('spoof-example.com',False,'sender@spoof-example.com'),('spoof-example.com',True,'sender@spoof-example.com'),('spoof-example.com',False,'sender@valid-example.com')]:
            code,text=submit(message(name,fake),sender)
            assert 500<=code<600,(name,code,text)
        print('PASS spoof, forged Authentication-Results and unaligned SPF rejected before queueing',flush=True)
        code,text=submit(message('quarantine-example.com'),'sender@quarantine-example.com');assert code==250,(code,text)
        code,text=submit(message('monitor-example.com'),'sender@monitor-example.com');assert code==250,(code,text)
        code,text=submit(message('temporary-example.com'),'sender@temporary-example.com');assert 400<=code<500,(code,text)
        print('PASS quarantine/monitor policies accepted for review; DNS outage defers',flush=True)
        start=len(received)
        accepted_private=submit(message('tenant-example.com'),'sender@tenant-example.com',False);assert accepted_private[0]==250,accepted_private
        def signed():
            candidates=[raw for _,raw in received[start:] if b'DKIM-Signature:' in raw]
            assert candidates
            return candidates[0]
        raw=eventually(signed)
        signature=BytesParser(policy=policy.default).parsebytes(raw)['DKIM-Signature'];assert 'd=tenant-example.com' in signature
        code,text=submit(raw,'sender@spoof-example.com');assert code==250,(code,text)
        print('PASS private sending DKIM-signed; aligned DKIM survives SPF failure on incoming mail',flush=True)
        corrupted=raw.replace(b'ordinary project planning notes',b'altered project planning notes')
        assert corrupted!=raw
        code,text=submit(corrupted,'sender@spoof-example.com');assert code>=500,(code,text)
        code,text=submit(message('monitor-example.com'),'');assert code==250,(code,text)
        print('PASS altered DKIM content rejected; null envelope sender handled',flush=True)
        def evidence():
            assert received
            headers,raw=received[0]
            encoded=headers.get('X-Thura-Smtp-Envelope') or headers.get('X-Thura-SMTP-Envelope');assert encoded
            frame=json.loads(base64.urlsafe_b64decode(encoded+'='*(-len(encoded)%4)))
            assert frame['ip']=='8.8.8.8',frame
        eventually(evidence)
        print('PASS original connection metadata preserved by signed relay',flush=True)
        # Unauthorized third-party rua destinations must not receive reports.
        code,text=submit(message('unauthorized-example.com'),'sender@unauthorized-example.com');assert code>=500,(code,text)
        os.environ['THURA_POSTFIX_TOKEN']='PUBLIC-report-fixture-token-at-least-32-characters'
        children.append(subprocess.Popen([sys.executable,'/opt/thura/reports.py'],stdout=logs,stderr=logs))
        def bridge_ready():
            with smtplib.SMTP('127.0.0.1',2526,timeout=2) as s:assert s.noop()[0]==250
        eventually(bridge_ready)
        day=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%d')
        result=subprocess.run(['rspamadm','dmarc_report',day],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=40)
        print('Report runner:',result.stdout.decode()[-4000:],flush=True)
        assert result.returncode==0
        assert reports,'No aggregate reports submitted'
        for headers,body in reports:
            assert json.loads(headers['X-Thura-Report-Recipients'])==['reports@collector-example.com'],headers
            assert headers['Authorization']=='Bearer '+os.environ['THURA_POSTFIX_TOKEN']
            parsed=BytesParser(policy=policy.default).parsebytes(body)
            assert parsed['Subject'].lower().startswith('report domain:')
            files=list(parsed.iter_attachments());assert len(files)==1
            xml=gzip.decompress(files[0].get_payload(decode=True))
            assert b'<feedback' in xml and b'8.8.8.8' in xml
            assert b'ordinary project planning notes' not in xml
        count=len(reports)
        subprocess.run(['rspamadm','dmarc_report',day],check=True,stdout=logs,stderr=logs,timeout=40)
        assert len(reports)==count,'Repeat report run sent duplicates'
        print('PASS aggregate reports use private outbox bridge, enforce rua authorization, omit original mail and do not repeat',flush=True)
        rspamd.terminate();rspamd.wait(timeout=10)
        refused=False
        try:
            with smtplib.SMTP('127.0.0.1',2527,timeout=10) as s:
                s.sendmail('sender@valid-example.com',['inbox@tenant-example.com'],message('valid-example.com'))
        except smtplib.SMTPResponseException as error:
            assert 400<=error.smtp_code<500,error.smtp_code;refused=True
        assert refused,'Filter outage accepted unchecked public mail'
        print('PASS unavailable authentication filter defers instead of accepting unchecked mail',flush=True)
        print('PASS offline authentication fixture complete',flush=True)
    except Exception:
        logs.flush();log=Path('/tmp/thura-auth-fixture.log').read_text();print('\n'.join(x for x in log.splitlines() if any(y in x.lower() for y in ('dkim_signing','lua_dkim','signing failure','permission','cannot load map')))[-18000:],flush=True);subprocess.run(['rspamadm','configdump','dkim_signing']);print('Received messages',len(received),flush=True);raise
    finally:
        for child in reversed(children):
            child.terminate()
        for child in reversed(children):
            try:child.wait(timeout=10)
            except subprocess.TimeoutExpired:child.kill();child.wait()
        dns.shutdown();sink.shutdown()


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--inside',action='store_true');parser.add_argument('--image',default='thura-postfix:test');parser.add_argument('--remote')
    args=parser.parse_args()
    if args.inside:return inside()
    root=Path(__file__).resolve().parents[1]
    files={}
    for name in ['configure.py','relay.py','smtp_tls.py','mail_auth.py','reports.py']:
        files['/opt/thura/'+name]=base64.b64encode((root/'deploy/postfix'/name).read_bytes()).decode()
    for p in (root/'deploy/postfix/rspamd').iterdir():
        if p.is_file():files['/etc/rspamd/local.d/'+p.name]=base64.b64encode(p.read_bytes()).decode()
    entry='import base64,pathlib,sys\n'
    entry+='files='+repr(files)+'\n'
    entry+='for p,b in files.items():pathlib.Path(p).write_bytes(base64.b64decode(b))\n'
    entry+="sys.argv=['mail-auth-fixture.py','--inside']\n"
    entry+='exec(compile('+repr(Path(__file__).read_text())+",'mail-auth-fixture.py','exec'))\n"
    command=['docker','run','--rm','--name','thura-mail-auth-fixture','--network','none','--memory','512m','--cpus','1','-i','--entrypoint','python3',args.image,'-']
    if args.remote:command=['ssh','-o','BatchMode=yes',args.remote,'sudo -n '+shlex.join(command)]
    subprocess.run(command,input=entry.encode(),check=True)


if __name__=='__main__':main()
