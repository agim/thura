import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import configure
import managed


MAILBOX = {'id': 'a0000000-0000-4000-8000-000000000010', 'secret': 'PUBLIC-inbound-signing-secret-for-tests'}


def platform_mount(root, generation='g1'):
    """Mirror the deploy platform's layout: current -> .generation-*/{fullchain,privkey}.pem."""
    directory = root / ('.generation-' + generation)
    directory.mkdir()
    (directory / 'fullchain.pem').write_text('CERT ' + generation)
    (directory / 'privkey.pem').write_text('KEY ' + generation)
    staged = root / '.current-next'
    staged.symlink_to(directory.name)
    staged.replace(root / 'current')
    return {'DEPLOY_PUBLIC_TLS_HOSTNAME': 'mail.team.example',
            'DEPLOY_PUBLIC_TLS_CERT_FILE': str(root / 'current' / 'fullchain.pem'),
            'DEPLOY_PUBLIC_TLS_KEY_FILE': str(root / 'current' / 'privkey.pem')}


class TLSFileTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.legacy = (self.root / 'none-cert.pem', self.root / 'none-key.pem')

    def tearDown(self):
        self.directory.cleanup()

    def test_platform_certificate_enables_hardened_starttls(self):
        environ = platform_mount(self.root)
        files = configure.tls_files(environ, self.legacy)
        self.assertEqual(files, (Path(environ['DEPLOY_PUBLIC_TLS_CERT_FILE']), Path(environ['DEPLOY_PUBLIC_TLS_KEY_FILE'])))
        options = configure.tls_options(files)
        self.assertEqual(options['smtpd_tls_security_level'], 'may')
        # The stable `current` path is configured, so a reload picks up renewals.
        self.assertEqual(options['smtpd_tls_cert_file'], environ['DEPLOY_PUBLIC_TLS_CERT_FILE'])
        self.assertEqual(options['smtpd_tls_key_file'], environ['DEPLOY_PUBLIC_TLS_KEY_FILE'])
        for name in ('smtpd_tls_protocols', 'smtpd_tls_mandatory_protocols'):
            for protocol in ('!SSLv2', '!SSLv3', '!TLSv1', '!TLSv1.1'):
                self.assertIn(protocol, options[name].split(', '))
        self.assertEqual(options['smtpd_tls_loglevel'], '1')

    def test_missing_certificate_never_offers_starttls(self):
        environ = {'DEPLOY_PUBLIC_TLS_CERT_FILE': str(self.root / 'current' / 'fullchain.pem'),
                   'DEPLOY_PUBLIC_TLS_KEY_FILE': str(self.root / 'current' / 'privkey.pem')}
        for case in (environ, {}):
            files = configure.tls_files(case, self.legacy)
            self.assertIsNone(files)
            self.assertIsNone(configure.tls_state(files))
            self.assertEqual(configure.tls_options(files), {'smtpd_tls_security_level': 'none', 'smtpd_tls_cert_file': '', 'smtpd_tls_key_file': ''})

    def test_unreadable_platform_key_disables_starttls(self):
        environ = platform_mount(self.root)
        real_access = os.access
        with mock.patch.object(configure.os, 'access', side_effect=lambda path, mode: not str(path).endswith('privkey.pem') and real_access(path, mode)):
            self.assertIsNone(configure.tls_files(environ, self.legacy))

    def test_standalone_secrets_remain_supported_and_must_be_paired(self):
        cert, key = self.legacy
        cert.write_text('CERT')
        with self.assertRaises(ValueError):
            configure.tls_files({}, self.legacy)
        key.write_text('KEY')
        self.assertEqual(configure.tls_files({}, self.legacy), self.legacy)

    def test_renewal_changes_the_served_state(self):
        environ = platform_mount(self.root, 'g1')
        first = configure.tls_state(configure.tls_files(environ, self.legacy))
        self.assertEqual(first, configure.tls_state(configure.tls_files(environ, self.legacy)))
        platform_mount(self.root, 'g2')
        renewed = configure.tls_state(configure.tls_files(environ, self.legacy))
        self.assertNotEqual(first, renewed)
        self.assertIn('.generation-g2', renewed[0][0])


class NoticeRecipientTests(unittest.TestCase):
    def test_postmaster_notices_stay_local(self):
        self.assertEqual(configure.notice_recipient('mail.team.example', {'postmaster@mail.team.example': MAILBOX, 'postmaster@team.example': MAILBOX}), 'postmaster@mail.team.example')
        self.assertEqual(configure.notice_recipient('mail.team.example', {'team@team.example': MAILBOX, 'postmaster@team.example': MAILBOX}), 'postmaster@team.example')
        # Never the bare `postmaster`, which resolves through the host's own public MX.
        self.assertEqual(configure.notice_recipient('mail.team.example', {'team@team.example': MAILBOX}), configure.NOTICE_SINK)
        self.assertEqual(configure.notice_recipient('mail.team.example', {}), configure.NOTICE_SINK)


