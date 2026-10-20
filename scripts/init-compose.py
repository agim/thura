#!/usr/bin/env python3
"""Create private local Compose secrets once; never print credentials or overwrite."""
import os
from pathlib import Path
import secrets
import sys

root = Path(__file__).resolve().parent.parent
path = root / '.env'
if '--upgrade-mail' in sys.argv:
    if not path.is_file() or path.is_symlink():
        raise SystemExit('Create a private .env first; refusing a missing or symbolic-link file.')
    if path.stat().st_mode & 0o077:
        raise SystemExit('Make .env private (chmod 600 .env) before adding a secret.')
    if any(line.strip().startswith('THURA_RSPAMD_PASSWORD=') for line in path.read_text().splitlines()):
        raise SystemExit('Existing mail-filter secret preserved.')
    with path.open('a') as stream:
        stream.write('\nTHURA_RSPAMD_PASSWORD='+secrets.token_urlsafe(36)+'\n')
    print('Added the private mail-filter secret; all existing settings were preserved.')
    raise SystemExit(0)
body = '# Private Compose bootstrap values. Back up the master key separately.\n' + '\n'.join(
    name + '=' + (secrets.token_hex(32) if name == 'LIDZA_MASTER_KEY' else secrets.token_urlsafe(36))
    for name in ('POSTGRES_PASSWORD', 'AUTH_SECRET', 'LIDZA_MASTER_KEY', 'THURA_SETUP_TOKEN', 'THURA_POSTFIX_TOKEN', 'THURA_RSPAMD_PASSWORD')
) + '\n'
try:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
except FileExistsError:
    raise SystemExit('Existing .env preserved. Add missing Compose values privately using docs/postfix.md.')
with os.fdopen(fd, 'w') as stream:
    stream.write(body)
(root / 'deploy/private/smtp').mkdir(parents=True, exist_ok=True, mode=0o700)
print('Created private .env. Open it locally for the first-administrator setup token. Run docker compose --env-file .env -f deploy/compose.yaml up -d --build.')
