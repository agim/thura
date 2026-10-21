import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('matrix_runtime', Path(__file__).with_name('matrix.py'))
matrix = importlib.util.module_from_spec(spec)
spec.loader.exec_module(matrix)

class MatrixRegistrationTests(unittest.TestCase):
    def test_stable_identity_private_namespace_no_public_signup_or_callback(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            env = {'MATRIX_SERVER_NAME':'chat.example.com','MATRIX_AS_TOKEN':'a'*48,'MATRIX_HS_TOKEN':'b'*48}
            with mock.patch.dict(os.environ,env,clear=True),mock.patch.object(matrix.os,'chown'):
                path=matrix.configure(root/'run',root/'state')
                initial=path.read_bytes()
                matrix.configure(root/'run',root/'state')
                self.assertEqual(initial,path.read_bytes())
                config=json.loads(initial)
                registration=json.loads((root/'run/matrix-registration.json').read_text())
                self.assertIsNone(registration['url'])
                self.assertFalse(config['enable_registration'])
                self.assertFalse(config['allow_guest_access'])
                self.assertEqual(config['listeners'][0]['bind_addresses'],['127.0.0.1'])
                self.assertEqual(registration['sender_localpart'],'thura_bot')
                self.assertEqual(path.stat().st_mode & 0o777,0o600)
                os.environ['MATRIX_SERVER_NAME']='other.example.com'
                with self.assertRaises(ValueError):matrix.configure(root/'run',root/'state')

if __name__=='__main__':unittest.main()
