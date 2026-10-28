"""Validate operator settings, configure Debian Postfix and start its queue."""
import ipaddress
import json
import os
import pathlib
import pwd
import re
import subprocess
import urllib.parse
import uuid


def settings(config, hostname, listen, allow_empty=False):
    hostname_pattern = r'(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?'
    if not re.fullmatch(hostname_pattern, hostname):
        raise ValueError('MAIL_HOSTNAME must be a DNS hostname')
    if not re.fullmatch(r'(?:127\.0\.0\.1:)?[0-9]{1,5}', listen) or not 1 <= int(listen.split(':')[-1]) <= 65535:
        raise ValueError('SMTP_LISTEN must be a port or 127.0.0.1:port')
    if int(listen.split(':')[-1]) == 2525 and listen != '127.0.0.1:2525':
        raise ValueError('Port 2525 is reserved for loopback submission')
    url = urllib.parse.urlsplit(config['baseUrl'])
    local = url.hostname == 'localhost'
    try:
        local = local or ipaddress.ip_address(url.hostname).is_loopback
    except ValueError:
        pass
    if url.username or url.password or url.query or url.fragment or url.path not in ('', '/') or not url.hostname or (url.scheme != 'https' and not (url.scheme == 'http' and local)):
        raise ValueError('baseUrl must be an HTTPS origin, or loopback HTTP')
    mailboxes = config['mailboxes']
    if not isinstance(mailboxes, dict) or not (0 if allow_empty else 1) <= len(mailboxes) <= 1000:
        raise ValueError('Configure 1–1000 mailbox addresses')
    domains = set()
    for address, mailbox in mailboxes.items():
        localpart, separator, domain = address.partition('@')
        if address != address.lower() or not separator or not re.fullmatch(r'[a-z0-9][a-z0-9._+-]{0,63}', localpart) or not re.fullmatch(hostname_pattern, domain):
            raise ValueError('Mailbox addresses must be canonical lowercase addresses')
        if str(uuid.UUID(mailbox['id'])) != mailbox['id'] or not isinstance(mailbox['secret'], str) or not 32 <= len(mailbox['secret']) <= 4096:
            raise ValueError('Mailbox IDs and private inbound secrets are required')
        domains.add(domain)
    return sorted(domains)


def recipient_history(config, path):
    """Keep removed recipients on the local pipe so queued mail cannot leak to MX."""
    addresses = set(config['mailboxes'])
    if path.exists():
        if path.is_symlink() or path.stat().st_size > 4 * 1024 * 1024:
            raise ValueError('invalid managed recipient history')
        saved = json.loads(path.read_text())
        if not isinstance(saved, list) or not all(isinstance(item, str) for item in saved):
            raise ValueError('invalid managed recipient history')
        addresses.update(saved)
    if len(addresses) > 10000:
        raise ValueError('managed recipient history limit reached; inspect queued mail before maintenance')
    for address in addresses:
        if len(address) > 318 or not re.fullmatch(r'[a-z0-9][a-z0-9._+\-]{0,63}@[a-z0-9][a-z0-9.\-]*\.[a-z0-9\-]+', address):
            raise ValueError('invalid managed recipient history address')
    staged = path.with_suffix('.next')
    fd = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(sorted(addresses), stream)
    staged.chmod(0o600)
    staged.replace(path)
    return sorted(addresses)


def pipe_service():
    """The local delivery transport. The SMTP envelope (client IP, HELO name and
    envelope sender) lets Thura's Rspamd scan evaluate SPF; null_sender= keeps a
    bounce's empty envelope sender empty instead of MAILER-DAEMON."""
    return ('thura unix - n n - - pipe flags=R null_sender= user=thura-mail '
            f'argv=/usr/bin/python3 {pathlib.Path(__file__).with_name("relay.py")} --config /etc/thura-postfix/private/relay.json --recipient ${{recipient}} '
            '--client-address=${client_address} --client-helo=${client_helo} --sender=${sender}\n')


