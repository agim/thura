#!/usr/bin/env python3
"""Synthetic smoke/resource checks against an isolated compact Compose project.

No external recipients, real user mail, model calls or paid feeds. Run browser
Office and media tests separately while these four services are constrained.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--project',default='thura-compact-acceptance')
parser.add_argument('--output',type=Path,default=Path('.lidza/compact-smoke.json'))
args=parser.parse_args()
if not re.fullmatch(r'thura-compact-[a-z0-9-]+',args.project):
    parser.error('Use an isolated thura-compact-* acceptance project')


def run(service,command,source=None):
    process=subprocess.run(['docker','exec',*(['-i'] if source else []),f'{args.project}-{service}-1',*command],input=source,text=True,capture_output=True,timeout=240)
    if process.returncode:
        raise RuntimeError(process.stderr.strip())
    return process.stdout


mail_test=r'''
import json,os,socket,struct,urllib.request,urllib.error
team='00000000-0000-4000-8000-000000000123'
body='The project planning meeting covers delivery dates, revised budgets, progress updates, shared responsibilities, approved milestones and next steps for our team. Please review the attached agenda before our scheduled discussion tomorrow. We will compare the equipment inventory, network requirements, proposed design, purchase order, expected arrival, installation schedule, customer feedback, technical specifications and support process. The construction team has confirmed availability for the onsite visit.'
raw=('From: colleague@example.test\r\nTo: reader@thura.test\r\nSubject: Project planning update\r\n\r\n'+body).encode()
def scan(data):
 with socket.create_connection(('127.0.0.1',3310),timeout=120) as s:
  s.sendall(b'zINSTREAM\0'+struct.pack('!I',len(data))+data+struct.pack('!I',0));return s.recv(2048).decode()
eicar=b'X5O!P%@AP[4'+bytes([92])+b'PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*'
assert 'OK' in scan(body.encode())
assert 'FOUND' in scan(eicar)
def learn(kind):
 req=urllib.request.Request('http://127.0.0.1:11334/learn'+kind,data=raw,headers={'Password':os.environ['MAIL_RSPAMD_PASSWORD'],'X-Thura-Workspace':team})
 try:
  with urllib.request.urlopen(req,timeout=20) as response:
   result=json.load(response)
   assert response.status in (200,208)
   if response.status==208: assert result.get('error','').endswith(' has been already learned as '+kind+', ignore it')
   else: assert result.get('success') is True
 except urllib.error.HTTPError as error: assert error.code==208
learn('ham');learn('ham');learn('spam');learn('ham')
req=urllib.request.Request('http://127.0.0.1:11333/checkv2',data=raw,headers={'X-Thura-Workspace':team})
with urllib.request.urlopen(req,timeout=20) as response:
 result=json.load(response);assert 'score' in result and not result.get('is_skipped')
with socket.create_connection(('127.0.0.1',3310),timeout=3) as s:
 s.sendall(b'zRELOAD\0');assert b'RELOADING' in s.recv(64)
assert 'FOUND' in scan(eicar)
print(json.dumps({'clean_av':'OK','eicar_av':'FOUND','rspamd_scan':True,'learning_reversal_and_replay':True,'clamav_reload':True}))
'''
run('thura',['python3','/opt/thura/app.py','--health'])
run('postfix',['python3','/opt/thura/compact.py','--health'])
checks=json.loads(run('postfix',['python3','-'],mail_test))
assert run('postgres',['psql','-U','thura','-d','thura','-Atc','select 1']).strip()=='1'
resources={}
for service in ['thura','postgres','postfix','collabora']:
    info=json.loads(subprocess.check_output(['docker','inspect',f'{args.project}-{service}-1'],text=True))[0]
    # Distroless Collabora has no shell; docker stats exposes its usage. The
    # other services expose cgroup peak and OOM counters directly.
    record={'limit_bytes':info['HostConfig']['Memory'],'cpu_limit':info['HostConfig']['NanoCpus']/1e9,'oom_killed':info['State']['OOMKilled'],'restarts':info['RestartCount']}
    assert not record['oom_killed'] and record['restarts']==0,record
    if service!='collabora':
        raw=run(service,['cat','/sys/fs/cgroup/memory.peak','/sys/fs/cgroup/memory.events']).splitlines()
        record['peak_bytes']=int(raw[0]);record['memory_events']=dict(line.split() for line in raw[1:])
        assert record['memory_events']['oom_kill']=='0'
    resources[service]=record
assert sum(r['limit_bytes'] for r in resources.values())==3584*1024*1024
assert sum(r['cpu_limit'] for r in resources.values())==2
report={'checks':checks,'resources':resources,'scope':'Synthetic functional smoke; not a capacity or detection-quality benchmark'}
args.output.parent.mkdir(parents=True,exist_ok=True)
args.output.write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
