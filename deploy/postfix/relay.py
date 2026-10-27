"""Postfix pipe delivery: success only after Thura persists the message.

The MTA retains/retries queued messages on EX_TEMPFAIL. Nothing sensitive is
logged. Recipient lookup never uses a shell; forwarding redirects are refused.
"""
import argparse
import base64
import ipaddress
import re
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


def deliver(config, recipient, raw, envelope=None):
    mailbox = config['mailboxes'].get(recipient.lower())
    if not mailbox:
        return 75 if config.get('managed') else 67  # Retain queued mail after a managed mapping is removed.
    if len(raw) > MAX_MESSAGE:
        return 65  # EX_DATAERR; also enforced before the MTA queues it.
    encoded = ''
    if envelope is not None:
        ipaddress.ip_address(envelope['ip'])
        if len(envelope['from'])>254 or len(envelope['helo'])>253 or not envelope['helo'] or re.search(r'[\r\n\x00]', envelope['from']+envelope['helo']):
            return 65
        encoded = base64.urlsafe_b64encode(json.dumps(envelope, separators=(',', ':')).encode()).decode().rstrip('=')
    timestamp = str(int(time.time()))
    delivery = hashlib.sha256(recipient.lower().encode() + b'\0' + (encoded.encode() + b'\0' if encoded else b'') + raw).hexdigest()
    signature = hmac.new(mailbox['secret'].encode(),
                         (b'smtp2.' if encoded else b'') + timestamp.encode() + b'.' + delivery.encode() + b'.' + (encoded.encode() + b'.' if encoded else b'') + raw,
                         hashlib.sha256).hexdigest()
    request = urllib.request.Request(
        config['baseUrl'].rstrip('/') + '/api/v1/inbound/' + mailbox['id'],
        data=raw, method='POST', headers={
            'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': timestamp,
            'X-Thura-Delivery': delivery, 'X-Thura-Signature': signature,
            **({'X-Thura-SMTP-Envelope': encoded} if encoded else {}),
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
    parser.add_argument('--client-ip')
    parser.add_argument('--sender', default='')
    parser.add_argument('--helo', default='unknown')
    args = parser.parse_args()
    try:
        with open(args.config, encoding='utf-8') as source:
            config = json.load(source)
        raw = sys.stdin.buffer.read(MAX_MESSAGE + 1)
        envelope = {'ip': args.client_ip, 'from': args.sender, 'helo': args.helo} if args.client_ip else None
        return deliver(config, args.recipient, raw, envelope)
    except (OSError, ValueError, KeyError, TypeError):
        return 75


if __name__ == '__main__':
    sys.exit(main())