def configure(config, hostname, listen, managed=False):
    domains = settings(config, hostname, listen, allow_empty=managed)
    transport_addresses = recipient_history(config, pathlib.Path('/var/spool/postfix/.thura-recipient-history.json')) if managed else config['mailboxes']
    private = pathlib.Path('/etc/thura-postfix/private')
    relay_group = pwd.getpwnam('thura-mail').pw_gid
    private.parent.mkdir(mode=0o755, exist_ok=True)
    private.parent.chmod(0o755)
    private.mkdir(mode=0o750, exist_ok=True)
    private.chmod(0o750)
    os.chown(private, 0, relay_group)
    target = private / 'relay.json'
    staged = target.with_suffix('.next')
    # Create privately before writing, including while replacing an old file.
    fd = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        json.dump(config, stream)
    os.chown(staged, 0, relay_group)
    staged.chmod(0o640)
    staged.replace(target)
    maps = pathlib.Path('/etc/postfix')
    contents = {
        # Publish local transports before newly accepted recipients.
        'thura_transport': ''.join(f'{address} thura:\n' for address in transport_addresses),
        'relay_recipients': ''.join(f'{address} OK\n' for address in config['mailboxes']),
    }
    for name, content in contents.items():
        staged_map = maps / (name + '.next')
        staged_map.write_text(content)
        subprocess.run(['postmap', str(staged_map)], check=True)
        (maps / (name + '.next.db')).replace(maps / (name + '.db'))
        staged_map.replace(maps / name)
    options = {
        'myhostname': hostname, 'mydestination': '', 'mynetworks': '127.0.0.1/32',
        'inet_interfaces': 'all', 'inet_protocols': 'ipv4',
        'relay_domains': ', '.join(domains),
        'relay_recipient_maps': 'hash:/etc/postfix/relay_recipients',
        'transport_maps': 'hash:/etc/postfix/thura_transport',
        'recipient_delimiter': '',
        'smtpd_relay_restrictions': 'permit_mynetworks, reject_unauth_destination',
        # Base64/MIME overhead must fit when Thura sends a valid 10 MiB file.
        # Public inbound SMTP overrides this to the existing 12 MiB boundary.
        'message_size_limit': str(16 * 1024 * 1024),
        'thura_destination_recipient_limit': '1',
        'smtp_tls_security_level': 'may', 'smtpd_tls_security_level': 'may',
        'maillog_file': '/dev/stdout',
    }
    # Optional test/private upstream relay; public delivery uses DNS/MX by default.
    relayhost = os.environ.get('POSTFIX_RELAYHOST', '')
    if relayhost:
        if not re.fullmatch(r'\[(?:[a-z0-9.-]+)\]:[0-9]{1,5}', relayhost):
            raise ValueError('POSTFIX_RELAYHOST must be [hostname]:port')
        options['relayhost'] = relayhost
    cert = pathlib.Path('/run/secrets/smtp-cert.pem')
    key = pathlib.Path('/run/secrets/smtp-key.pem')
    if cert.exists() != key.exists():
        raise ValueError('Mount both the SMTP TLS certificate and key')
    if cert.exists():
        options.update(smtpd_tls_cert_file=str(cert), smtpd_tls_key_file=str(key))
    for name, value in options.items():
        subprocess.run(['postconf', '-e', f'{name} = {value}'], check=True)
    # Retain Debian's queue/proxymap/TLS/log services. Replacing the entire
    # master.cf would lose services used implicitly by distribution defaults.
    lines = []
    skip_continuation = False
    for line in (maps / 'master.cf').read_text().splitlines(keepends=True):
        fields = line.split()
        if line.startswith((' ', '\t')) and skip_continuation:
            continue
        if fields and not line.startswith(('#', ' ', '\t')):
            skip_continuation = False
            if fields[0] == 'thura' or (len(fields) >= 8 and fields[1] == 'inet' and fields[7] == 'smtpd'):
                skip_continuation = True
                continue
        lines.append(line)
    smtp = '127.0.0.1:2525 inet n - n - - smtpd\n  -o message_size_limit=16777216\n'
    if listen != '127.0.0.1:2525':
        smtp += f'{listen} inet n - n - - smtpd\n  -o message_size_limit=12582912\n  -o smtpd_relay_restrictions=reject_unauth_destination\n'
    (maps / 'master.cf').write_text(''.join(lines) + '\n' + smtp + pipe_service())
    subprocess.run(['postfix', 'check'], check=True)


def main():
    if os.environ.get('THURA_POSTFIX_MANAGED') == 'true':
        import managed
        managed.main()
        return
    with open(os.environ.get('THURA_RELAY_CONFIG', '/run/secrets/thura-relay.json'), encoding='utf-8') as source:
        config = json.load(source)
    configure(config, os.environ['MAIL_HOSTNAME'].lower(), os.environ.get('SMTP_LISTEN', '127.0.0.1:2525'))
    os.execvp('postfix', ['postfix', 'start-fg'])


if __name__ == '__main__':
    main()
