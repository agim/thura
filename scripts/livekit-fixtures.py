#!/usr/bin/env python3
"""Start a loopback-only LiveKit fixture with public synthetic credentials (Linux Docker)."""
from pathlib import Path
import argparse
import subprocess
import time
import urllib.request
p = argparse.ArgumentParser(description=__doc__)
p.add_argument('--root', default='/tmp/thura-livekit-fixture')
args = p.parse_args()
root = Path(args.root).resolve()
root.mkdir(parents=True, exist_ok=True)
config = root / 'config.yaml'
config.write_text('''port: 7880
bind_addresses:
  - 127.0.0.1
rtc:
  node_ip: 127.0.0.1
  tcp_port: 7881
  udp_port: 7882
  use_external_ip: false
room:
  auto_create: false
keys:
  thura-local-test-key: thura-livekit-local-service-fixture-secret-981734
webhook:
  api_key: thura-local-test-key
  urls:
    - http://host.docker.internal:3002/api/v1/meet/webhook
logging:
  level: warn
''')
config.chmod(0o644)
subprocess.run(['docker', 'rm', '-f', 'thura-livekit-test'], capture_output=True, check=False)
cmd = ['docker', 'run', '-d', '--name', 'thura-livekit-test', '--network', 'host', '--add-host', 'host.docker.internal:host-gateway']
for variable in ['HTTPS_PROXY', 'HTTP_PROXY', 'https_proxy', 'http_proxy']:
    cmd += ['-e', variable + '=']
for variable in ['NO_PROXY', 'no_proxy']:
    cmd += ['-e', variable + '=host.docker.internal,localhost,127.0.0.1']
cmd += ['-v', f'{config}:/etc/thura-livekit.yaml:ro', 'livekit/livekit-server@sha256:d0c04791bf63ca8dcea123571827d56ba9504bd57c8d5de60ee03b5408cc3154', '--config', '/etc/thura-livekit.yaml']
subprocess.run(cmd, capture_output=True, check=True)
deadline = time.monotonic() + 30
while True:
    try:
        with urllib.request.urlopen('http://127.0.0.1:7880/', timeout=2) as response:
            if response.status == 200:
                break
    except (OSError, ValueError):
        pass
    if time.monotonic() >= deadline:
        raise SystemExit('LiveKit fixture did not become ready; inspect thura-livekit-test logs.')
    time.sleep(0.5)
print('Synthetic LiveKit fixture ready on loopback ports 7880/7881/7882. Start the test app on 0.0.0.0:3002 for signed webhooks.')
