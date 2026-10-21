#!/usr/bin/env python3
"""Drive an isolated, real Deploy agent. Never contacts the production console.

Keep credentials and agent state under .lidza; stdout contains status only.
Requires a configuration-contract-capable agent and installation master key.
"""
import argparse
import ast
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shlex
import smtplib
from email.message import EmailMessage
from email.utils import formatdate,make_msgid
import subprocess
import time
from client import Client,eventually
from wizard import Wizard
from core import configuration

# Acceptance runs on its own bounded VM; never operate on the demo Docker context.
os.environ.pop('DOCKER_CONTEXT',None)
os.environ['DOCKER_HOST']='unix://'+str(Path.home()/'.colima/thura-deploy-acceptance/docker.sock')

ROOT=Path(__file__).resolve().parents[2]
STATE=ROOT/'.lidza/deploy-stack-acceptance'
os.environ['DOCKER_CONFIG']=str(STATE/'docker-config')
APP_ID='thura-stack-acceptance'
DOMAIN='thura-stack.localhost'
EMAIL='operator@outside.example'
PASSWORD='PUBLIC-stack acceptance river maple 8429'


def docker(*args):
    return subprocess.run(['docker',*args],check=True,capture_output=True,text=True,timeout=90).stdout.strip()


def credentials():
    return json.loads((STATE/'agent.json').read_text())


def agent():
    config=credentials()
    return Client('http://'+config['listen'],token=config['api_key'])


def current():
    return next(item for item in agent().call('/v1/apps') if item['id']==APP_ID)


def deployment_done(identifier):
    row=next(item for item in agent().call('/v1/deployments') if item['id']==identifier)
    if row['status']=='failed':
        # Bounded agent output remains private; never echo credential-bearing logs.
        (STATE/'failed-deployment.json').write_text(json.dumps(row,indent=2))
        raise RuntimeError('Deployment failed; inspect private failed-deployment.json')
    return row if row['status'] in ('live','setup_required') else None


def wait_deploy(identifier):
    deadline=time.monotonic()+1800
    while time.monotonic()<deadline:
        result=deployment_done(identifier)
        if result:return result
        time.sleep(1)
    raise RuntimeError('Deployment did not finish within acceptance budget')


def initialize(binary):
    STATE.mkdir(mode=0o700,parents=True,exist_ok=True)
    if (STATE/'agent.json').exists():raise RuntimeError('Owned agent already configured; resume it explicitly')
    config={'listen':'127.0.0.1:19090','proxy_listen':'127.0.0.1:19881','tls_listen':'127.0.0.1:19443','data_dir':str(STATE/'data'),'api_key':secrets.token_hex(32)}
    (STATE/'agent.json').write_text(json.dumps(config));(STATE/'agent.json').chmod(0o600)
    start(binary)


def start(binary):
    binary=str(Path(binary).resolve())
    # Run the real Linux agent inside the same 4-GiB budget as the services.
    # Its private state is in the owned directory, shared with the host for QA.
    pidfile=shlex.quote(str(STATE/'agent-linux.pid'))
    guard='if [ -f '+pidfile+' ]; then pid=$(cat '+pidfile+'); if kill -0 "$pid" 2>/dev/null; then exit 9; fi; fi; '
    command=guard+'umask 077; nohup env -u DOCKER_HOST -u DOCKER_CONTEXT -u DOCKER_CONFIG '+shlex.quote(binary)+' -config '+shlex.quote(str(STATE/'agent.json'))+' > '+shlex.quote(str(STATE/'agent-linux.log'))+' 2>&1 < /dev/null & echo $! > '+shlex.quote(str(STATE/'agent-linux.pid'))
    subprocess.run(['colima','-p','thura-deploy-acceptance','ssh','--','sudo','sh','-c',command],check=True,capture_output=True,text=True,timeout=30)
    eventually(lambda:Client('http://127.0.0.1:19090').call('/health'),'isolated Linux agent')


