"""Postfix pipe delivery: success only after Thura persists the message.

The MTA retains/retries queued messages on EX_TEMPFAIL. Nothing sensitive is
logged. Recipient lookup never uses a shell; forwarding redirects are refused.
"""
import argparse
import hashlib
import hmac
import json
import sys
import time
import urllib.error
import urllib.request

MAX_MESSAGE = 12 * 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, url):
        return None


def deliver(config, recipient, raw):
    mailbox = config['mailboxes'].get(recipient.lower())
    if not mailbox:
        return 67  # EX_NOUSER; RCPT checks should already have refused this.
    if len(raw) > MAX_MESSAGE:
        return 65  # EX_DATAERR; also enforced before the MTA queues it.
    timestamp = str(int(time.time()))
    delivery = hashlib.sha256(recipient.lower().encode() + b'\0' + raw).hexdigest()
    signature = hmac.new(mailbox['secret'].encode(),
                         timestamp.encode() + b'.' + delivery.encode() + b'.' + raw,
                         hashlib.sha256).hexdigest()
    request = urllib.request.Request(
        config['baseUrl'].rstrip('/') + '/api/v1/inbound/' + mailbox['id'],
        data=raw, method='POST', headers={
            'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': timestamp,
            'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature,
        })
    try:
        with urllib.request.build_opener(NoRedirect).open(request, timeout=15) as response:
            return 0 if 200 <= response.status < 300 else 75
    except (urllib.error.URLError, TimeoutError, OSError):
        # Configuration/authentication errors are also temporary: an operator
        # can repair them without discarding already accepted mail.
        return 75


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--config', required=True)
    parser.add_argument('--recipient', required=True)
    args = parser.parse_args()
    try:
        with open(args.config, encoding='utf-8') as source:
            config = json.load(source)
        raw = sys.stdin.buffer.read(MAX_MESSAGE + 1)
        return deliver(config, args.recipient, raw)
    except (OSError, ValueError, KeyError, TypeError):
        return 75


if __name__ == '__main__':
    sys.exit(main())