class ConfigureTests(unittest.TestCase):
    """Run configure() against a scratch /etc and record what reaches postconf."""

    def run_configure(self, environ, mailboxes):
        with tempfile.TemporaryDirectory() as directory:
            etc = Path(directory) / 'etc'
            (etc / 'postfix').mkdir(parents=True)
            (etc / 'postfix' / 'master.cf').write_text('smtp inet n - y - - smtpd\ndiscard unix - - n - - discard\n')
            postconf = {}

            def run(argv, check):
                if argv[0] == 'postmap':
                    Path(argv[1] + '.db').write_text('')
                elif argv[:2] == ['postconf', '-e']:
                    name, _, value = argv[2].partition(' = ')
                    postconf[name] = value

            config = {'baseUrl': 'http://127.0.0.1:3000', 'mailboxes': mailboxes}
            with mock.patch.object(configure, 'ETC', etc), \
                 mock.patch.object(configure, 'LEGACY_CERT', Path(directory) / 'none-cert'), \
                 mock.patch.dict(configure.os.environ, environ, clear=True), \
                 mock.patch.object(configure.pwd, 'getpwnam', return_value=mock.Mock(pw_gid=os.getgid())), \
                 mock.patch.object(configure.os, 'chown'), \
                 mock.patch.object(configure.subprocess, 'run', side_effect=run):
                state = configure.configure(config, 'mail.team.example', '25')
            transport = (etc / 'postfix' / 'thura_transport').read_text()
        return postconf, state, transport

    def test_configuration_with_platform_certificate(self):
        with tempfile.TemporaryDirectory() as directory:
            environ = platform_mount(Path(directory))
            postconf, state, _ = self.run_configure(environ, {'team@team.example': MAILBOX, 'postmaster@team.example': MAILBOX})
            self.assertIsNotNone(state)
        self.assertEqual(postconf['smtpd_tls_security_level'], 'may')
        self.assertEqual(postconf['smtpd_tls_cert_file'], environ['DEPLOY_PUBLIC_TLS_CERT_FILE'])
        self.assertEqual(postconf['smtpd_tls_key_file'], environ['DEPLOY_PUBLIC_TLS_KEY_FILE'])
        self.assertEqual(postconf['smtpd_tls_loglevel'], '1')
        self.assertEqual(postconf['smtpd_tls_mandatory_protocols'], '!SSLv2, !SSLv3, !TLSv1, !TLSv1.1')
        self.assertEqual(postconf['smtp_tls_security_level'], 'may')
        for name in ('bounce_notice_recipient', '2bounce_notice_recipient', 'delay_notice_recipient', 'error_notice_recipient'):
            self.assertEqual(postconf[name], 'postmaster@team.example')

    def test_configuration_without_certificate(self):
        postconf, state, transport = self.run_configure({}, {'team@team.example': MAILBOX})
        self.assertIsNone(state)
        self.assertEqual(postconf['smtpd_tls_security_level'], 'none')
        self.assertEqual(postconf['smtpd_tls_cert_file'], '')
        self.assertEqual(postconf['smtpd_tls_key_file'], '')
        self.assertEqual(postconf['2bounce_notice_recipient'], configure.NOTICE_SINK)
        self.assertIn(configure.NOTICE_SINK + ' discard:\n', transport)
        self.assertIn('team@team.example thura:\n', transport)


class ReloadTests(unittest.TestCase):
    def test_certificate_issue_and_renewal_reload_postfix(self):
        payload = {'enabled': True, 'revision': 1, 'hostname': 'mail.team.example', 'listen': '25',
                   'config': {'baseUrl': 'http://127.0.0.1:3000', 'mailboxes': {'team@team.example': MAILBOX}}}
        process = mock.Mock()
        process.poll.return_value = None
        handlers = {}
        # Loop 1: started without certificate. 2: unchanged. 3: issued. 4: unchanged. 5: renewed.
        states = iter([None, None, ('g1',), ('g1',), ('g2',)])

        def tls_state(_files):
            try:
                return next(states)
            except StopIteration:
                handlers[managed.signal.SIGTERM](None, None)
                return ('g2',)

        with mock.patch.dict(managed.os.environ, {'THURA_POSTFIX_TOKEN': 'PUBLIC-private-sidecar-token-at-least-32'}), \
             mock.patch.object(managed, 'fetch', return_value=payload), \
             mock.patch.object(managed.signal, 'signal', side_effect=lambda sig, fn: handlers.update({sig: fn})), \
             mock.patch.object(managed.time, 'sleep'), \
             mock.patch.object(managed.configure, 'configure', return_value=None) as apply, \
             mock.patch.object(managed.configure, 'tls_files', return_value=('cert', 'key')), \
             mock.patch.object(managed.configure, 'tls_state', side_effect=tls_state), \
             mock.patch.object(managed.configure, 'apply_tls') as apply_tls, \
             mock.patch.object(managed.subprocess, 'Popen', return_value=process), \
             mock.patch.object(managed.subprocess, 'run') as run:
            managed.main()
        self.assertEqual(apply.call_count, 1)
        self.assertEqual(apply_tls.call_count, 2)
        self.assertEqual(sum(call.args[0] == ['postfix', 'reload'] for call in run.call_args_list), 2)


if __name__ == '__main__':
    unittest.main()
