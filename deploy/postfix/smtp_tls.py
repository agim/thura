"""Validate operator/deployment certificates; never advertise unusable STARTTLS."""
import os
import hashlib
import pathlib
import ssl
import subprocess

SECRETS = pathlib.Path('/run/secrets')
PERSISTENT = pathlib.Path('/var/spool/postfix/etc/thura-tls')


def configuration(hostname, directories=None):
    options = {'smtpd_tls_security_level': 'none', 'smtpd_tls_cert_file': '',
               'smtpd_tls_key_file': '', 'smtpd_tls_chain_files': ''}
    deployed = os.environ.get('DEPLOY_PUBLIC_TLS_BUNDLE_FILE', '') if directories is None else ''
    if deployed:
        # Resolve current once: certificate and key must come from one generation.
        bundle = pathlib.Path(deployed).resolve()
        hostname = os.environ.get('DEPLOY_PUBLIC_TLS_HOSTNAME', '')
        if not hostname:
            raise ValueError('Deployment certificate hostname is missing')
        if not bundle.exists():
            return options, 'certificate-pending'
        pairs = [(bundle, bundle)]
    else:
        pairs = [(directory / 'smtp-cert.pem', directory / 'smtp-key.pem')
                 for directory in (directories if directories is not None else (SECRETS, PERSISTENT))]
    for cert, key in pairs:
        if not cert.exists() and not key.exists():
            continue
        if not cert.is_file() or not key.is_file():
            raise ValueError('Mount both SMTP TLS certificate and private key')
        if max(cert.stat().st_size, key.stat().st_size) > 1024 * 1024:
            raise ValueError('SMTP TLS material exceeds its size limit')
        if key.stat().st_mode & 0o077:
            raise ValueError('SMTP TLS private key must be owner-only')
        try:
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(str(cert), str(key))
            for arguments in (['-checkhost', hostname], ['-checkend', '0']):
                subprocess.run(['openssl', 'x509', '-in', str(cert), '-noout', *arguments],
                               check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)
        except (OSError, ssl.SSLError, subprocess.SubprocessError) as error:
            raise ValueError('SMTP TLS certificate is invalid, expired, mismatched or for another hostname') from error
        options.update(smtpd_tls_security_level='may', smtpd_tls_cert_file=str(cert), smtpd_tls_key_file=str(key))
        digest = hashlib.sha256(cert.read_bytes() + key.read_bytes()).hexdigest()
        return options, digest
    return options, 'no-certificate'
