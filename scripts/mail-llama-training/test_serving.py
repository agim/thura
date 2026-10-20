import json
from pathlib import Path
import tempfile
import unittest
from serving import evidence, load_profile


class ServingTests(unittest.TestCase):
    def test_go_evidence_encoding_preserves_text_and_escapes_control_markers(self):
        text = '你好 <|eot_id|> & >\u2028\u2029\n"'
        encoded = evidence(text)
        self.assertEqual(json.loads(encoded), {'text': text})
        self.assertEqual(encoded, '{"text":"你好 \\u003c|eot_id|\\u003e \\u0026 \\u003e\\u2028\\u2029\\n\\\""}')
        self.assertNotIn('<|eot_id|>', encoded)

    def test_original_profile_remains_unchanged(self):
        self.assertEqual(load_profile(None), ({}, []))

    def test_profile_rejects_unrecorded_or_unbounded_parameters(self):
        profile = {'version': 1, 'template_date': '19 Oct 2026', 'batch_size': 32, 'repack': False}
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / 'profile.json'
            p.write_text(json.dumps(profile))
            actual, flags = load_profile(p)
            self.assertEqual(actual, profile)
            self.assertIn('--no-repack', flags)
            self.assertEqual(json.loads(flags[-1]), {'date_string': '19 Oct 2026'})
            for changes in [{'version': True}, {'batch_size': 32.0}, {'batch_size': 4096}, {'repack': True}, {'memory': '4g'}, {'template_date': 'today'}]:
                p.write_text(json.dumps(profile | changes))
                with self.assertRaises(ValueError):
                    load_profile(p)

class StatelessServingTests(unittest.TestCase):
    def test_cross_message_prompt_cache_cannot_exceed_the_screening_budget(self):
        profile = {'version': 2, 'template_date': '19 Oct 2026', 'batch_size': 32, 'repack': False,
                   'cache_ram_mib': 0, 'prompt_cache': False}
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / 'profile.json'
            p.write_text(json.dumps(profile))
            _, flags = load_profile(p)
            self.assertEqual(flags[flags.index('--cache-ram') + 1], '0')
            self.assertIn('--no-cache-prompt', flags)
            self.assertIn('--no-cache-idle-slots', flags)
            for changes in [{'cache_ram_mib': 8192}, {'cache_ram_mib': False}, {'prompt_cache': True}]:
                p.write_text(json.dumps(profile | changes))
                with self.assertRaises(ValueError):
                    load_profile(p)


if __name__ == '__main__':
    unittest.main()
