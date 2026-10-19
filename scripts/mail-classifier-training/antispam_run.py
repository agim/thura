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
    args = parser.parse_args()
    root = Path(args.root).resolve()
    name = 'thura-antispam-comparison-' + uuid.uuid4().hex[:12]
    try:
        docker('create', '--name', name, '--cpus', '1', '--memory', str(MEMORY), '--memory-swap', str(MEMORY),
               '--pids-limit', '64', '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
               '--network', 'none', '--tmpfs', '/tmp:size=16m,mode=1777', '--entrypoint', 'python',
               '--mount', f'type=bind,src={root / "antispam-spam"},dst=/models,readonly',
               '--mount', f'type=bind,src={root / "dataset-locked" / "validation.jsonl"},dst=/data/validation.jsonl,readonly',
               '--mount', f'type=bind,src={Path(__file__).with_name("antispam.py").resolve()},dst=/app/antispam.py,readonly',
               'thura-local-mail-evaluation:local', '/app/antispam.py', '--models', '/models', '--corpus', '/data/validation.jsonl')
        inspection = json.loads(docker('inspect', name).stdout)[0]
        settings = inspection['HostConfig']
        if settings['NanoCpus'] != 1000000000 or settings['Memory'] != MEMORY or settings['MemorySwap'] != MEMORY or settings['NetworkMode'] != 'none':
            raise ValueError('requested constraints not set')
        response = docker('start', '-a', name, timeout=900)
        state = json.loads(docker('inspect', name).stdout)[0]['State']
        if state['ExitCode'] or state['OOMKilled']:
            raise ValueError('candidate inference failed under limits')
        report = json.loads(response.stdout)
        group = report['cgroup']
        if group['memory.max'] != str(MEMORY) or group['memory.swap.max'] != '0' or int(group['memory.peak']) >= MEMORY:
            raise ValueError('memory constraints failed')
        quota, period = map(int, group['cpu.max'].split())
        events = dict(line.split() for line in group['memory.events'].splitlines())
        if quota != period or int(events.get('oom', 0)) or int(events.get('oom_kill', 0)):
            raise ValueError('CPU or OOM gate failed')
        timings = sorted(row['millis'] for row in report['results'])
        report.update(constraints_verified=True, image_id=inspection['Image'], container_state=state,
                      latency_ms={'p50': timings[math.ceil(len(timings) * .5) - 1], 'p95': timings[math.ceil(len(timings) * .95) - 1], 'max': timings[-1]})
        Path(args.output).write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({key: report[key] for key in ['cohorts', 'latency_ms', 'cgroup']}))
    finally:
        docker('rm', '--force', name, check=False)


if __name__ == '__main__':
    main()
