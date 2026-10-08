# DOCX/XLSX editing

The optional connector uses ONLYOFFICE DocumentServer 9.0.4. Community Edition is AGPL-3.0 and has edition/concurrency limits; review its license and deployment terms for your distribution. The connector does not provision a production service or establish fidelity for arbitrary documents.

Set non-secret `OFFICE_SERVER_URL` to the service origin visible to the browser and `OFFICE_APP_URL` to the Thura origin reachable from DocumentServer. Both are HTTP origins with no path/query/userinfo; production should use HTTPS. Set `OFFICE_JWT_SECRET` through sealed credentials and configure the exact same secret in DocumentServer (`JWT_ENABLED=true`, `JWT_IN_BODY=true`). Use a strong random secret of at least 32 characters. The source and callback endpoints must be reachable by DocumentServer. Never disable signature validation.

Opening a DOCX/XLSX creates an eight-hour session tied to a workspace member and an immutable source version. Editor configuration is signed; the source ticket lasts ten minutes and source requests recheck membership and trash. The app's CSP permits the configured document origin for scripts, frames and connections. Treat that service as trusted code: isolate and maintain it, restrict its egress, and do not share its signing secret with unrelated services.

Save callbacks verify HS256 and match the signed key, status and URL. Only statuses 2/6 create versions. The download URL must use the configured service origin; redirects are refused and content is bounded to 10 MB. Session/file row locks serialize saves; checksums make repeated successful callbacks idempotent. A different edit/upload changing the base version refuses the save, returning the service's `error:1` envelope so its recoverable copy is retained. Download the editor copy before closing a failed/conflicting session. Existing editor data or previously downloaded bytes cannot be recalled on membership removal; new source requests and saves are refused. Keep the service's recovery storage until failed saves are resolved.

## Local integration check

The normal Go suite uses a contract-test document provider. `integration/office.spec.ts` exercises the real 9.0.4 service separately, including typing, saving and checking the resulting DOCX/XLSX XML. It requires Docker, Chromium and Python 3. The fixtures are minimal synthetic DOCX and XLSX documents; both passed opening, editing, saving, version-history and downloaded-content checks. Passing them does not establish general DOCX/XLSX fidelity.

A cloud test container uses port 8082, a synthetic test-only JWT secret, `host.docker.internal` mapped to the host gateway, and an app listening on `0.0.0.0:3002` against the test database. Its app origin is `http://host.docker.internal:3002` and browser-visible service origin is `http://127.0.0.1:8082`. Private IP access is enabled only for this isolated test connection. If the environment sets an HTTP proxy, exclude localhost and host.docker.internal in both `NO_PROXY` and `no_proxy`; otherwise the proxy can refuse local service requests.

Seed `e2e/seed.sql` in the test database, build the frontend/binary, run the app with those test settings, and run:

```sh
BASE_URL=http://127.0.0.1:3002 npx playwright test --config integration/playwright.config.ts
```

Production must use reachable TLS origins, private storage, persistent DocumentServer recovery data and a tested backup policy. Do not copy the synthetic test secret into production.