def stop():
    pidfile=shlex.quote(str(STATE/'agent-linux.pid'))
    expected=shlex.quote(str(STATE/'agent-linux'))
    command='pid=$(cat '+pidfile+'); test "$(readlink /proc/$pid/exe)" = '+expected+' || exit 9; kill -TERM "$pid"; for n in $(seq 1 60); do kill -0 "$pid" 2>/dev/null || exit 0; sleep 1; done; exit 10'
    subprocess.run(['colima','-p','thura-deploy-acceptance','ssh','--','sudo','sh','-c',command],check=True,capture_output=True,text=True,timeout=70)


def provision(branch):
    key_path=STATE/'application-master.key'
    if not key_path.exists():key_path.write_text(secrets.token_hex(32));key_path.chmod(0o600)
    key=key_path.read_text().strip()
    environment={'LIDZA_MASTER_KEY':key,'APP_URL':'https://'+DOMAIN,'EMBED_PROVIDER':'none','MAIL_AI_SCREENING':'false','AUTH_COOKIE_SECURE':'false','JOBS_WORKERS':'1','THURA_RTC_NODE_IP':'127.0.0.1','THURA_TENANT_SMTP_HOSTS':'127.0.0.1'}
    agent().call('/v1/apps/'+APP_ID,{'repository':'agim/thura','branch':branch,'domain':DOMAIN,'runtime_profile':'minimum','env':environment},method='PUT')
    settings=agent().call('/v1/apps/'+APP_ID+'/settings')
    if not settings.get('stack_profiles_supported'):raise RuntimeError('Agent does not advertise stack support')
    agent().call('/v1/apps/'+APP_ID+'/database',{'mode':'local','backup':{'hours':0,'keep':1,'offsite':False}})
    eventually(lambda:any(row['ready'] and any(a['app_id']==APP_ID for a in row['attachments']) for row in agent().call('/v1/databases')),'managed PostgreSQL',seconds=180)


def deploy():
    token=subprocess.run(['gh','auth','token'],check=True,capture_output=True,text=True).stdout.strip()
    row=agent().call('/v1/apps/'+APP_ID+'/deploy',{'github_token':token})
    print(json.dumps({'deployment':row['id'],'status':'queued'}),flush=True)
    wait_deploy(row['id'])


def service(role):
    stack=current()['current']['stack']
    return next(s['container'] for s in stack['containers'] if s['service']['role']==role)


def start_sink():
    # Reuse the existing SMTP test implementation; no additional live container.
    spec=importlib.util.spec_from_file_location('postfix_fixture',ROOT/'scripts/postfix-compose-fixture.py')
    fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)
    sink=STATE/'sink.py';sink.write_text(fixture.SINK.replace("root=pathlib.Path('/fixture')","root=pathlib.Path('/var/lib/thura-valkey/acceptance-fixture');root.mkdir(mode=0o700,exist_ok=True)"))
    mail=service('mail')
    docker('exec','--user','0',mail,'mkdir','-p','/fixture')
    docker('cp',str(sink),mail+':/fixture/sink.py')
    docker('exec','-d','--user','0',mail,'python3','/fixture/sink.py')


