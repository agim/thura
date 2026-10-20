"""Private loopback-only clients for an owned deployment acceptance fixture."""
import http.cookiejar
import ipaddress
import json
import time
import urllib.error
import urllib.parse
import urllib.request


def loopback_origin(raw):
    url = urllib.parse.urlsplit(raw)
    try:
        local = ipaddress.ip_address(url.hostname or '').is_loopback
    except ValueError:
        local = url.hostname == 'localhost' or (url.hostname or '').endswith('.localhost')
    if not local or url.scheme not in ('http','https') or url.username or url.query or url.fragment or url.path not in ('','/'):
        raise ValueError('Acceptance clients require an explicit loopback origin')
    return raw.rstrip('/')


class SameOriginRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        def origin(raw):
            url = urllib.parse.urlsplit(raw)
            return url.scheme, url.hostname, url.port or (443 if url.scheme == 'https' else 80)
        if origin(request.full_url) != origin(newurl):
            raise RuntimeError('Fixture redirect left its loopback origin')
        return super().redirect_request(request, fp, code, msg, headers, newurl)


class Client:
    def __init__(self, origin, token=None, host=None):
        self.origin = loopback_origin(origin)
        self.token = token
        self.host = host
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(SameOriginRedirects(), urllib.request.HTTPCookieProcessor(self.cookies))

    def call(self, path, data=None, method=None, form=False, text=False):
        if not path.startswith('/') or path.startswith('//'):
            raise ValueError('Fixture requests must stay on their loopback origin')
        body = None if data is None else (urllib.parse.urlencode(data).encode() if form else json.dumps(data).encode())
        headers = {'Content-Type':'application/x-www-form-urlencoded' if form else 'application/json','Sec-Fetch-Site':'same-origin'}
        if self.token: headers['Authorization'] = 'Bearer '+self.token
        if self.host: headers['Host'] = self.host
        request = urllib.request.Request(self.origin+path,data=body,headers=headers,method=method)
        try:
            with self.opener.open(request,timeout=35) as response:
                raw=response.read(8*1024*1024+1)
                if len(raw)>8*1024*1024:raise RuntimeError('Fixture response exceeded its bound')
                if self.token and urllib.parse.urlsplit(response.url).netloc != urllib.parse.urlsplit(self.origin).netloc:
                    raise RuntimeError('Agent request left its loopback origin')
                if form and 'error=' in response.url:raise RuntimeError('Wizard action refused; inspect private fixture logs')
                if text or form:return raw.decode()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            # Never echo credential-bearing requests or arbitrary provider bodies.
            raise RuntimeError(f'Fixture {method or ("POST" if data is not None else "GET")} {path.split("?")[0]} returned HTTP {error.code}') from None


def eventually(check, label, seconds=180):
    deadline=time.monotonic()+seconds
    last=None
    while time.monotonic()<deadline:
        try:
            result=check()
            if result:return result
        except (OSError,RuntimeError,ValueError) as error:last=type(error).__name__
        time.sleep(.5)
    raise RuntimeError(f'{label} did not become ready'+(f' ({last})' if last else ''))
