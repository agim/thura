import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import smtp_tls


class TLSConfigurationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def certificate(self, directory, hostname='mail.team.example'):
        directory.mkdir(exist_ok=True)
        cert, key = directory / 'smtp-cert.pem', directory / 'smtp-key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                        '-keyout', str(key), '-out', str(cert), '-days', '2',
                        '-subj', '/CN=' + hostname], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        key.chmod(0o600)
        return cert, key

    def test_no_certificate_explicitly_disables_tls_and_clears_stale_paths(self):
        options, digest = smtp_tls.configuration('mail.team.example', [self.root])
        self.assertEqual(options['smtpd_tls_security_level'], 'none')
        self.assertEqual(options['smtpd_tls_cert_file'], '')
        self.assertEqual(options['smtpd_tls_key_file'], '')
        self.assertEqual(options['smtpd_tls_chain_files'], '')
        self.assertEqual(digest, 'no-certificate')

    def test_valid_pair_enables_tls_and_renewal_changes_digest(self):
        cert, _ = self.certificate(self.root)
        options, old = smtp_tls.configuration('mail.team.example', [self.root])
        self.assertEqual(options['smtpd_tls_security_level'], 'may')
        self.assertEqual(options['smtpd_tls_cert_file'], str(cert))
        self.certificate(self.root)
        _, new = smtp_tls.configuration('mail.team.example', [self.root])
        self.assertNotEqual(old, new)

    def test_wrong_hostname_and_mismatched_keys_are_rejected(self):
        cert, key = self.certificate(self.root)
        with self.assertRaises(ValueError):
            smtp_tls.configuration('other.example', [self.root])
        _, other = self.certificate(self.root / 'other')
        key.write_bytes(other.read_bytes())
        with self.assertRaises(ValueError):
            smtp_tls.configuration('mail.team.example', [self.root])

    def test_incomplete_or_world_readable_private_key_is_rejected(self):
        cert, key = self.certificate(self.root)
        key.chmod(0o644)
        with self.assertRaises(ValueError):
            smtp_tls.configuration('mail.team.example', [self.root])
        key.chmod(0o600)
        cert.unlink()
        with self.assertRaises(ValueError):
            smtp_tls.configuration('mail.team.example', [self.root])

    def test_expired_certificate_and_persistent_volume_fallback(self):
        secrets = self.root / 'secrets'
        secrets.mkdir()
        persistent = self.root / 'persistent'
        self.certificate(persistent)
        options, _ = smtp_tls.configuration('mail.team.example', [secrets, persistent])
        self.assertEqual(options['smtpd_tls_cert_file'], str(persistent / 'smtp-cert.pem'))
        with mock.patch.object(smtp_tls.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'openssl')):
            with self.assertRaises(ValueError):
                smtp_tls.configuration('mail.team.example', [persistent])

    def test_deploy_bundle_uses_one_generation_and_public_hostname(self):
        from unittest.mock import patch
        generation = self.root / 'generation-one'
        cert, key = self.certificate(generation)
        bundle = generation / 'bundle.pem'
        bundle.write_bytes(cert.read_bytes() + key.read_bytes())
        bundle.chmod(0o400)
        current = self.root / 'current'
        current.symlink_to(generation, target_is_directory=True)
        with patch.dict(os.environ, {'DEPLOY_PUBLIC_TLS_BUNDLE_FILE': str(current / 'bundle.pem'), 'DEPLOY_PUBLIC_TLS_HOSTNAME': 'mail.team.example'}):
            options, first = smtp_tls.configuration('different-internal-host.example')
            self.assertEqual(options['smtpd_tls_cert_file'], str(bundle.resolve()))
            self.assertEqual(options['smtpd_tls_key_file'], str(bundle.resolve()))
            self.assertEqual(options['smtpd_tls_security_level'], 'may')
            second_generation = self.root / 'generation-two'
            cert2, key2 = self.certificate(second_generation)
            second = second_generation / 'bundle.pem'
            second.write_bytes(cert2.read_bytes() + key2.read_bytes())
            second.chmod(0o400)
            current.unlink(); current.symlink_to(second_generation, target_is_directory=True)
            renewed, digest = smtp_tls.configuration('different-internal-host.example')
            self.assertNotEqual(first, digest)
            self.assertEqual(renewed['smtpd_tls_cert_file'], str(second.resolve()))
        with patch.dict(os.environ, {'DEPLOY_PUBLIC_TLS_BUNDLE_FILE': str(self.root / 'pending.pem'), 'DEPLOY_PUBLIC_TLS_HOSTNAME': 'mail.team.example'}):
            pending, _ = smtp_tls.configuration('different-internal-host.example')
            self.assertEqual(pending['smtpd_tls_security_level'], 'none')
