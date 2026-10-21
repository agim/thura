import json
from pathlib import Path
import tempfile
import unittest
from core import configuration


class CoreConfigurationTests(unittest.TestCase):
    def test_local_defaults_do_not_invent_keys_peers_or_service_addresses(self):
        self.assertEqual(configuration(),{
            'ai':{'THURA_AI_ENABLED':'true','LLM_PROVIDER':'ollama'},
            'integrations':{'SMIP_ENABLED':'true'},
            'mail':{'SMIP_MAIL_ENABLED':'true'},
        })

    def test_private_explicit_cloud_choice_is_preserved_and_unknown_values_rejected(self):
        with tempfile.TemporaryDirectory() as root:
            path=Path(root)/'core.json'
            override={'ai':{'LLM_PROVIDER':'anthropic','LLM_MODEL':'operator-model','LLM_API_KEY':'PUBLIC-synthetic-key'}}
            path.write_text(json.dumps(override));path.chmod(0o600)
            self.assertEqual(configuration(path)['ai'],dict(override['ai'],THURA_AI_ENABLED='true'))
            path.chmod(0o644)
            with self.assertRaises(RuntimeError):configuration(path)
            path.chmod(0o600)
            path.write_text(json.dumps({'ai':{'UNRECOGNIZED_SECRET':'x'}}))
            with self.assertRaises(RuntimeError):configuration(path)
