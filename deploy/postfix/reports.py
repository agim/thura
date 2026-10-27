"""Private SMTP bridge: submit aggregate DMARC reports to Thura's normal outbox."""
import json
import os
import re
import socketserver
import urllib.error
import urllib.request
from relay import NoRedirect

MAX = 4 * 1024 * 1024


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(30)
        recipients = []
        self.wfile.write(b'220 Thura private DMARC submission\r\n')
        while line := self.rfile.readline(4097):
            if len(line) > 4096:
                return
            verb = line.split(b' ', 1)[0].strip().upper()
            if verb == b'HELO':
                self.wfile.write(b'250 localhost\r\n')
            elif verb == b'EHLO':
                self.wfile.write(b'250-localhost\r\n250 SIZE 4194304\r\n')
            elif verb == b'MAIL':
                recipients = []
                self.wfile.write(b'250 OK\r\n')
            elif verb == b'RCPT':
                match = re.fullmatch(rb'RCPT TO:\s*<([^<>\r\n ]+)>\s*\r?\n', line, re.I)
                if not match or len(recipients) >= 10:
                    self.wfile.write(b'550 Invalid report recipient\r\n')
                else:
                    recipients.append(match.group(1).decode('ascii'))
                    self.wfile.write(b'250 OK\r\n')
            elif verb == b'DATA' and recipients:
                self.wfile.write(b'354 Send report\r\n')
                content = bytearray()
                while part := self.rfile.readline(MAX + 1):
                    if part == b'.\r\n':
                        break
                    content += part[1:] if part.startswith(b'..') else part
                    if len(content) > MAX:
                        self.wfile.write(b'552 Report too large\r\n')
                        return
                else:
                    return
                request = urllib.request.Request('http://127.0.0.1:3000/api/v1/setup/postfix/reports',
                    data=bytes(content), method='POST', headers={'Authorization': 'Bearer ' + os.environ['THURA_POSTFIX_TOKEN'],
                    'Content-Type': 'message/rfc822', 'X-Thura-Report-Recipients': json.dumps(recipients)})
                try:
                    with urllib.request.build_opener(NoRedirect).open(request, timeout=20) as response:
                        ok = response.status == 202
                except (OSError, urllib.error.URLError):
                    ok = False
                self.wfile.write(b'250 Report accepted into Thura outbox\r\n' if ok else b'451 Report could not be accepted\r\n')
                recipients = []
            elif verb == b'RSET':
                recipients = []
                self.wfile.write(b'250 OK\r\n')
            elif verb == b'NOOP':
                self.wfile.write(b'250 OK\r\n')
            elif verb == b'QUIT':
                self.wfile.write(b'221 Bye\r\n')
                return
            else:
                self.wfile.write(b'503 Invalid report SMTP command\r\n')


class Server(socketserver.TCPServer):
    allow_reuse_address = True


if __name__ == '__main__':
    # Shared app network namespace; never publish this port.
    with Server(('127.0.0.1', 2526), Handler) as server:
        server.serve_forever()
