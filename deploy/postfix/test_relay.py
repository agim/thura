import base64
import hashlib
import hmac
import http.server
import json
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

    def test_smtp_envelope_is_signed_for_spf(self):
        raw = b'From: alerts@origin.example\r\nTo: inbox@tenant.example\r\n\r\nForwarded\r\n'
        with mock.patch('relay.time.time', return_value=1000):
            self.assertEqual(relay.deliver(self.config, 'inbox@tenant.example', raw, '203.0.113.7', 'mx.forwarder.example', 'bounce@forwarder.example'), 0)
        _, headers, body = self.requests[-1]
        self.assertEqual(body, raw)
        envelope = headers['X-Thura-Envelope']
        decoded = json.loads(base64.urlsafe_b64decode(envelope + '=' * (-len(envelope) % 4)))
        self.assertEqual(decoded, {'clientAddress': '203.0.113.7', 'clientHelo': 'mx.forwarder.example', 'sender': 'bounce@forwarder.example'})
        signed = b'v2.' + headers['X-Thura-Timestamp'].encode() + b'.' + envelope.encode() + b'.' + headers['X-Thura-Delivery'].encode() + b'.' + raw
        self.assertEqual(headers['X-Thura-Signature'], hmac.new(SECRET.encode(), signed, hashlib.sha256).hexdigest())
        # The delivery ID is unchanged by the envelope, so retries stay idempotent.
        self.assertEqual(headers['X-Thura-Delivery'], hashlib.sha256(b'inbox@tenant.example\0' + raw).hexdigest())

    def test_envelope_is_omitted_or_trimmed_when_unusable(self):
        raw = b'From: sender@example.net\r\n\r\nBody\r\n'
        self.assertEqual(relay.envelope_header('', 'helo.example', 'a@example.net'), '')
        self.assertEqual(relay.envelope_header('unknown', 'helo.example', 'a@example.net'), '')
        self.assertEqual(relay.envelope_header('203.0.113.7', 'helo.example', 'bad sender@example.net'), '')
        bounce = relay.envelope_header('2001:db8::1', 'bad helo\nname', '')
        self.assertEqual(json.loads(base64.urlsafe_b64decode(bounce + '=' * (-len(bounce) % 4))), {'clientAddress': '2001:db8::1', 'clientHelo': '', 'sender': ''})
        relay.deliver(self.config, 'inbox@tenant.example', raw, 'unknown', 'helo.example', 'a@example.net')
        _, headers, _ = self.requests[-1]
        self.assertNotIn('X-Thura-Envelope', headers)
        signed = headers['X-Thura-Timestamp'].encode() + b'.' + headers['X-Thura-Delivery'].encode() + b'.' + raw
        self.assertEqual(headers['X-Thura-Signature'], hmac.new(SECRET.encode(), signed, hashlib.sha256).hexdigest())

    def test_postfix_pipe_passes_the_smtp_envelope(self):
        service = configure.pipe_service()
        self.assertIn(' pipe flags=R null_sender= user=thura-mail ', service)
        for argument in ('--recipient ${recipient}', '--client-address=${client_address}', '--client-helo=${client_helo}', '--sender=${sender}'):
            self.assertIn(argument, service)
        self.assertTrue(service.endswith('\n') and service.count('\n') == 1)

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
