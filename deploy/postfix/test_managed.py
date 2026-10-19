import copy
import tempfile
from pathlib import Path
import unittest
from unittest import mock
import configure
import managed
import relay


class ManagedTests(unittest.TestCase):
    def payload(self):
        return {'enabled': True, 'revision': 1, 'hostname': 'mail.team.example',
                'listen': '25', 'config': {'baseUrl': 'http://127.0.0.1:3000',
                'mailboxes': {'team@team.example': {'id': 'a0000000-0000-4000-8000-000000000010',
                'secret': 'PUBLIC-inbound-signing-secret-for-tests'}}}}

    def test_bootstrap_is_outgoing_only_and_has_no_receiving_mapping(self):
        payload = self.payload()
        payload['listen'] = '127.0.0.1:2525'
        payload['config']['mailboxes'] = {}
        self.assertEqual(managed.validate(payload), payload)
        with self.assertRaises(ValueError):
            configure.settings(payload['config'], payload['hostname'], payload['listen'])
        payload['listen'] = '25'
        with self.assertRaises(ValueError):
            managed.validate(payload)

    def test_sidecar_restricts_origin_ports_and_mappings(self):
        for section, name, value in (
            ('config', 'baseUrl', 'https://outside.example'),
            ('root', 'listen', '2525'), ('root', 'hostname', 'mail.example\nrelay_domains=*'),
            ('root', 'enabled', 'true'),
            ('config', 'mailboxes', {'*@team.example': {'id': 'x', 'secret': 'x'}}),
        ):
            payload = self.payload()
            (payload if section == 'root' else payload[section])[name] = value
            with self.assertRaises(ValueError):
                managed.validate(payload)

    def test_removed_mapping_defers_previously_queued_mail(self):
        config = {'managed': True, 'mailboxes': {}}
        self.assertEqual(relay.deliver(config, 'deleted@team.example', b'queued'), 75)

    def test_removed_recipient_transport_history_survives_restart_without_secrets(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'history.json'
            config = self.payload()['config']
            self.assertEqual(configure.recipient_history(config, path), ['team@team.example'])
            self.assertNotIn('PUBLIC', path.read_text())
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            config['mailboxes'] = {}
            self.assertEqual(configure.recipient_history(config, path), ['team@team.example'])
            self.assertEqual(relay.deliver(dict(config, managed=True), 'team@team.example', b'queued'), 75)
            path.write_text('["bad\\naddress@example.org"]')
            with self.assertRaises(ValueError):
                configure.recipient_history(config, path)

    def test_reload_recovers_after_failed_read_without_restarting_postfix(self):
        first = self.payload()
        changed = copy.deepcopy(first)
        changed['revision'] = 2
        changed['config']['mailboxes']['second@team.example'] = changed['config']['mailboxes']['team@team.example']
        process = mock.Mock()
        process.poll.return_value = None
        signal_handlers = {}
        reads = iter([first, OSError('unavailable'), first, changed])

        def fetch(_token):
            try:
                value = next(reads)
            except StopIteration:
                signal_handlers[managed.signal.SIGTERM](None, None)
                return changed
            if isinstance(value, Exception):
                raise value
            return value

        with mock.patch.dict(managed.os.environ, {'THURA_POSTFIX_TOKEN': 'PUBLIC-private-sidecar-token-at-least-32'}), \
             mock.patch.object(managed, 'fetch', side_effect=fetch), \
             mock.patch.object(managed.signal, 'signal', side_effect=lambda sig, fn: signal_handlers.update({sig: fn})), \
             mock.patch.object(managed.time, 'sleep'), \
             mock.patch.object(managed.configure, 'configure') as apply, \
             mock.patch.object(managed.subprocess, 'Popen', return_value=process) as start, \
             mock.patch.object(managed.subprocess, 'run') as run:
            managed.main()
        self.assertEqual(start.call_count, 1)
        self.assertEqual(apply.call_count, 2)
        self.assertEqual(sum(call.args[0] == ['postfix', 'reload'] for call in run.call_args_list), 1)
        self.assertEqual(sum(call.args[0] == ['postfix', 'stop'] for call in run.call_args_list), 1)
        process.wait.assert_called_once()
