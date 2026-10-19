"""Run pinned classifier evaluation under hard 1 CPU / 1 GiB / no-swap limits."""
import argparse
import json
import math
from pathlib import Path
import subprocess
import uuid

MEMORY = 1024 ** 3
ROOT = Path(__file__).resolve().parent


def metrics(rows):
    benign = [r for r in rows if r['expected'] == 'safe']
    malicious = [r for r in rows if r['expected'] == 'phishing']
    invalid = sum(r.get('prediction') not in ['safe', 'phishing'] for r in rows)
    false_positives = sum(r.get('prediction') != 'safe' for r in benign)
    missed = sum(r.get('prediction') != 'phishing' for r in malicious)
    return {'count': len(rows), 'benign': len(benign), 'phishing': len(malicious),
            'invalid_or_failed': invalid, 'false_positives': false_positives, 'missed_phishing': missed,
            'false_positive_rate': false_positives / len(benign) if benign else None,
            'phishing_recall': 1 - missed / len(malicious) if malicious else None,
            'accuracy': sum(r.get('prediction') == r['expected'] for r in rows) / len(rows) if rows else None}


def quality_groups(rows):
    groups = {
        'authored_email': [r for r in rows if r['id'].startswith('authored-')],
        'publisher_email': [r for r in rows if r['id'].startswith(('publisher-test-0-', 'publisher-test-1-'))],
        'publisher_url': [r for r in rows if r['id'].startswith(('publisher-test-2-', 'publisher-test-3-'))],
        'instruction_challenge': [r for r in rows if r['id'].startswith('challenge-instruction-')],
        'long_tail_challenge': [r for r in rows if r['id'].startswith('challenge-tail-')],
    }
    return {name: metrics(group) for name, group in groups.items() if group}


def performance(rows):
    timings = sorted(row['millis'] for row in rows)
    return {'count': len(timings), 'latency_ms': {
        'first': rows[0]['millis'], 'p50': timings[math.ceil(len(timings) * .5) - 1],
        'p95': timings[math.ceil(len(timings) * .95) - 1], 'max': timings[-1]},
        'sequential_cases_per_minute': len(timings) * 60000 / sum(timings)}


def docker(*args, check=True):
    return subprocess.run(['docker', *args], capture_output=True, text=True, check=check)


def run(args):
    model_dir = Path(args.models).resolve()
    corpus = Path(args.dataset).resolve()
    # The in-container verifier also checks actual bytes; reject altered local manifests here.
    if json.loads((model_dir / 'artifacts.json').read_text()) != json.loads((ROOT / 'artifacts.json').read_text()):
        raise ValueError('unrecognized artifact manifest')
    name = 'thura-classifier-benchmark-' + uuid.uuid4().hex[:12]
    report = {'status': 'failed', 'constraints_verified': False,
              'limits': {'cpus': 1, 'memory_bytes': MEMORY, 'swap_bytes': 0},
              'gates': json.loads((ROOT / 'gates.json').read_text())}
    try:
        docker('create', '--name', name, '--cpus', '1', '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
               '--pids-limit', '64', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--tmpfs', '/tmp:size=16m,mode=1777',
               '--mount', f'type=bind,src={model_dir},dst=/models,readonly',
               '--mount', f'type=bind,src={corpus},dst=/data/cases.json,readonly',
               args.image, '--precision', args.precision)
        inspection = json.loads(docker('inspect', name).stdout)[0]
        host = inspection['HostConfig']
        report['docker'] = {'image_id': inspection['Image'], 'host_config':
                            {k: host[k] for k in ['NanoCpus', 'Memory', 'MemorySwap', 'NetworkMode']}}
        report['architecture'] = docker('image', 'inspect', args.image, '--format', '{{.Architecture}}').stdout.strip()
        if host['NanoCpus'] != 1_000_000_000 or host['Memory'] != MEMORY or host['MemorySwap'] != MEMORY or host['NetworkMode'] != 'none':
            raise ValueError('Docker resource isolation not enforced')
        result = docker('start', '-a', name, check=False)
        state = json.loads(docker('inspect', name).stdout)[0]['State']
        report['container_state'] = {k: state[k] for k in ['ExitCode', 'OOMKilled', 'StartedAt', 'FinishedAt']}
        if result.returncode or state['ExitCode']:
            report['reason'] = 'Classifier process failed; no passing benchmark claimed.'
            report['diagnostic'] = result.stderr[-2000:]
            return report
        report.update(json.loads(result.stdout))
        cgroup = report['cgroup']
        quota, period = map(int, cgroup['cpu.max'].split())
        events = dict(line.split() for line in cgroup['memory.events'].splitlines())
        if (quota != period or int(cgroup['memory.max']) != MEMORY or int(cgroup['memory.swap.max']) != 0
                or int(events.get('oom', 0)) or int(events.get('oom_kill', 0)) or state['OOMKilled']
                or int(cgroup['memory.peak']) >= MEMORY):
            raise ValueError('cgroup limits or no-OOM requirement failed')
        report['constraints_verified'] = True
        report['groups'] = quality_groups(report['results'])
        report.update(performance(report['results']))
        emails = [row for row in report['results'] if not row['id'].startswith(('publisher-test-2-', 'publisher-test-3-'))]
        if emails:
            report['email_performance'] = performance(emails)
        failures = []
        for label, group in report['groups'].items():
            if group['invalid_or_failed'] or (group['phishing_recall'] is not None and group['phishing_recall'] < .95):
                failures.append(label + ': phishing recall/validity below gate')
            if group['false_positive_rate'] is not None and group['false_positive_rate'] > .02:
                failures.append(label + ': benign false positive rate above gate')
        speed = report.get('email_performance', report)
        if speed['latency_ms']['p95'] > 5000 or speed['sequential_cases_per_minute'] < 5:
            failures.append('throughput/latency below gate')
        report['failures'] = failures
        report['status'] = 'rejected' if failures else 'measured'
        report['quality_note'] = 'Publisher test data is not independently held out by Thura. Authored cases are a smoke test, not a population accuracy estimate. Measured is not automatic production approval.'
        return report
    finally:
        docker('rm', '--force', name, check=False)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--models', required=True)
    parser.add_argument('--dataset', default=str(ROOT / 'cases.json'))
    parser.add_argument('--image', default='thura-mail-classifier-benchmark:local')
    parser.add_argument('--precision', choices=['int8', 'fp32'], default='int8')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    report = run(args)
    Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({k: report.get(k) for k in ['status', 'constraints_verified', 'precision', 'groups', 'latency_ms', 'sequential_cases_per_minute', 'reason']}))
    raise SystemExit(0 if report['status'] == 'measured' else 2)


if __name__ == '__main__':
    main()
