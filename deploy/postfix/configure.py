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


def settings(config, hostname, listen):
    hostname_pattern = r'(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?'
    if not re.fullmatch(hostname_pattern, hostname):
        raise ValueError('MAIL_HOSTNAME must be a DNS hostname')
    if not re.fullmatch(r'(?:127\.0\.0\.1:)?[0-9]{1,5}', listen) or not 1 <= int(listen.split(':')[-1]) <= 65535:
        raise ValueError('SMTP_LISTEN must be a port or 127.0.0.1:port')
    url = urllib.parse.urlsplit(config['baseUrl'])
    local = url.hostname == 'localhost'
    try:
        local = local or ipaddress.ip_address(url.hostname).is_loopback
    except ValueError:
        pass
    if url.username or url.password or url.query or url.fragment or url.path not in ('', '/') or not url.hostname or (url.scheme != 'https' and not (url.scheme == 'http' and local)):
        raise ValueError('baseUrl must be an HTTPS origin, or loopback HTTP')
    mailboxes = config['mailboxes']
    if not isinstance(mailboxes, dict) or not 1 <= len(mailboxes) <= 1000:
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


def main():
    with open(os.environ.get('THURA_RELAY_CONFIG', '/run/secrets/thura-relay.json'), encoding='utf-8') as source:
        config = json.load(source)
    hostname = os.environ['MAIL_HOSTNAME'].lower()
    listen = os.environ.get('SMTP_LISTEN', '127.0.0.1:2525')
    domains = settings(config, hostname, listen)
    private = pathlib.Path('/etc/thura-postfix/private')
    relay_group = pwd.getpwnam('thura-mail').pw_gid
    private.parent.mkdir(mode=0o755, exist_ok=True)
    private.parent.chmod(0o755)
    private.mkdir(mode=0o750, exist_ok=True)
    private.chmod(0o750)
    os.chown(private, 0, relay_group)
    target = private / 'relay.json'
    target.write_text(json.dumps(config), encoding='utf-8')
    os.chown(target, 0, relay_group)
    target.chmod(0o640)
    maps = pathlib.Path('/etc/postfix')
    (maps / 'relay_recipients').write_text(''.join(f'{address} OK\n' for address in config['mailboxes']))
    (maps / 'thura_transport').write_text(''.join(f'{address} thura:\n' for address in config['mailboxes']))
    for name in ('relay_recipients', 'thura_transport'):
        subprocess.run(['postmap', str(maps / name)], check=True)
    options = {
        'myhostname': hostname, 'mydestination': '', 'mynetworks': '127.0.0.1/32',
        'inet_interfaces': 'all', 'inet_protocols': 'ipv4',
        'relay_domains': ', '.join(domains),
        'relay_recipient_maps': 'hash:/etc/postfix/relay_recipients',
        'transport_maps': 'hash:/etc/postfix/thura_transport',
        'smtpd_relay_restrictions': 'permit_mynetworks, reject_unauth_destination',
        'message_size_limit': str(12 * 1024 * 1024),
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
    for line in (maps / 'master.cf').read_text().splitlines(keepends=True):
        fields = line.split()
        if fields and not line.startswith(('#', ' ', '\t')):
            if fields[0] == 'thura' or (len(fields) >= 8 and fields[1] == 'inet' and fields[7] == 'smtpd'):
                continue
        lines.append(line)
    (maps / 'master.cf').write_text(''.join(lines) +
        f'\n{listen} inet n - n - - smtpd\n'
        'thura unix - n n - - pipe flags=R user=thura-mail '
        f'argv=/usr/bin/python3 {pathlib.Path(__file__).with_name("relay.py")} --config /etc/thura-postfix/private/relay.json --recipient ${{recipient}}\n')
    subprocess.run(['postfix', 'check'], check=True)
    os.execvp('postfix', ['postfix', 'start-fg'])


if __name__ == '__main__':
    main()
