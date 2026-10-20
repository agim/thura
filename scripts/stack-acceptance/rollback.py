"""Check retained stored bytes and learning across an owned real stack rollback."""
import json
from client import Client
from run import agent,current,docker,service,STATE,APP_ID,DOMAIN,EMAIL,PASSWORD


def snapshot():
    sql="SELECT json_build_object('versions',(SELECT json_agg(row_to_json(v) ORDER BY id) FROM (SELECT id,file_id,number,object_key,checksum FROM file_version ORDER BY id) v),'feedback',(SELECT json_agg(row_to_json(f) ORDER BY id) FROM (SELECT id,classification,revision,learned_revision,state FROM mail_filter_feedback ORDER BY id) f))"
    data=json.loads(docker('exec','lidza-db-'+APP_ID,'psql','-U','app','-d','app','-Atc',sql))
    source='import json,hashlib,os; from pathlib import Path; root=Path(os.environ["STORAGE_DIR"]); print(json.dumps({str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob("*") if p.is_file()},sort_keys=True))'
    data['stored_bytes']=json.loads(docker('exec',service('app'),'python3','-c',source))
    lua="local r={}; for _,k in ipairs(redis.call('KEYS','RSthura-team:*')) do local a=redis.call('HGETALL',k); local v={}; for i=1,#a,2 do v[a[i]]=a[i+1] end; r[k]=v end; return cjson.encode(r)"
    data['learning']=json.loads(docker('exec',service('mail'),'valkey-cli','--raw','EVAL',lua,'0'))
    data['bindings']=current()['database_bindings']
    return data


before=snapshot();release=current()['current']['id']
assert len(before['versions'])==4 and len(before['stored_bytes'])>=4
assert before['feedback'][0]['classification']=='ham' and before['feedback'][0]['state']=='learned'
agent().call('/v1/apps/'+APP_ID+'/rollback',{},method='POST')
after=snapshot()
(STATE/'rollback-before.json').write_text(json.dumps(before,sort_keys=True,indent=2))
(STATE/'rollback-after.json').write_text(json.dumps(after,sort_keys=True,indent=2))
assert before==after,'Rollback changed durable state: '+','.join(k for k in before if before[k]!=after[k])
assert current()['current']['id']!=release
assert not current()['current']['stack'].get('bootstrap',False)
app=Client('http://127.0.0.1:19881',host=DOMAIN);app.call('/api/v1/auth/login',{'email':EMAIL,'password':PASSWORD})
assert app.call('/api/v1/setup/status')['active']
report={'rollback_preserved_version_metadata':True,'rollback_preserved_actual_stored_bytes':True,'rollback_preserved_learned_feedback':True,'same_managed_database_attachment':True,'bootstrap_cleared':True,'versions':len(before['versions'])}
(STATE/'rollback.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
