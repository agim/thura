import unittest
import benchmark
import evaluate


class BenchmarkTests(unittest.TestCase):
    def test_failed_predictions_are_never_removed_from_quality_denominators(self):
        result = benchmark.metrics([
            {'expected': 'safe', 'prediction': 'phishing'},
            {'expected': 'safe', 'prediction': None},
            {'expected': 'phishing', 'prediction': 'safe'},
            {'expected': 'phishing', 'prediction': 'phishing'},
            {'expected': 'phishing', 'prediction': 'error'},
        ])
        self.assertEqual(result['invalid_or_failed'], 2)
        self.assertEqual(result['false_positive_rate'], 1)
        self.assertAlmostEqual(result['phishing_recall'], 1 / 3)
        self.assertEqual(result['accuracy'], .2)

    def test_tail_and_boundary_tokens_are_all_included(self):
        for size in [0, 1, 510, 511, 1020, 5000]:
            ids = list(range(size))
            chunks = evaluate.windows(ids)
            self.assertTrue(chunks)
            self.assertTrue(all(len(chunk) <= 510 for chunk in chunks))
            self.assertEqual(set(x for chunk in chunks for x in chunk), set(ids))

    def test_publisher_urls_do_not_inflate_email_metrics(self):
        rows = [{'id': 'publisher-test-0-a', 'expected': 'safe', 'prediction': 'phishing'},
                {'id': 'publisher-test-2-b', 'expected': 'safe', 'prediction': 'safe'}]
        result = benchmark.quality_groups(rows)
        self.assertEqual(result['publisher_email']['false_positive_rate'], 1)
        self.assertEqual(result['publisher_url']['false_positive_rate'], 0)


if __name__ == '__main__':
    unittest.main()
