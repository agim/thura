"""Persistent DKIM keys and standards-based Rspamd authentication/reporting."""
import base64
import datetime
import json
import hashlib
import os
from pathlib import Path
import pwd
import re
import secrets
import signal
import subprocess
import tempfile

STATE = Path('/var/spool/postfix/etc/thura-auth')
CONFIG = Path('/etc/rspamd/local.d')


def write(path, content, mode=0o640, group=None):
    raw = content.encode() if isinstance(content, str) else content
    if path.exists() and path.read_bytes() == raw:
        return False
    fd, name = tempfile.mkstemp(prefix='.thura-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as out:
            os.fchmod(out.fileno(), mode)
            if group is not None:
                os.fchown(out.fileno(), 0, group)
            out.write(raw)
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, path)
    finally:
        Path(name).unlink(missing_ok=True)
    return True


def ucl(values):
    lines = []
    for key, value in values.items():
        if isinstance(value, dict):
            lines.append(json.dumps(key) + " {\n" + ucl(value) + "}\n")
        else:
            lines.append(json.dumps(key) + " = " + json.dumps(value) + ";\n")
    return ''.join(lines)


def reload_rspamd():
    subprocess.run(['rspamadm', 'configtest'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
    status = json.loads(Path('/tmp/thura-mail-processes.json').read_text())
    number = int(status.get('rspamd', status.get('0', 0)))
    if number < 2 or Path('/proc/' + str(number) + '/comm').read_text().strip() != 'rspamd':
        raise ValueError('Refusing to signal an unexpected Rspamd process')
    os.kill(number, signal.SIGHUP)


def configure(domains, hostname, report_domain='', state=None, config=None):
    state, config = state or STATE, config or CONFIG
    group = pwd.getpwnam('_rspamd').pw_gid
    if state.is_symlink():
        raise ValueError("DKIM state must not be a symlink")
    state.mkdir(mode=0o750, parents=True, exist_ok=True)
    state.chmod(0o750)
    os.chown(state, 0, group)
    selector_file = state / 'selector'
    if selector_file.is_symlink():
        raise ValueError('DKIM selector must not be a symlink')
    if not selector_file.exists():
        write(selector_file, 'thura-' + secrets.token_hex(4), group=group)
    selector = selector_file.read_text().strip()
    if not re.fullmatch(r'thura-[a-f0-9]{8}', selector):
        raise ValueError('Invalid persisted DKIM selector; retain original keys and repair state')
    records = []
    paths, selectors = [], []
    for domain in domains:
        if not re.fullmatch(r'[a-z0-9][a-z0-9.-]*\.[a-z0-9-]+', domain):
            raise ValueError('Invalid DKIM domain')
        key = state / (domain + '.' + selector + '.key')
        if key.is_symlink():
            raise ValueError('DKIM keys must not be symlinks')
        if not key.exists():
            generated = subprocess.check_output(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:2048'], stderr=subprocess.DEVNULL, timeout=20)
            write(key, generated, group=group)
        key.chmod(0o640)
        os.chown(key, 0, group)
        public = subprocess.check_output(['openssl', 'pkey', '-in', str(key), '-pubout', '-outform', 'DER'], stderr=subprocess.DEVNULL, timeout=5)
        record = 'v=DKIM1; k=rsa; p=' + base64.b64encode(public).decode()
        records.append({'domain': domain, 'name': selector + '._domainkey.' + domain, 'type': 'TXT', 'value': record})
        selectors.append(domain + ' ' + selector + '\n')
        paths.append(domain + ' ' + str(key) + '\n')
    write(state / 'public-records.json', json.dumps(records, indent=2), mode=0o644, group=group)
    changed = write(config / 'thura-dkim-selectors.map', ''.join(selectors), mode=0o644)
    changed = write(config / 'thura-dkim-paths.map', ''.join(paths), mode=0o644) or changed
    signing = {'enabled': True, 'try_fallback': False, 'use_esld': False, 'sign_local': True, 'sign_authenticated': True,
               'sign_inbound': False, 'allow_hdrfrom_mismatch': False, 'allow_hdrfrom_mismatch_local': False,
               'allow_hdrfrom_multiple': False, 'allow_username_mismatch': False,
               'selector_map': str(config / 'thura-dkim-selectors.map'), 'path_map': str(config / 'thura-dkim-paths.map')}
    changed = write(config / 'dkim_signing.conf', ucl(signing), mode=0o644) or changed
    reporting = {'enabled': False}
    if report_domain:
        if report_domain not in domains:
            raise ValueError('DMARC report identity must be a configured receiving domain')
        reporting = {'enabled': True, 'email': 'postmaster@' + report_domain,
                     'domain': report_domain, 'org_name': 'Thura', 'helo': hostname,
                     'smtp': '127.0.0.1', 'smtp_port': 2526,
                     'report_local_controller': False, 'retries': 0, 'keys_expire': '7d'}
    dmarc = {'actions': {'reject': 'reject', 'quarantine': 'add header'}, 'reporting': reporting}
    changed = write(config / 'dmarc.conf', ucl(dmarc), mode=0o644) or changed
    headers = {'use': ['authentication-results'], 'skip_local': False, 'skip_authenticated': False,
               'routines': {'authentication-results': {'authserv_id': hostname, 'remove': 0}}}
    changed = write(config / 'milter_headers.conf', ucl(headers), mode=0o644) or changed
    digest = hashlib.sha256(b''.join((config / name).read_bytes() for name in (
        'thura-dkim-selectors.map', 'thura-dkim-paths.map', 'dkim_signing.conf',
        'dmarc.conf', 'milter_headers.conf'))).hexdigest()
    applied = state / 'applied-config'
    if changed or not applied.exists() or applied.read_text() != digest:
        reload_rspamd()
        write(applied, digest, mode=0o600)
    return records


def reports(report_domain, state=None, now=None):
    """One daily attempt, persisted before sending; never silently repeat uncertainty."""
    if not report_domain:
        return
    state = state or STATE
    now = now or datetime.datetime.now(datetime.timezone.utc)
    if now.hour < 1:
        return
    day = (now.date() - datetime.timedelta(days=1)).strftime('%Y%m%d')
    marker = state / 'report-attempt.json'
    if marker.exists() and json.loads(marker.read_text()).get('day') == day:
        return
    status = {'day': day, 'state': 'delivery_uncertain'}
    write(marker, json.dumps(status), mode=0o600)
    try:
        result = subprocess.run(['rspamadm', 'dmarc_report', day], check=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
        output = result.stdout.lower()
        if any(failure in output for failure in (b"couldn't send mail", b'cannot process reports', b'cannot get reports', b'cannot resolve')):
            raise RuntimeError('Report command reported incomplete delivery')
        status['state'] = 'completed'
    except (OSError, RuntimeError, subprocess.SubprocessError):
        print('DMARC report attempt failed or is uncertain; inspect report status before an explicit retry', flush=True)
    write(marker, json.dumps(status), mode=0o600)
