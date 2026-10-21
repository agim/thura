"""Deployment-owned Matrix registration; no interactive homeserver account setup."""
import hashlib
import json
import os
from pathlib import Path
import re


def configure(runtime=Path('/run/thura'), state=None):
    state = state or Path(os.environ.get('THURA_MATRIX_STATE', '/app/matrix'))
    name = os.environ.get('THURA_SERVICE_MATRIX_SERVER_NAME', os.environ.get('MATRIX_SERVER_NAME', ''))
    token = os.environ.get('THURA_SERVICE_MATRIX_AS_TOKEN', os.environ.get('MATRIX_AS_TOKEN', ''))
    hs_token = os.environ.get('THURA_SERVICE_MATRIX_HS_TOKEN', os.environ.get('MATRIX_HS_TOKEN', ''))
    if not re.fullmatch(r'[a-z0-9][a-z0-9.-]*\.[a-z0-9.-]+', name) or len(token) < 32 or len(hs_token) < 32:
        raise ValueError('Deployment must supply the Matrix identity and stable service credentials')
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chown(state, 65532, 65532)
    identity = state/'server-name'
    if identity.exists() and identity.read_text() != name:
        raise ValueError('Retain the original Matrix server identity on updates')
    if not identity.exists():
        identity.write_text(name)
        identity.chmod(0o600)
        os.chown(identity, 65532, 65532)
    runtime.mkdir(mode=0o755, parents=True, exist_ok=True)
    registration = {
        'id': 'thura', 'url': None, 'as_token': token, 'hs_token': hs_token,
        'sender_localpart': 'thura_bot', 'rate_limited': False,
        'namespaces': {'users': [{'exclusive': True, 'regex': r'@thura_.*:'+re.escape(name)}], 'rooms': [], 'aliases': []},
    }
    logging = {'version': 1, 'formatters': {'plain': {'format': '%(levelname)s %(name)s %(message)s'}}, 'handlers': {'console': {'class': 'logging.StreamHandler', 'formatter': 'plain'}}, 'root': {'level': 'WARNING', 'handlers': ['console']}, 'disable_existing_loggers': False}
    config = {
        'server_name': name, 'pid_file': str(runtime/'matrix.pid'),
        'listeners': [{'port': 8008, 'bind_addresses': ['127.0.0.1'], 'type': 'http', 'tls': False, 'x_forwarded': False, 'resources': [{'names': ['client'], 'compress': False}]}],
        'database': {'name': 'sqlite3', 'args': {'database': str(state/'homeserver.db')}},
        'media_store_path': str(state/'media'), 'signing_key_path': str(state/'signing.key'),
        'log_config': str(runtime/'matrix-log.json'), 'report_stats': False,
        'enable_registration': False, 'allow_guest_access': False,
        'app_service_config_files': [str(runtime/'matrix-registration.json')],
        'federation_domain_whitelist': [], 'trusted_key_servers': [],
        'suppress_key_server_warning': True,
        'macaroon_secret_key': hashlib.sha256(('thura-matrix-macaroon:'+hs_token).encode()).hexdigest(),
        'form_secret': hashlib.sha256(('thura-matrix-form:'+hs_token).encode()).hexdigest(),
        'caches': {'global_factor': 0.1, 'per_cache_factors': {'get_users_in_room': 0.1}},
        'event_cache_size': '1K',
    }
    for filename, value in [('matrix-registration.json', registration), ('matrix-log.json', logging), ('matrix.json', config)]:
        path = runtime/filename
        path.write_text(json.dumps(value))
        path.chmod(0o600)
        os.chown(path, 65532, 65532)
    return runtime/'matrix.json'