def bootstrap(core_path):
    core=configuration(core_path)
    start_sink()
    application=Client('http://127.0.0.1:19881',host=DOMAIN)
    token=agent().call('/v1/apps/'+APP_ID+'/environment/THURA_SETUP_TOKEN')['value']
    def restart():
        row=agent().call('/v1/apps/'+APP_ID+'/reload',{},method='POST')
        wait_deploy(row['id'])
        start_sink()
    steps={
        'server':{'APP_URL':'https://'+DOMAIN,'THURA_WORKSPACE_NAME':'Stack acceptance','THURA_OPERATOR_EMAIL':EMAIL,'THURA_TIMEZONE':'UTC','THURA_BACKUP_PLAN':'Isolated acceptance fixture; retained only until verification completes'},
        'mail':{'THURA_MAIL_INBOUND':'postfix','THURA_MAIL_OUTBOUND':'provider','THURA_POSTFIX_HOSTNAME':'mail.team.example','THURA_MAIL_ADDRESS':'inbox@team.example','MAIL_FROM':'notifications@team.example','MAIL_PROVIDER':'smtp','MAIL_REGION':'us','MAIL_SMTP_HOST':'127.0.0.1','MAIL_SMTP_PORT':'2526','MAIL_SMTP_SECURITY':'none','MAIL_AI_SCREENING':'false'},
        'storage':{'STORAGE_PROVIDER':'local','STORAGE_DIR':'/var/lib/lidza-storage','STORAGE_ENDPOINT':'https://s3.amazonaws.com','STORAGE_REGION':'us-east-1'},
        'policy':{'DRIVE_QUOTA_BYTES':'1073741824','THURA_MAX_MEMBERS':'100','THURA_ADMIN_INVITES':'true','THURA_ALLOW_SHARES':'true','THURA_ALLOW_ANONYMOUS_SHARES':'false','MAIL_MAX_RECIPIENTS':'50'},
        'ai':{},
        'integrations':{},
    }
    for step,values in core.items():steps[step].update(values)
    Wizard(application,restart).bootstrap(token,EMAIL,PASSWORD,steps)


