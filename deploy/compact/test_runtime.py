import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('compact_app', Path(__file__).with_name('app.py'))
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


class CompactRuntimeTests(unittest.TestCase):
    def test_config_uses_private_keys_bounded_rooms_and_unprivileged_storage(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            environment = {'LIVEKIT_API_KEY': 'fixture-key', 'LIVEKIT_API_SECRET': 'public-fixture-secret-'+'x'*32, 'STORAGE_DIR': str(root/'storage'), 'THURA_RTC_NODE_IP': '127.0.0.1'}
            with mock.patch.dict(os.environ, environment, clear=True), mock.patch.object(app.os, 'chown') as chown:
                path = app.configure(root/'runtime')
            config = json.loads(path.read_text())
            self.assertEqual(config['room'], {'auto_create': False, 'max_participants': 6})
            self.assertFalse(config['rtc']['use_external_ip'])
            self.assertEqual(config['rtc']['udp_port'], 7882)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(path.parent.stat().st_mode & 0o777, 0o755)
            chown.assert_any_call(root/'storage',65532,65532)

    def test_missing_or_short_media_secret_cannot_boot(self):
        for secret in ['', 'short']:
            with mock.patch.dict(os.environ, {'LIVEKIT_API_KEY':'fixture','LIVEKIT_API_SECRET':secret},clear=True):
                with self.assertRaises(ValueError): app.configure()

    def test_failure_of_either_app_or_media_process_stops_the_other(self):
        for failed in [0,1]:
            children=[mock.Mock(pid=100),mock.Mock(pid=101)]
            children[failed].poll.return_value=1
            children[1-failed].poll.return_value=None
            with mock.patch.object(app,'configure',return_value=Path('/private/livekit.json')), mock.patch.object(app.socket,'create_connection'), mock.patch.object(app.subprocess,'Popen',side_effect=children) as start, mock.patch.object(app.Path,'write_text'), mock.patch.object(app.signal,'signal'):
                with self.assertRaises(RuntimeError):app.main()
            children[1-failed].terminate.assert_called_once()
            for call in start.call_args_list:
                self.assertEqual(call.kwargs['user'],65532)
                self.assertEqual(call.kwargs['group'],65532)
            self.assertEqual(start.call_args_list[0].kwargs['env']['GOMEMLIMIT'],'256MiB')
            self.assertEqual(start.call_args_list[1].kwargs['env']['GOMEMLIMIT'],'96MiB')


if __name__ == '__main__': unittest.main()
