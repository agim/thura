"""Launch a dedicated 1-CPU/1-GiB, no-swap, no-network inference run."""
import argparse
import json
import math
from pathlib import Path
import subprocess
import uuid

MEMORY = 1024 ** 3


def regression_acceptance(report):
    # This authored challenge has no ordinary-spam examples. Its absence is not
    # a spam recall result; the separate, three-class final test still owns that gate.
    measured = report['metrics']
    if (measured['errors'] or (measured['phishing_recall'] or 0) < .95
            or measured['legitimate_false_positive_rate'] is None
            or measured['legitimate_false_positive_rate'] > .02):
        return False
    return all((value['phishing_recall'] or 0) >= .95
               for name, value in report['cohorts'].items()
               if name in ['instruction', 'tail'])


def docker(*args, check=True, timeout=60):
    return subprocess.run(['docker', *args], capture_output=True, text=True, check=check, timeout=timeout)


def run(args):
    name = 'thura-local-mail-benchmark-' + uuid.uuid4().hex[:12]
    report = {'status': 'failed', 'constraints_verified': False, 'limits': {'cpus': 1, 'memory_bytes': MEMORY, 'swap_bytes': 0}}
    try:
        command = ['create', '--name', name, '--cpus', '1', '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
                   '--pids-limit', '64', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
                   '--network', 'none', '--tmpfs', '/tmp:size=16m,mode=1777',
                   '--mount', f'type=bind,src={Path(args.models).resolve()},dst=/models,readonly',
                   '--mount', f'type=bind,src={Path(args.corpus).resolve()},dst=/data/corpus.jsonl,readonly']
        if args.selection:
            command.extend(['--mount', f'type=bind,src={Path(args.selection).resolve()},dst=/selection/selection.json,readonly'])
        command.extend([args.image, '--kind', args.kind, '--phase', args.phase])
        docker(*command)
        inspection = json.loads(docker('inspect', name).stdout)[0]
        host = inspection['HostConfig']
        report['docker'] = {'image_id': inspection['Image'], 'host_config': {key: host[key] for key in ['NanoCpus', 'Memory', 'MemorySwap', 'NetworkMode']}}
        if host['NanoCpus'] != 1000000000 or host['Memory'] != MEMORY or host['MemorySwap'] != MEMORY or host['NetworkMode'] != 'none':
            raise ValueError('requested resource isolation not enforced')
        response = docker('start', '-a', name, check=False, timeout=900)
        state = json.loads(docker('inspect', name).stdout)[0]['State']
        report['container_state'] = {key: state[key] for key in ['ExitCode', 'OOMKilled', 'StartedAt', 'FinishedAt']}
        if response.returncode or state['ExitCode'] or state['OOMKilled']:
            report['reason'] = 'Inference failed under the requested hard limits.'
            report['diagnostic'] = response.stderr[-3000:]
            return report
        report.update(json.loads(response.stdout))
        cgroup = report['cgroup']
        quota, period = map(int, cgroup['cpu.max'].split())
        events = dict(line.split() for line in cgroup['memory.events'].splitlines())
        if (quota != period or int(cgroup['memory.max']) != MEMORY or int(cgroup['memory.swap.max']) != 0
                or int(cgroup['memory.peak']) >= MEMORY or int(events.get('oom', 0)) or int(events.get('oom_kill', 0))):
            raise ValueError('cgroup or memory acceptance failed')
        report['constraints_verified'] = True
        timings = sorted(row['millis'] for row in report['results'])
        report['latency_ms'] = {'p50': timings[math.ceil(len(timings) * .5) - 1],
                                'p95': timings[math.ceil(len(timings) * .95) - 1], 'max': timings[-1]}
        report['sequential_emails_per_minute'] = len(timings) * 60000 / sum(timings)
        report['performance_acceptance'] = report['latency_ms']['p95'] <= 5000 and report['sequential_emails_per_minute'] >= 5
        if args.phase == 'regression':
            report['numerical_acceptance'] = regression_acceptance(report)
        report['status'] = 'measured' if args.phase == 'validation' else ('passed' if report['numerical_acceptance'] and report['performance_acceptance'] else 'rejected')
        # A later real phishing cohort cannot be hidden by aggregate synthetic results.
        if args.phase == 'test' and report['cohorts'].get('nazario-2025', {}).get('phishing_recall', 0) < .95:
            report['status'] = 'rejected'
            report['reason'] = 'Later real-phishing cohort failed 95% recall.'
        return report
    finally:
        docker('rm', '--force', name, check=False)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--models', required=True)
    parser.add_argument('--corpus', required=True)
    parser.add_argument('--kind', choices=['baseline', 'int8', 'fp32'], required=True)
    parser.add_argument('--phase', choices=['validation', 'test', 'regression'], required=True)
    parser.add_argument('--selection')
    parser.add_argument('--image', default='thura-local-mail-evaluation:local')
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    if args.phase != 'validation' and not args.selection:
        parser.error('final evaluation requires a validation-selected threshold artifact')
    report = run(args)
    Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
    if args.phase == 'validation' and report['status'] == 'measured':
        Path(args.output).with_suffix('.selection.json').write_text(json.dumps(report['selection'], indent=2) + '\n')
    print(json.dumps({key: report.get(key) for key in ['status', 'phase', 'kind', 'metrics', 'latency_ms', 'reason']}))
    raise SystemExit(0 if report['status'] in ['passed', 'measured'] else 2)


if __name__ == '__main__':
    main()
