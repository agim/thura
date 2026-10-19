"""Evaluate a locally trained Llama in a private 2-CPU / 2-GiB service."""
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
    model = Path(args.model).resolve()
    provenance = json.loads(Path(args.provenance).read_text())
    model_sha = provenance['model_sha256']
    corpus = Path(args.corpus).resolve()
    client = Path(args.client).resolve() if args.client else root / 'llama1b-client'
    if sha(model) != model_sha or not provenance.get('adapter_sha256'):
        raise ValueError('trained artifact differs from frozen provenance')
    name = 'thura-trained-llama-' + uuid.uuid4().hex[:12]
    report = {'status': 'failed', 'model': 'Llama-Thura-mail-1B Q4_K_M', 'model_sha256': model_sha,
              'model_bytes': model.stat().st_size, 'corpus_sha256': sha(corpus), 'client_sha256': sha(client),
              'limits': {'cpus': 2, 'memory_bytes': MEMORY, 'swap_bytes': 0}, 'phase': 'trained model: exposed regression plus validation spam pilot',
              'production_acceptance': False, 'training_provenance': provenance}
    report['latency_budget_seconds'] = args.latency_budget_seconds
    try:
        grammar_args = ['--grammar', 'root ::= "A" | "B" | "C"'] if args.class_decision else []
        if args.class_decision and provenance.get('objective') != 'class-decision':
            raise ValueError('decision serving requires matching trained objective')
        docker('create', '--name', name, '--cpus', '2', '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
               '--pids-limit', '128', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--tmpfs', '/tmp:size=32m,mode=1777', '--user', '65532:65532',
               '--mount', f'type=bind,src={model},dst=/models/model.gguf,readonly',
               '--mount', f'type=bind,src={client},dst=/bench,readonly',
               IMAGE, '--model', '/models/model.gguf', '--alias', 'thura-mail-llama1b', '--host', '127.0.0.1', '--port', '8080',
               '--ctx-size', '4096', '--parallel', '1', '--threads', '2', '--threads-batch', '2', '--n-gpu-layers', '0',
               '--batch-size', '128', '--ubatch-size', '128', '--fit', 'off', '--no-context-shift', '--no-webui', '--jinja', '--seed', '42', *grammar_args)
        inspection = json.loads(docker('inspect', name).stdout)[0]
        host = inspection['HostConfig']
        if host['NanoCpus'] != 2000000000 or host['Memory'] != MEMORY or host['MemorySwap'] != MEMORY or host['NetworkMode'] != 'none':
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
        if args.class_decision and report['prompt_sha256'] != provenance['prompt_sha256']:
            raise ValueError('benchmark client differs from the trained contract')
        group = report['cgroup']
        quota, period = map(int, group['cpu.max'].split())
        events = dict(line.split() for line in group['memory.events'].splitlines())
        report['constraints_verified'] = group['memory.max'] == str(MEMORY) and group['memory.swap.max'] == '0' and quota == 2 * period
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
        report['performance_acceptance'] = report['latency_ms']['p95'] <= args.latency_budget_seconds * 1000 and report['sequential_emails_per_minute'] >= 5
        report['numerical_acceptance'] = acceptance(measured)
        report['challenge_acceptance'] = all((report['cohorts'][source]['phishing_recall'] or 0) >= .95 for source in ['instruction', 'tail'])
        # Latency is a planning measurement: the user explicitly prioritizes accuracy.
        report['status'] = 'pilot_pass_only' if report['resource_acceptance'] and report['numerical_acceptance'] and report['challenge_acceptance'] else 'rejected'
        report['context_tokens'] = 4096
        report['context_shift'] = False
        report['decoding'] = {'temperature': 0, 'seed': 42, 'max_output_tokens': 8 if args.class_decision else 32, 'parallel_slots': 1,
                              'class_decision_only': args.class_decision}
        return report
    finally:
        docker('rm', '--force', name, check=False)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    parser.add_argument('--model', required=True)
    parser.add_argument('--provenance', required=True)
    parser.add_argument('--client')
    parser.add_argument('--class-decision', action='store_true')
    parser.add_argument('--latency-budget-seconds', type=float, default=30)
    parser.add_argument('--corpus', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    result = run(args)
    Path(args.output).write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps({key: result.get(key) for key in ['status', 'reason', 'metrics', 'latency_ms', 'cohorts']}))
