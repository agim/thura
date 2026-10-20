import http.server
import threading
import unittest
from client import Client,loopback_origin
from wizard import SavedFields


class ClientTests(unittest.TestCase):
    def test_fixture_cannot_target_external_servers_or_embed_credentials(self):
        for raw in ['http://production.example','http://user:secret@127.0.0.1:19090','http://127.0.0.1/path','http://127.0.0.1?token=secret','file:///private/file']:
            with self.assertRaises(ValueError):loopback_origin(raw)
        self.assertEqual(loopback_origin('http://127.0.0.1:19090/'),'http://127.0.0.1:19090')
        self.assertEqual(loopback_origin('http://fixture.localhost:19090'),'http://fixture.localhost:19090')
        client=Client('http://127.0.0.1:19090')
        for path in ['https://production.example/','//production.example/']:
            with self.assertRaises(ValueError):client.call(path)

    def test_redirects_cannot_send_bearer_token_to_another_origin(self):
        received=[]
        class Target(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                received.append(self.headers.get('Authorization'))
                self.send_response(200);self.end_headers();self.wfile.write(b'{}')
            def log_message(self,*args):pass
        target=http.server.ThreadingHTTPServer(('127.0.0.1',0),Target)
        class Redirect(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(302)
                self.send_header('Location','http://127.0.0.1:'+str(target.server_port)+'/receive')
                self.end_headers()
            def log_message(self,*args):pass
        source=http.server.ThreadingHTTPServer(('127.0.0.1',0),Redirect)
        threads=[threading.Thread(target=s.serve_forever,daemon=True) for s in (source,target)]
        for thread in threads:thread.start()
        try:
            client=Client('http://127.0.0.1:'+str(source.server_port),token='private-token')
            with self.assertRaisesRegex(RuntimeError,'redirect left'):client.call('/redirect')
            self.assertEqual(received,[])
        finally:
            for server in (source,target):server.shutdown();server.server_close()
            for thread in threads:thread.join()

    def test_wizard_submits_seeded_fields_without_clearing_saved_secrets(self):
        values=SavedFields('<form action="setup/save"><input name="COLLABORA_SERVER_URL" value="http://office:9980"><input type="password" name="COLLABORA_TOKEN_SECRET" value=""><input type="checkbox" name="clear_COLLABORA_TOKEN_SECRET" value="true"><textarea name="THURA_BACKUP_PLAN">Preserve &amp; test backups</textarea></form><form action="setup/restart"><input name="confirm" value="restart"></form>').values
        self.assertEqual(values,{'COLLABORA_SERVER_URL':'http://office:9980','COLLABORA_TOKEN_SECRET':'','THURA_BACKUP_PLAN':'Preserve & test backups'})

    def test_fixture_errors_do_not_echo_credentials_or_response_bodies(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                self.send_response(403);self.end_headers();self.wfile.write(b'private-provider-secret-do-not-print')
            def log_message(self,*args):pass
        server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            client=Client('http://127.0.0.1:'+str(server.server_port),token='private-agent-token')
            with self.assertRaises(RuntimeError) as caught:client.call('/v1/apps',{'secret':'private-payload'})
            self.assertEqual(str(caught.exception),'Fixture POST /v1/apps returned HTTP 403')
        finally:server.shutdown();server.server_close();thread.join()


if __name__=='__main__':unittest.main()
