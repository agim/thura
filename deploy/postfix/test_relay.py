import hashlib
import hmac
import http.server
import threading
import unittest
from unittest import mock

import configure
import relay

SECRET = 'thura-postfix-public-synthetic-secret-184729'
MAILBOX = 'a0000000-0000-4000-8000-000000000010'


class RelayTests(unittest.TestCase):
    def setUp(self):
        self.requests = []
        self.status = 200
        fixture = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                raw = self.rfile.read(int(self.headers['Content-Length']))
                fixture.requests.append((self.path, dict(self.headers), raw))
                self.send_response(fixture.status)
                if fixture.status == 302:
                    self.send_header('Location', '/must-not-follow')
                self.end_headers()

            def log_message(self, *_args):
                pass

        self.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.config = {'baseUrl': f'http://127.0.0.1:{self.server.server_port}',
                       'mailboxes': {'inbox@tenant.example': {'id': MAILBOX, 'secret': SECRET}}}

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_raw_bytes_recipient_signature_and_retry_identity(self):
        raw = b'From: sender@example.net\r\nTo: inbox@tenant.example\r\n\r\nBody\r\n'
        for timestamp in (1000, 1001):
            with mock.patch('relay.time.time', return_value=timestamp):
                self.assertEqual(relay.deliver(self.config, 'INBOX@tenant.example', raw), 0)
        self.assertEqual(len(self.requests), 2)
        deliveries = []
        for path, headers, body in self.requests:
            self.assertEqual(path, '/api/v1/inbound/' + MAILBOX)
            self.assertEqual(body, raw)
            deliveries.append(headers['X-Thura-Delivery'])
            signed = headers['X-Thura-Timestamp'].encode() + b'.' + headers['X-Thura-Delivery'].encode() + b'.' + raw
            self.assertEqual(headers['X-Thura-Signature'], hmac.new(SECRET.encode(), signed, hashlib.sha256).hexdigest())
        self.assertEqual(deliveries[0], deliveries[1])
        self.assertNotEqual(self.requests[0][1]['X-Thura-Signature'], self.requests[1][1]['X-Thura-Signature'])

    def test_failure_and_redirect_retain_the_mta_queue(self):
        for status in (401, 403, 409, 503, 302):
            self.status = status
            self.assertEqual(relay.deliver(self.config, 'inbox@tenant.example', b'raw'), 75)
        self.assertEqual(len(self.requests), 5)
        self.assertTrue(all(path != '/must-not-follow' for path, _, _ in self.requests))

    def test_unknown_recipient_and_oversized_message_do_not_forward(self):
        self.assertEqual(relay.deliver(self.config, 'other@tenant.example', b'raw'), 67)
        self.assertEqual(relay.deliver(self.config, 'inbox@tenant.example', b'x' * (relay.MAX_MESSAGE + 1)), 65)
        self.assertEqual(self.requests, [])

    def test_operator_configuration_and_multiple_domains(self):
        self.config['mailboxes']['team@second.example'] = {'id': MAILBOX, 'secret': SECRET}
        self.assertEqual(configure.settings(self.config, 'mail.operator.example', '127.0.0.1:2525'), ['second.example', 'tenant.example'])
        self.config['baseUrl'] = 'https://app.operator.example'
        self.assertEqual(len(configure.settings(self.config, 'mail.operator.example', '25')), 2)
        for invalid in ('http://external.example', 'https://user:password@app.example', 'https://app.example/?token=x', 'https://app.example/private'):
            self.config['baseUrl'] = invalid
            with self.assertRaises(ValueError):
                configure.settings(self.config, 'mail.operator.example', '25')
        self.config['baseUrl'] = 'https://app.example'
        for invalid in ('25\nmalicious unix', '0', '65536', '0.0.0.0:25', '2525'):
            with self.assertRaises(ValueError):
                configure.settings(self.config, 'mail.operator.example', invalid)


if __name__ == '__main__':
    unittest.main()