def mail_flow():
    app=Client('http://127.0.0.1:19881',host=DOMAIN)
    app.call('/api/v1/auth/login',{'email':EMAIL,'password':PASSWORD})
    workspace=next(row['id'] for row in app.call('/api/v1/workspaces')['items'] if row['name']=='Stack acceptance')
    mailbox=next(row['id'] for row in app.call('/api/v1/workspaces/'+workspace+'/mailboxes')['items'] if row['address']=='inbox@team.example')
    prefix='/api/v1/workspaces/'+workspace+'/mailboxes/'+mailbox+'/messages'
    message=EmailMessage();message['From']='colleague@outside.example';message['To']='inbox@team.example';message['Subject']='Managed stack SMTP acceptance '+secrets.token_hex(4);message['Date']=formatdate(localtime=False);message['Message-ID']=make_msgid(domain='outside.example')
    message.set_content('The project planning meeting covers delivery dates, revised budgets, progress updates, shared responsibilities, approved milestones and next steps for our team. Please review the agenda before our scheduled discussion tomorrow. We will compare the equipment inventory, network requirements, proposed design, purchase order, expected arrival, installation schedule, customer feedback, technical specifications and support process. The construction team has confirmed availability for the onsite visit.')
    def send():
        with smtplib.SMTP('127.0.0.1',25,timeout=15,local_hostname='outside.example') as smtp:smtp.send_message(message)
        return True
    eventually(send,'managed SMTP ingress')
    def find(folder):return next((row for row in app.call(prefix+'?folder='+folder)['items'] if row['subject']==message['Subject']),None)
    item=eventually(lambda:find('inbox') or find('spam'),'actual incoming message persistence')
    path=prefix+'/'+item['id']
    app.call(path+'/review',{'action':'report-spam'})
    eventually(lambda:find('spam'),'Report spam moves to Junk')
    eventually(lambda:app.call(path)['screening']['feedbackState']=='learned','durable spam learning')
    app.call(path+'/review',{'action':'not-spam'})
    eventually(lambda:find('inbox'),'Not spam restores Inbox')
    eventually(lambda:app.call(path)['screening']['feedbackState']=='learned','durable ham correction')
    with smtplib.SMTP('127.0.0.1',25,timeout=15,local_hostname='outside.example') as smtp:
        smtp.ehlo();smtp.mail('colleague@outside.example');code,_=smtp.rcpt('unowned@external.example')
        assert code>=500,'public port must refuse arbitrary relay'
    captures=json.loads(docker('exec',service('mail'),'python3','-c','import json; from pathlib import Path; print(json.dumps(Path("/var/lib/thura-valkey/acceptance-fixture/outgoing.jsonl").read_text().splitlines()))'))
    assert len(captures)==1 and 'Thura server setup: email check' in captures[0]
    report={'smtp_received':True,'public_relay_refused':True,'outgoing_provider_probe_captured':True,'junk_spam_learning':True,'not_spam_restored_and_learned':True,'ai_calls':0}
    (STATE/'mail-flow.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))


def smoke():
    tree=ast.parse((ROOT/'scripts/compact-smoke.py').read_text())
    source=next(ast.literal_eval(node.value) for node in tree.body if isinstance(node,ast.Assign) and any(isinstance(target,ast.Name) and target.id=='mail_test' for target in node.targets))
    result=subprocess.run(['docker','exec','-i',service('mail'),'python3','-'],input=source,check=True,capture_output=True,text=True,timeout=240)
    checks=json.loads(result.stdout)
    resources={}
    names={role:service(role) for role in ['app','mail','office']}
    names['postgres']='lidza-db-'+APP_ID
    for role,name in names.items():
        row=json.loads(docker('inspect',name))[0]
        record={'limit_bytes':row['HostConfig']['Memory'],'cpu_limit':row['HostConfig']['NanoCpus']/1e9,'oom_killed':row['State']['OOMKilled'],'restarts':row['RestartCount']}
        if role=='office':
            # The vendor image is distroless. Read this exact owned container's
            # cgroup from the Linux host without copying a debug tool into it.
            command=['colima','-p','thura-deploy-acceptance','ssh','--','sudo','cat']
            group=subprocess.check_output(command+['/proc/'+str(row['State']['Pid'])+'/cgroup'],text=True).strip().split('::',1)[1]
            assert group in ('/docker/'+row['Id'],'/system.slice/docker-'+row['Id']+'.scope')
            raw=subprocess.check_output(command+['/sys/fs/cgroup'+group+'/memory.peak','/sys/fs/cgroup'+group+'/memory.events'],text=True).splitlines()
        else:
            raw=docker('exec',name,'cat','/sys/fs/cgroup/memory.peak','/sys/fs/cgroup/memory.events').splitlines()
        record['peak_bytes']=int(raw[0]);record['memory_events']=dict(line.split() for line in raw[1:])
        assert record['memory_events']['oom_kill']=='0'
        assert not record['oom_killed'],record
        resources[role]=record
    assert sum(row['limit_bytes'] for row in resources.values())==3584*1048576
    assert sum(row['cpu_limit'] for row in resources.values())==2
    checks['managed_postgres_query']=docker('exec',names['postgres'],'psql','-U','app','-d','app','-Atc','select 1')=='1'
    assert checks['managed_postgres_query']
    report={'checks':checks,'resources':resources,'scope':'Actual minimum-profile Deploy stack on a 2-CPU,4-GiB VM; synthetic functional acceptance, not throughput or mail-detection accuracy.'}
    (STATE/'smoke.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2))


def main():
    os.umask(0o077)
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['initialize','start','stop','provision','deploy','status','bootstrap','smoke','mail-flow'])
    parser.add_argument('--agent',default=str(STATE/'agent-linux'))
    parser.add_argument('--branch',default='main')
    parser.add_argument('--core-config',help='Optional private overrides for the default free local core services')
    args=parser.parse_args()
    if args.action=='initialize':initialize(args.agent)
    elif args.action=='start':start(args.agent)
    elif args.action=='stop':stop()
    elif args.action=='provision':provision(args.branch)
    elif args.action=='deploy':deploy()
    elif args.action=='bootstrap':bootstrap(args.core_config)
    elif args.action=='smoke':smoke()
    elif args.action=='mail-flow':mail_flow()
    else:
        app=current()
        print(json.dumps({'app':app['id'],'profile':app.get('runtime_profile'),'current':app.get('current',{}).get('id'),'deployments':[{'id':d['id'],'status':d['status']} for d in agent().call('/v1/deployments') if d['app_id']==APP_ID]}));return
    print(json.dumps({'action':args.action,'status':'ok'}))


if __name__=='__main__':main()
