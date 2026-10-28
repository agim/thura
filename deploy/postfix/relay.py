"""Postfix pipe delivery: success only after Thura persists the message.

The MTA retains/retries queued messages on EX_TEMPFAIL. Nothing sensitive is
logged. Recipient lookup never uses a shell; forwarding redirects are refused.
"""
import argparse
import base64
import hashlib
import hmac
import ipaddress
import json
import re
import sys
import time
import urllib.error
import urllib.request

MAX_MESSAGE = 12 * 1024 * 1024


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, url):
        return None


PRINTABLE = re.compile(r'[\x21-\x7e]*')


def envelope_header(client_address, client_helo, sender):
    """Signed SMTP envelope for SPF: Postfix's ${client_address}, ${client_helo}
    and ${sender}. Omitted (SPF then stays unchecked) when the client address is
    not an IP, e.g. locally generated mail."""
    try:
        ip = ipaddress.ip_address(client_address or '')
    except ValueError:
        return ''
    if not (len(sender) <= 320 and PRINTABLE.fullmatch(sender)):
        return ''
    if not (len(client_helo) <= 255 and PRINTABLE.fullmatch(client_helo)):
        client_helo = ''
    fields = {'clientAddress': str(ip), 'clientHelo': client_helo, 'sender': sender}
    encoded = json.dumps(fields, separators=(',', ':'), sort_keys=True).encode()
    return base64.urlsafe_b64encode(encoded).rstrip(b'=').decode()


def deliver(config, recipient, raw, client_address='', client_helo='', sender=''):
    mailbox = config['mailboxes'].get(recipient.lower())
    if not mailbox:
        return 75 if config.get('managed') else 67  # Retain queued mail after a managed mapping is removed.
    if len(raw) > MAX_MESSAGE:
        return 65  # EX_DATAERR; also enforced before the MTA queues it.
    timestamp = str(int(time.time()))
    delivery = hashlib.sha256(recipient.lower().encode() + b'\0' + raw).hexdigest()
    envelope = envelope_header(client_address, client_helo, sender)
    headers = {'Content-Type': 'message/rfc822', 'X-Thura-Timestamp': timestamp, 'X-Thura-Delivery': delivery}
    if envelope:
        signed = b'v2.' + timestamp.encode() + b'.' + envelope.encode() + b'.' + delivery.encode() + b'.' + raw
        headers['X-Thura-Envelope'] = envelope
    else:
        signed = timestamp.encode() + b'.' + delivery.encode() + b'.' + raw
    headers['X-Thura-Signature'] = hmac.new(mailbox['secret'].encode(), signed, hashlib.sha256).hexdigest()
    request = urllib.request.Request(
        config['baseUrl'].rstrip('/') + '/api/v1/inbound/' + mailbox['id'],
        data=raw, method='POST', headers=headers)
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
    parser.add_argument('--client-address', default='')
    parser.add_argument('--client-helo', default='')
    parser.add_argument('--sender', default='')
    args = parser.parse_args()
    try:
        with open(args.config, encoding='utf-8') as source:
            config = json.load(source)
        raw = sys.stdin.buffer.read(MAX_MESSAGE + 1)
        return deliver(config, args.recipient, raw, args.client_address, args.client_helo, args.sender)
    except (OSError, ValueError, KeyError, TypeError):
        return 75


if __name__ == '__main__':
    sys.exit(main())
