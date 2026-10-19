"""A generic 1B comparison in an isolated 1-CPU / 2-GiB service, not an integration."""
import argparse
import hashlib
import json
import math
from pathlib import Path
import subprocess
import sys
import time
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'mail-classifier-training'))
from common import acceptance, metrics

IMAGE = 'ghcr.io/ggml-org/llama.cpp:server@sha256:143926008cbbbab8fae8fd87809875af3e42d3d676c30c11c55c0f3f683a5a07'
MEMORY = 2 * 1024 ** 3
MODEL_SHA = '84fffd9ce0ecc7e0a36ec7a3c3aa21bb888213f8bf3db9408d5520a26412f06e'


def sha(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        while chunk := stream.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def docker(*args, check=True, data=None, timeout=60):
    return subprocess.run(['docker', *args], input=data, capture_output=True, text=True, check=check, timeout=timeout)


def run(args):
    root = Path(args.root).resolve()
    model = root / 'llama1b' / 'model.gguf'
    corpus = Path(args.corpus).resolve()
    client = root / 'llama1b-client'
    if sha(model) != MODEL_SHA or model.stat().st_size != 807694144:
        raise ValueError('generic model differs from pinned official registry blob')
    name = 'thura-llama1b-pilot-' + uuid.uuid4().hex[:12]
    report = {'status': 'failed', 'model': 'Llama-3.2-1B-Instruct Q4_K_M', 'model_sha256': MODEL_SHA,
              'model_bytes': model.stat().st_size, 'corpus_sha256': sha(corpus), 'client_sha256': sha(client),
              'limits': {'cpus': 1, 'memory_bytes': MEMORY, 'swap_bytes': 0}, 'phase': 'known regression plus validation spam pilot',
              'production_acceptance': False}
    try:
        docker('create', '--name', name, '--cpus', '1', '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
               '--pids-limit', '128', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--tmpfs', '/tmp:size=32m,mode=1777', '--user', '65532:65532',
               '--mount', f'type=bind,src={model},dst=/models/model.gguf,readonly',
               '--mount', f'type=bind,src={client},dst=/bench,readonly',
               IMAGE, '--model', '/models/model.gguf', '--alias', 'thura-mail-llama1b', '--host', '127.0.0.1', '--port', '8080',
               '--ctx-size', '8192', '--parallel', '1', '--threads', '1', '--threads-batch', '1', '--n-gpu-layers', '0',
               '--batch-size', '128', '--ubatch-size', '128', '--fit', 'off', '--no-context-shift', '--no-webui', '--jinja', '--seed', '42')
        inspection = json.loads(docker('inspect', name).stdout)[0]
        host = inspection['HostConfig']
        if host['NanoCpus'] != 1000000000 or host['Memory'] != MEMORY or host['MemorySwap'] != MEMORY or host['NetworkMode'] != 'none':
            raise ValueError('requested container constraints not enforced')
        report['image_id'] = inspection['Image']
        started = time.monotonic()
        docker('start', name)
        while time.monotonic() - started < 180:
            state = json.loads(docker('inspect', name).stdout)[0]['State']
            if not state['Running']:
                report.update(reason='Model failed startup under requested limits', container_state=state)
                return report
            health = docker('exec', name, 'curl', '-fsS', '--max-time', '2', 'http://127.0.0.1:8080/health', check=False)
            if health.returncode == 0:
                break
            time.sleep(1)
        else:
            report['reason'] = 'Startup exceeded 180 seconds'
            return report
        report['startup_seconds'] = time.monotonic() - started
        response = docker('exec', '-i', name, '/bench', data=corpus.read_text(), timeout=3600, check=False)
        # Preserve measurements before applying any gate, including a rejected run.
        Path(args.output).with_suffix('.raw.json').write_text(response.stdout)
        Path(args.output).with_suffix('.progress.log').write_text(response.stderr)
        report['container_state'] = json.loads(docker('inspect', name).stdout)[0]['State']
        if response.returncode or report['container_state']['OOMKilled']:
            report.update(reason='Inference failed under requested limits', diagnostic=response.stderr[-3000:])
            return report
        report.update(json.loads(response.stdout))
        group = report['cgroup']
        quota, period = map(int, group['cpu.max'].split())
        events = dict(line.split() for line in group['memory.events'].splitlines())
        report['constraints_verified'] = group['memory.max'] == str(MEMORY) and group['memory.swap.max'] == '0' and quota == period
        report['resource_acceptance'] = (report['constraints_verified'] and int(group['memory.peak']) < MEMORY
                                          and not int(events.get('oom', 0)) and not int(events.get('oom_kill', 0)))
        measured = metrics(report['results'])
        report['metrics'] = measured
        report['cohorts'] = {source: metrics([r for r in report['results'] if r['source'] == source])
                             for source in sorted({r['source'] for r in report['results']})}
        timings = sorted(r['millis'] for r in report['results'])
        report['latency_ms'] = {'p50': timings[math.ceil(len(timings) * .5) - 1],
                                'p95': timings[math.ceil(len(timings) * .95) - 1], 'max': timings[-1]}
        report['sequential_emails_per_minute'] = len(timings) * 60000 / sum(timings)
        report['performance_acceptance'] = report['latency_ms']['p95'] <= 5000 and report['sequential_emails_per_minute'] >= 5
        report['numerical_acceptance'] = acceptance(measured)
        report['challenge_acceptance'] = all((report['cohorts'][source]['phishing_recall'] or 0) >= .95 for source in ['instruction', 'tail'])
        report['status'] = 'pilot_pass_only' if report['resource_acceptance'] and report['numerical_acceptance'] and report['performance_acceptance'] and report['challenge_acceptance'] else 'rejected'
        report['context_tokens'] = 8192
        report['context_shift'] = False
        report['decoding'] = {'temperature': 0, 'seed': 42, 'max_output_tokens': 32, 'parallel_slots': 1}
        return report
    finally:
        docker('rm', '--force', name, check=False)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    parser.add_argument('--corpus', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    result = run(args)
    Path(args.output).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({key: result.get(key) for key in ['status', 'reason', 'metrics', 'latency_ms', 'cohorts']}))
