#!/usr/bin/env python3
"""Two isolated Synapse fixtures, with public synthetic credentials. No production use."""
import argparse
import json
from pathlib import Path
import subprocess
import time
import urllib.request

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--root', default='/tmp/thura-matrix-fixtures')
args = p.parse_args()
root = Path(args.root).resolve()
root.mkdir(parents=True, exist_ok=True)
image = 'ghcr.io/element-hq/synapse@sha256:76ffaa19418923ab7b70fa0947c1ae16c07b45a65f596c1ff0424ce5eac8b185'
network = 'thura-matrix-test'
subprocess.run(['docker', 'network', 'create', network], capture_output=True, check=False)
for label, port in [('a', 8108), ('b', 8109)]:
    data = root / label
    data.mkdir(exist_ok=True)
    data.chmod(0o777)  # Disposable synthetic test data, writable by Synapse UID 991.
    server = f'matrix-{label}.thura.test:8448'
    registration = {'id': 'thura', 'url': None, 'as_token': f'thura-matrix-{label}-application-service-fixture-token-99184', 'hs_token': f'thura-matrix-{label}-homeserver-fixture-token-88341', 'sender_localpart': 'thura_bot', 'rate_limited': False, 'namespaces': {'users': [{'exclusive': True, 'regex': f'@thura_.*:matrix-{label}\\.thura\\.test:8448'}], 'aliases': [{'exclusive': True, 'regex': f'#thura_room_.*:matrix-{label}\\.thura\\.test:8448'}], 'rooms': []}}
    config = {'server_name': server, 'public_baseurl': f'http://127.0.0.1:{port}/', 'pid_file': '/data/homeserver.pid', 'tls_certificate_path': '/data/server.crt', 'tls_private_key_path': '/data/server.key', 'listeners': [{'port': 8448, 'tls': True, 'type': 'http', 'resources': [{'names': ['federation'], 'compress': False}]}, {'port': 8008, 'tls': False, 'type': 'http', 'resources': [{'names': ['client', 'federation'], 'compress': False}]}], 'database': {'name': 'sqlite3', 'args': {'database': '/data/homeserver.db'}}, 'log_config': '/data/log.yaml', 'media_store_path': '/data/media_store', 'signing_key_path': '/data/server.signing.key', 'report_stats': False, 'enable_registration': False, 'macaroon_secret_key': f'thura-matrix-{label}-macaroon-test-secret-77183', 'form_secret': f'thura-matrix-{label}-form-test-secret-33148', 'app_service_config_files': ['/data/registration.yaml'], 'federation_verify_certificates': False, 'federation_domain_whitelist': ['matrix-a.thura.test:8448', 'matrix-b.thura.test:8448'], 'ip_range_blacklist': [], 'ip_range_whitelist': ['172.16.0.0/12'], 'suppress_key_server_warning': True, 'trusted_key_servers': []}
    for name, value in [('registration.yaml', registration), ('homeserver.yaml', config)]:
        path = data / name
        path.write_text(json.dumps(value))
        path.chmod(0o644)
    (data / 'log.yaml').write_text('version: 1\nformatters:\n  simple:\n    format: "%(levelname)s %(name)s %(message)s"\nhandlers:\n  console:\n    class: logging.StreamHandler\n    formatter: simple\nroot:\n  level: WARNING\n  handlers: [console]\n')
    (data / 'log.yaml').chmod(0o644)
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2', '-keyout', str(data / 'server.key'), '-out', str(data / 'server.crt'), '-subj', f'/CN=matrix-{label}.thura.test'], check=True, capture_output=True)
    (data / 'server.key').chmod(0o644)
    (data / 'server.crt').chmod(0o644)
    subprocess.run(['docker', 'rm', '-f', f'thura-matrix-{label}'], capture_output=True, check=False)
    cmd = ['docker', 'run', '-d', '--name', f'thura-matrix-{label}', '--network', network, '--network-alias', f'matrix-{label}.thura.test', '-p', f'127.0.0.1:{port}:8008']
    for variable in ['HTTPS_PROXY', 'HTTP_PROXY', 'https_proxy', 'http_proxy']:
        cmd += ['-e', variable + '=']
    for variable in ['NO_PROXY', 'no_proxy']:
        cmd += ['-e', variable + '=matrix-a.thura.test,matrix-b.thura.test,localhost,127.0.0.1']
    cmd += ['-v', f'{data}:/data', image]
    subprocess.run(cmd, check=True, capture_output=True)
for port in [8108, 8109]:
    deadline = time.monotonic() + 60
    while True:
        try:
            with urllib.request.urlopen(f'http://127.0.0.1:{port}/_matrix/client/versions', timeout=2) as response:
                if response.status == 200:
                    break
        except (OSError, ValueError):
            pass
        if time.monotonic() >= deadline:
            raise SystemExit(f'Synapse fixture on {port} did not become ready; inspect its Docker logs.')
        time.sleep(0.5)
print('Two disposable Matrix federation fixtures are ready on client ports 8108 and 8109.')
