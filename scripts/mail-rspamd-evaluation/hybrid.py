"""Frozen offline Rspamd -> setup-model experiment. Never activates live intake."""
import hashlib
import http.client
import json
import math
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

from client import classify, impossible


def sha(data):
    return hashlib.sha256(data).hexdigest()


def route(local, review):
    """Local findings cannot be downgraded; every unflagged message needs review."""
    if local in ('spam', 'phishing'):
        return local
    if local != 'legitimate':
        raise ValueError('incomplete local scan')
    if review.get('error') or review.get('category') not in ('legitimate', 'spam', 'phishing'):
        raise ValueError('incomplete or uncertain model review')
    return review['category']


def scan(data):
    connection = http.client.HTTPConnection('127.0.0.1', 11333, timeout=8)
    try:
        connection.request('POST', '/checkv2', body=data, headers={'Content-Type': 'message/rfc822'})
        response = connection.getresponse()
        body = response.read(512*1024+1)
        if response.status != 200 or len(body) > 512*1024:
            raise ValueError('failed or oversized local scan')
        reply = json.loads(body)
        local, _, symbols = classify(reply)
        return local, {'score': reply['score'], 'action': reply['action'], 'phishing_symbols': symbols,
                       'symbols': {k: v.get('score') for k,v in reply.get('symbols',{}).items()}}
    finally:
        connection.close()


def cgroup():
    return {key: Path('/sys/fs/cgroup', key).read_text().strip() for key in
            ('memory.max','memory.peak','memory.swap.max','memory.events','cpu.max','cpu.stat')}


def require_limits(group):
    if group['memory.max'] != str(2*1024**3) or group['memory.swap.max'] != '0' or group['cpu.max'] != '200000 100000':
        raise ValueError('evaluation resource limits changed')


def read_json(url):
    with urllib.request.urlopen(url, timeout=5) as response:
        raw = response.read(512*1024+1)
    if len(raw) > 512*1024:
        raise ValueError('oversized model metadata')
    return json.loads(raw)


def require_model(contract, config):
    tags = read_json(config['base_url']+'/api/tags')
    models = [m for m in tags['models'] if m['name'] == config['model']]
    if len(models) != 1 or models[0]['digest'] != contract['model_digest']:
        raise ValueError('setup-configured model identity changed')


def main():
    contract = json.load(sys.stdin)
    root = Path('/evaluation')
    for path, digest in contract['files'].items():
        if sha((root/path).read_bytes()) != digest:
            raise ValueError('frozen evaluation artifact changed: '+path)
    config = json.loads((root/'model.json').read_text())
    if config['provider'] != 'ollama' or config['model'] != contract['model'] or not config['enabled']:
        raise ValueError('setup snapshot is not enabled or changed')
    require_limits(cgroup())
    require_model(contract, config)
    description = json.loads(subprocess.check_output([str(root/'hybrid-llm'),'--describe'],timeout=5))
    if description['prompt_sha256'] != contract['prompt_sha256']:
        raise ValueError('screening prompt changed')
    evidence = json.loads((root/'evidence.json').read_text())
    results, reasons = [], []
    start = time.monotonic()
    calls = 0
    for row in contract['cases']:
        result = {key: row[key] for key in ('id','label','source')}
        started = time.monotonic()
        result['llm_called'] = False
        try:
            raw = (Path('/corpus')/(row['id']+'.eml')).read_bytes()
            if len(raw) > 2*1024**2 or sha(raw) != row['message_sha256']:
                raise ValueError('MIME corpus changed')
            local, details = scan(raw)
            result.update(local_prediction=local, local=details)
            review = {}
            if local == 'legitimate':
                if calls >= contract['maximum_model_calls']:
                    raise ValueError('evaluation model budget exhausted')
                payload = json.dumps({'text': evidence[row['id']]},ensure_ascii=False,separators=(',',':')).encode()
                if len(payload)>24000 or sha(payload) != row['evidence_sha256']:
                    raise ValueError('bounded complete model evidence changed')
                calls += 1
                result['llm_called'] = True
                print('reviewing',len(results)+1,'model call',calls,file=sys.stderr,flush=True)
                reply = subprocess.run([str(root/'hybrid-llm'),'--config',str(root/'model.json')],input=payload,
                                       stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=config['timeout_seconds']+5,check=True)
                if len(reply.stdout)>8192:
                    raise ValueError('oversized model result')
                review = json.loads(reply.stdout)
                # Keep generated explanations private; publish only measured outcomes.
                result['model'] = review
            result['prediction'] = route(local,review)
        except Exception as error:
            result.update(prediction='error',error=type(error).__name__)
        result['millis'] = (time.monotonic()-started)*1000
        results.append(result)
        print(json.dumps({'type':'case','result':result}),flush=True)
        print('completed',len(results),'/',len(contract['cases']),file=sys.stderr,flush=True)
        reasons = impossible(results,contract['planned_counts'])
        if len(results)>=30 and reasons:
            break
        if time.monotonic()-start>contract['maximum_run_seconds']:
            reasons.append('evaluation time budget exhausted')
            break
    # No model download, training, provider change, production write, or threshold tuning.
    require_model(contract,config)
    model_memory = read_json(config['base_url']+'/api/ps')
    print(json.dumps({'type':'summary','status':'rejected' if reasons else 'complete_pending_assessment',
                      'reasons':reasons,'complete':len(results)==len(contract['cases']),
                      'planned_counts':contract['planned_counts'],'observed_count':len(results),
                      'model_calls':calls,'elapsed_seconds':time.monotonic()-start,'cgroup':cgroup(),
                      'external_ollama_loaded_models':model_memory,'production_enabled':False}),flush=True)


if __name__ == '__main__':
    main()
