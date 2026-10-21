"""Run Thura and its small-call SFU in one container, as unprivileged users."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
import urllib.request
import matrix


def managed_state(name, default):
    if os.environ.get('THURA_MANAGED_STATE_IN_STORAGE') == 'true':
        return Path(os.environ.get('STORAGE_DIR', '/app/storage'))/'.thura-services'/name
    return default


def configure(directory=Path('/run/thura')):
    key = os.environ.get('LIVEKIT_API_KEY', '')
    secret = os.environ.get('LIVEKIT_API_SECRET', '')
    if not key or len(secret) < 32:
        raise ValueError('Generate private LiveKit credentials before starting')
    config = {
        'port': 7880,
        'bind_addresses': ['0.0.0.0'],
        'rtc': {'tcp_port': 7881, 'udp_port': 7882, 'use_external_ip': True},
        'room': {'auto_create': False, 'max_participants': 6},
        'keys': {key: secret},
        'webhook': {'api_key': key, 'urls': ['http://127.0.0.1:3000/api/v1/meet/webhook']},
        'logging': {'level': 'warn'},
    }
    if os.environ.get('THURA_RTC_NODE_IP'):
        config['rtc']['node_ip'] = os.environ['THURA_RTC_NODE_IP']
        config['rtc']['use_external_ip'] = False
    directory.mkdir(mode=0o755, exist_ok=True)
    directory.chmod(0o755)
    path = directory / 'livekit.json'
    path.write_text(json.dumps(config))
    path.chmod(0o600)
    os.chown(path, 65532, 65532)
    storage = Path(os.environ.get('STORAGE_DIR', '/app/storage'))
    storage.mkdir(mode=0o700, exist_ok=True)
    os.chown(storage, 65532, 65532)
    if os.environ.get('THURA_MANAGED_STATE_IN_STORAGE') == 'true':
        state_root=storage/'.thura-services'
        state_root.mkdir(mode=0o700,exist_ok=True)
        state_root.chmod(0o700)
        os.chown(state_root,65532,65532)
    return path


def health():
    for pid in json.loads(Path('/run/thura/processes.json').read_text()).values():
        os.kill(pid, 0)
    urls=['http://127.0.0.1:3000/api/v1/health', 'http://127.0.0.1:7880/']
    if os.environ.get('THURA_MANAGED_MATRIX') == 'true':
        urls.append('http://127.0.0.1:8008/_matrix/client/versions')
    if os.environ.get('THURA_MANAGED_OLLAMA') == 'true':
        urls.append('http://127.0.0.1:11434/api/tags')
    for url in urls:
        with urllib.request.urlopen(url, timeout=3) as response:
            if response.status != 200:
                raise RuntimeError('Application or meeting server unavailable')


def main():
    config = configure()
    stopping = False
    children = []
    def stop(_signal, _frame):
        nonlocal stopping
        stopping = True
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    try:
        # Mail owns Valkey in the shared namespace; wait without requiring
        # Thura's HTTP API so managed Postfix startup has no dependency cycle.
        deadline = time.monotonic() + 300
        while not stopping:
            try:
                with socket.create_connection(('127.0.0.1', 6379), timeout=1):
                    break
            except OSError:
                if time.monotonic() >= deadline:
                    raise RuntimeError('Private Valkey did not become ready')
                time.sleep(.25)
        if stopping:
            return
        commands=[['/app/thura'], ['/usr/local/bin/livekit-server', '--config', str(config)]]
        if os.environ.get('THURA_MANAGED_MATRIX') == 'true':
            matrix_config=matrix.configure(state=managed_state('matrix',None))
            subprocess.run(['/usr/local/bin/python', '-m', 'synapse.app.homeserver', '--generate-keys', '--config-path', str(matrix_config)], user=65532, group=65532, extra_groups=[], check=True)
            commands.append(['/usr/local/bin/python', '-m', 'synapse.app.homeserver', '--config-path', str(matrix_config)])
        if os.environ.get('THURA_MANAGED_OLLAMA') == 'true':
            model_state=managed_state('models',Path('/app/models'))
            model_state.mkdir(mode=0o700, parents=True, exist_ok=True)
            os.chown(model_state,65532,65532)
            commands.append(['/usr/local/bin/ollama','serve'])
        for command in commands:
            environment = dict(os.environ)
            if 'ollama' in command[0]:
                environment.update({'HOME':str(model_state),'OLLAMA_MODELS':str(model_state/'models'),'OLLAMA_HOST':'127.0.0.1:11434','OLLAMA_NO_CLOUD':'1','OLLAMA_CONTEXT_LENGTH':'2048','OLLAMA_NUM_PARALLEL':'1','OLLAMA_MAX_LOADED_MODELS':'1','OLLAMA_MAX_QUEUE':'2','OLLAMA_KEEP_ALIVE':'0'})
            environment['GOMEMLIMIT'] = '96MiB' if 'livekit-server' in command[0] else '256MiB'
            children.append(subprocess.Popen(command, user=65532, group=65532, extra_groups=[], env=environment))
        Path('/run/thura/processes.json').write_text(json.dumps({str(index): child.pid for index, child in enumerate(children)}))
        while not stopping:
            if any(child.poll() is not None for child in children):
                raise RuntimeError('Application or meeting server exited')
            time.sleep(.25)
    finally:
        for child in children:
            if child.poll() is None:
                child.terminate()
        for child in children:
            try:
                child.wait(timeout=15)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)


if __name__ == '__main__':
    try:
        health() if '--health' in sys.argv else main()
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        print('Thura application services unavailable; inspect service logs', file=sys.stderr)
        sys.exit(1)
