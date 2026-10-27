import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest import mock
import mail_auth


class MailAuthenticationTests(unittest.TestCase):
    def test_keys_and_unique_selector_survive_restart_and_publish_only_public_material(self):
        with tempfile.TemporaryDirectory() as directory, \
             mock.patch.object(mail_auth.pwd, 'getpwnam', return_value=types.SimpleNamespace(pw_gid=os.getgid())), \
             mock.patch.object(mail_auth.os, 'chown'), mock.patch.object(mail_auth.os, 'fchown'), \
             mock.patch.object(mail_auth, 'reload_rspamd') as reload:
            root = Path(directory)
            state, config = root / 'private', root / 'config'
            config.mkdir()
            first = mail_auth.configure(['team.example'], 'mail.team.example', 'team.example', state, config)
            key = next(state.glob('*.key'))
            before = key.read_bytes()
            second = mail_auth.configure(['team.example'], 'mail.team.example', 'team.example', state, config)
            self.assertEqual(first, second)
            self.assertEqual(key.read_bytes(), before)
            self.assertEqual(key.stat().st_mode & 0o777, 0o640)
            self.assertNotIn('PRIVATE KEY', (state / 'public-records.json').read_text())
            self.assertIn('v=DKIM1; k=rsa; p=', first[0]['value'])
            self.assertEqual(reload.call_count, 1)
            self.assertIn('"reject" = "reject"', (config / 'dmarc.conf').read_text())
            self.assertIn('"smtp_port" = 2526', (config / 'dmarc.conf').read_text())
            key.unlink()
            key.symlink_to(root / 'operator-private-key')
            with self.assertRaises(ValueError):
                mail_auth.configure(['team.example'], 'mail.team.example', 'team.example', state, config)

    def test_uncertain_report_attempt_is_not_silently_repeated_after_restart(self):
        with tempfile.TemporaryDirectory() as directory, \
             mock.patch.object(mail_auth.subprocess, 'run', side_effect=subprocess.TimeoutExpired('rspamadm', 120)) as send:
            now = datetime.datetime(2026, 10, 27, 2, tzinfo=datetime.timezone.utc)
            state = Path(directory)
            mail_auth.reports('team.example', state, now)
            mail_auth.reports('team.example', state, now)
            self.assertEqual(send.call_count, 1)
            status = json.loads((state / 'report-attempt.json').read_text())
            self.assertEqual(status, {'day': '20261026', 'state': 'delivery_uncertain'})
            self.assertEqual((state / 'report-attempt.json').stat().st_mode & 0o777, 0o600)

    def test_reports_are_disabled_without_an_operator_identity(self):
        with mock.patch.object(mail_auth.subprocess, 'run') as send:
            mail_auth.reports('')
            send.assert_not_called()
