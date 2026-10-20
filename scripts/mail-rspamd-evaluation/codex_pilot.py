"""Blinded Codex batch probe, not a production mail-provider adapter.

Run in an empty private directory with shell, apps, delegation and web search
disabled. Only deidentified text/opaque ids enter the prompt; never gold labels.
The process uses an existing Codex sign-in, never extracted account credentials.
"""
import hashlib
import json
from pathlib import Path
import subprocess
import time


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def blind_items(local_cases, manifest, evidence):
    review = [{'original_id':r['id'], 'kind':'remaining', 'text':evidence[r['id']]}
              for r in local_cases if r['local_prediction']=='legitimate']
    review.extend({'original_id':r['id'], 'kind':'challenge', 'text':evidence[r['id']]}
                  for r in manifest if r['source'] in ('instruction','tail'))
    review.sort(key=lambda r: hashlib.sha256(('codex-blind-pilot-v1:'+r['original_id']).encode()).hexdigest())
    if len(review)>512 or len({r['original_id'] for r in review})!=len(review):
        raise ValueError('duplicate or excessive evaluation cases')
    items, mapping = [], []
    for i,r in enumerate(review):
        if not isinstance(r['text'],str) or not r['text'].strip() or len(r['text'].encode())>12000:
            raise ValueError('complete bounded email evidence required')
        items.append({'id':str(i+1), 'evidence':{'text':r['text']}})
        mapping.append({'id':str(i+1), 'original_id':r['original_id'], 'kind':r['kind']})
    return items, mapping


def validate_answers(answers, mapping):
    if set(answers)!={'verdicts'} or not isinstance(answers['verdicts'],list):
        raise ValueError('invalid answer envelope')
    verdicts = answers['verdicts']
    if len(verdicts)!=len(mapping) or len({v['id'] for v in verdicts})!=len(verdicts) or {v['id'] for v in verdicts}!={m['id'] for m in mapping}:
        raise ValueError('incomplete, duplicated or unknown answer ids')
    for v in verdicts:
        if set(v)!={'id','category','explanation'} or v['category'] not in ('legitimate','spam','phishing','unknown') or not isinstance(v['explanation'],str) or not v['explanation'].strip():
            raise ValueError('invalid classification')
    return {v['id']:v for v in verdicts}


def command(root):
    root = Path(root).resolve()
    return ['codex','exec','--ignore-user-config','--skip-git-repo-check',
            '--ephemeral','--sandbox','read-only','-C',str(root/'empty'),
            '-c','features.shell_tool=false','-c','features.apply_patch_freeform=false',
            '-c','features.apps=false','-c','features.multi_agent=false',
            '-c','web_search="disabled"','--output-schema',str(root/'schema.json'),
            '--output-last-message',str(root/'answers.json'),'--color','never','-']


def run(root):
    root = Path(root).resolve()
    contract = json.loads((root/'contract.json').read_text())
    for name,digest in contract['files'].items():
        if sha(root/name)!=digest:
            raise ValueError('frozen pilot input changed')
    if (root/'answers.json').exists() or (root/'stdout.log').exists():
        raise ValueError('never overwrite an exposed experiment')
    args = command(root)
    (root/'command.json').write_text(json.dumps(args,indent=2)+'\n')
    start = time.monotonic()
    with (root/'prompt.txt').open() as payload, (root/'stdout.log').open('w') as out, (root/'stderr.log').open('w') as err:
        result = subprocess.run(args,stdin=payload,stdout=out,stderr=err,timeout=600)
    (root/'runtime.json').write_text(json.dumps({'seconds':time.monotonic()-start,'exit_code':result.returncode},indent=2)+'\n')
    if result.returncode!=0:
        raise ValueError('Codex evaluation failed; no automatic retry')
    return validate_answers(json.loads((root/'answers.json').read_text()), json.loads((root/'mapping.json').read_text()))


if __name__=='__main__':
    import argparse
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('private_frozen_experiment',type=Path)
    run(parser.parse_args().private_frozen_experiment)
