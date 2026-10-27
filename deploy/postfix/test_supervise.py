import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import supervise


class CombinedMailTests(unittest.TestCase):
    def test_controller_secret_is_private_and_cannot_inject_config(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'secret.inc'
            for bad in ['', 'short', 'x'*40+'\nsecure_ip = "0.0.0.0/0"']:
                with self.assertRaises(ValueError):supervise.configure_secret(bad,path)
            with mock.patch.object(supervise.pwd,'getpwnam',return_value=mock.Mock(pw_uid=os.getuid(),pw_gid=os.getgid())):
                supervise.configure_secret('private_secret_'+'x'*32,path)
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            self.assertIn('enable_password',path.read_text())
	    
    def test_secret_write_does_not_follow_an_existing_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);target=root/'protected';target.write_text('unchanged')
            path=root/'secret.inc';path.symlink_to(target)
            with mock.patch.object(supervise.pwd,'getpwnam',return_value=mock.Mock(pw_uid=os.getuid(),pw_gid=os.getgid())):
                supervise.configure_secret('private_secret_'+'x'*32,path)
            self.assertEqual(target.read_text(),'unchanged')
            self.assertFalse(path.is_symlink())
            self.assertEqual(path.stat().st_mode&0o777,0o600)

    def test_failure_of_either_service_stops_both_children(self):
        for failed in [0,1]:
            children=[mock.Mock(pid=100),mock.Mock(pid=101)]
            children[failed].poll.return_value=1
            children[1-failed].poll.return_value=None
            with mock.patch.dict(os.environ, {'MAIL_RSPAMD_PASSWORD':'PUBLIC-test-controller-secret-at-least-32'}), \
                 mock.patch.object(supervise.urllib.request,'urlopen') as ready, \
                 mock.patch.object(supervise,'configure_secret'), \
                 mock.patch.object(supervise.subprocess,'Popen',side_effect=children), \
                 mock.patch.object(supervise.Path,'write_text'), \
                 mock.patch.object(supervise.signal,'signal'):
                ready.return_value.__enter__.return_value.status=200
                with self.assertRaises(RuntimeError):supervise.main()
            children[1-failed].terminate.assert_called_once()
            for child in children:child.wait.assert_called_once()


if __name__=='__main__':unittest.main()
