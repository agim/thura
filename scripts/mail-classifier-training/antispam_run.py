"""Measure the pinned binary spam candidate without opening the final test set."""
import argparse
import json
import math
from pathlib import Path
import uuid

from benchmark import MEMORY, docker


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--candidate', choices=['spam', 'phishing'], default='spam')
    parser.add_argument('--kind', choices=['fp32', 'int8'], default='fp32')
    parser.add_argument('--memory-gib', type=int, choices=[1, 2], default=1)
    parser.add_argument('--corpus')
    parser.add_argument('--phase', choices=['validation', 'regression'], default='validation')
    args = parser.parse_args()
    root = Path(args.root).resolve()
    memory = args.memory_gib * MEMORY
    corpus = Path(args.corpus).resolve() if args.corpus else root / 'dataset-locked' / 'validation.jsonl'
    name = 'thura-antispam-comparison-' + uuid.uuid4().hex[:12]
    try:
        docker('create', '--name', name, '--cpus', '1', '--memory', str(memory), '--memory-swap', str(memory),
               '--pids-limit', '64', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--tmpfs', '/tmp:size=16m,mode=1777', '--entrypoint', 'python',
               '--mount', f'type=bind,src={root / ("antispam-" + args.candidate)},dst=/models,readonly',
               '--mount', f'type=bind,src={corpus},dst=/data/validation.jsonl,readonly',
               '--mount', f'type=bind,src={Path(__file__).with_name("antispam.py").resolve()},dst=/app/antispam.py,readonly',
               'thura-local-mail-evaluation:local', '/app/antispam.py', '--models', '/models', '--corpus', '/data/validation.jsonl',
               '--kind', args.kind, '--phase', args.phase)
        inspection = json.loads(docker('inspect', name).stdout)[0]
        settings = inspection['HostConfig']
        if settings['NanoCpus'] != 1000000000 or settings['Memory'] != memory or settings['MemorySwap'] != memory or settings['NetworkMode'] != 'none':
            raise ValueError('requested constraints not set')
        response = docker('start', '-a', name, timeout=3600, check=False)
        state = json.loads(docker('inspect', name).stdout)[0]['State']
        if response.returncode or state['ExitCode'] or state['OOMKilled']:
            failure = {'status': 'failed', 'kind': args.kind, 'candidate': args.candidate,
                       'limits': {'cpus': 1, 'memory_bytes': memory, 'swap_bytes': 0},
                       'container_state': state, 'image_id': inspection['Image'], 'diagnostic': response.stderr[-4000:]}
            Path(args.output).write_text(json.dumps(failure, indent=2) + '\n')
            print(json.dumps(failure))
            raise SystemExit(2)
        report = json.loads(response.stdout)
        group = report['cgroup']
        quota, period = map(int, group['cpu.max'].split())
        events = dict(line.split() for line in group['memory.events'].splitlines())
        isolation = group['memory.max'] == str(memory) and group['memory.swap.max'] == '0' and quota == period
        resource_acceptance = isolation and int(group['memory.peak']) < memory and not int(events.get('oom', 0)) and not int(events.get('oom_kill', 0))
        timings = sorted(row['millis'] for row in report['results'])
        report.update(constraints_verified=isolation, resource_acceptance=resource_acceptance,
                      status='measured' if resource_acceptance else 'resource_rejected',
                      image_id=inspection['Image'], container_state=state,
                      limits={'cpus': 1, 'memory_bytes': memory, 'swap_bytes': 0},
                      latency_ms={'p50': timings[math.ceil(len(timings) * .5) - 1], 'p95': timings[math.ceil(len(timings) * .95) - 1], 'max': timings[-1]})
        report['performance_acceptance'] = report['latency_ms']['p95'] <= 5000 and len(timings) * 60000 / sum(timings) >= 5
        if args.candidate == 'phishing':
            groups = report['cohorts']
            rates = {label: groups[label]['flagged_as_phishing'] / groups[label]['count'] if groups[label]['count'] else None for label in groups}
            report['binary_phishing_metrics'] = {'phishing_recall': rates['phishing'], 'legitimate_false_positive_rate': rates['legitimate'], 'spam_as_phishing_rate': rates['spam']}
            report['pilot_numerical_acceptance'] = rates['phishing'] is not None and rates['phishing'] >= .95 and rates['legitimate'] is not None and rates['legitimate'] <= .02 and rates['spam'] is not None and rates['spam'] <= .05
            report['status'] = 'pilot_pass_only' if resource_acceptance and report['performance_acceptance'] and report['pilot_numerical_acceptance'] else 'rejected'
        Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({key: report.get(key) for key in ['status', 'cohorts', 'binary_phishing_metrics', 'latency_ms', 'cgroup']}))
    finally:
        docker('rm', '--force', name, check=False)


if __name__ == '__main__':
    main()
