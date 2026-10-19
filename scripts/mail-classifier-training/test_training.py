from email.message import EmailMessage
import unittest

import common
from prepare import AMBIGUOUS, PROMOTION, split, source_row
from train import calibrated
from benchmark import regression_acceptance


class TrainingTests(unittest.TestCase):
    def test_spam_and_phishing_remain_distinct_and_errors_count(self):
        result = common.metrics([
            {'label': 'legitimate', 'prediction': 'spam'},
            {'label': 'legitimate', 'prediction': 'phishing'},
            {'label': 'phishing', 'prediction': 'spam'},
            {'label': 'phishing', 'prediction': 'phishing'},
            {'label': 'spam', 'prediction': None},
        ])
        self.assertEqual(result['legitimate_false_positive_rate'], 1)
        self.assertEqual(result['phishing_recall'], .5)
        self.assertEqual(result['spam_recall'], 0)
        self.assertEqual(result['errors'], 1)
        self.assertFalse(common.acceptance(result))

    def test_html_mislabeled_as_plain_is_read_without_scripts_and_with_destinations(self):
        message = EmailMessage()
        message['Subject'] = 'Notice'
        message.set_content('<html><div>Hello customer this is the visible text of your notice.</div><p>Read the document</p><a href="https://example.invalid/login">Open</a><script>secret_script()</script></html>')
        result = common.email_text(message)
        self.assertIn('visible text', result)
        self.assertIn('https://example.invalid/login', result)
        self.assertNotIn('<div>', result)
        self.assertNotIn('secret_script', result)

    def test_archive_spam_selection_excludes_impersonation_and_credential_requests(self):
        self.assertTrue(PROMOTION.search('We offer SEO and web design services'))
        self.assertFalse(AMBIGUOUS.search('We offer SEO and web design services'))
        self.assertTrue(AMBIGUOUS.search('Enter your password for an exclusive discount'))
        self.assertTrue(AMBIGUOUS.search('Your Costco purchase receipt and reward'))

    def test_source_year_and_exact_duplicates_cannot_leak(self):
        text = 'Subject: Security review notice From: recipient@example.com Please review the account settings and keep your access credentials private.'
        rows = [source_row('old', '1', 'phishing', text, 'train'),
                source_row('new', '2', 'phishing', text, 'test')]
        partitions, exclusions = split(rows)
        self.assertFalse(partitions['train'])
        self.assertEqual(len(partitions['test']), 1)
        self.assertEqual(exclusions['older_campaign_overlaps_later_partition'], 1)

    def test_conflicting_exact_labels_are_rejected_instead_of_guessed(self):
        text = 'This is exactly the same message with enough words for a stable text fingerprint.'
        rows = [source_row('one', '1', 'legitimate', text), source_row('two', '2', 'phishing', text)]
        partitions, exclusions = split(rows)
        self.assertEqual(sum(map(len, partitions.values())), 0)
        self.assertEqual(exclusions['conflicting_exact_label_group'], 2)

    def test_windowing_includes_every_tail_token(self):
        ids = list(range(2849))
        chunks = common.windows(ids)
        self.assertEqual(set(x for part in chunks for x in part), set(ids))
        self.assertTrue(all(len(part) <= 510 for part in chunks))

    def test_threshold_selection_requires_both_abuse_classes(self):
        scores = [{'label': 'legitimate', 'scores': [.95, .03, .02]},
                  {'label': 'spam', 'scores': [.02, .96, .02]},
                  {'label': 'phishing', 'scores': [.02, .01, .97]}]
        result = calibrated(scores)
        self.assertTrue(common.acceptance(result['metrics']))
        self.assertEqual(common.classify([.01, .98, .01], .5, .5), 'spam')
        self.assertEqual(common.classify([.01, .01, .98], .5, .5), 'phishing')

    def test_regression_requires_tail_detection_without_inventing_spam_results(self):
        measured = common.metrics([{'label': 'legitimate', 'prediction': 'legitimate'},
                                   {'label': 'phishing', 'prediction': 'phishing'}])
        report = {'metrics': measured, 'cohorts': {'tail': measured}}
        self.assertTrue(regression_acceptance(report))
        self.assertIsNone(measured['spam_recall'])
        report['cohorts']['tail'] = {**measured, 'phishing_recall': .75}
        self.assertFalse(regression_acceptance(report))

    def test_three_class_pilot_cannot_ignore_spam_failure(self):
        measured = common.metrics([{'label': 'legitimate', 'prediction': 'legitimate'},
                                   {'label': 'phishing', 'prediction': 'phishing'},
                                   {'label': 'spam', 'prediction': 'legitimate'}])
        self.assertFalse(regression_acceptance({'metrics': measured, 'cohorts': {}}))


if __name__ == '__main__':
    unittest.main()
