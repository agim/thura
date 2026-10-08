# Matrix Chat

Thura stores workspace/room mappings in Postgres and uses a Matrix application
service on the Go control plane. Room and message requests always use the
workspace's current membership. Direct conversations are additionally restricted
to the two named participants. The browser receives no Matrix access token;
Thura sessions cannot log into Matrix directly. Virtual identities use a stable
hash of the Thura account subject, distinct from the service bot.

This first version deliberately uses server-readable rooms. It does not claim
end-to-end encryption, encrypted device recovery, encrypted search, or user
access through third-party Matrix clients. The Go API boundary keeps immediate
local revocation enforceable while key/device design remains open. Text is
rendered as text, including externally supplied HTML strings. Replies, read
markers, backward pagination, periodic polling/reconnect, and Drive-file copies
are supported. Matrix event transaction IDs deduplicate retries. Failed sends
retain the transaction ID until the user changes the message.

Shared channels require invite-only room join rules and joined-member history.
Workspace owners/admins can invite or ban a remote Matrix identity on a channel;
that grant is specific to the Matrix room and conveys no other workspace access.
Private conversations accept existing workspace members only. Remote servers
retain federated messages and files; bans and Drive-share revocation cannot
recall copies already received. A file send reads an authorized immutable Drive
version through the official storage pack and sends a copy to Matrix's media
repository. Attachment downloads resolve the reference from an authorized room
event, enforce a 10 MB limit, and force binary download without active preview.
Repeated file sends can leave unused media uploads even though event retries do
not create duplicate messages; configure Synapse media retention and backups.

A minute reconciliation job kicks virtual users whose workspace membership was
removed and removes their private-room participant grant. Until it runs, local
API access is already denied and there is no direct Matrix token for the former
member. The job processes 100 removals per tick. Monitor failed jobs during
homeserver outages. Workspace rooms are limited to 100; timelines load 50 events
per page. The application service owns rooms and their moderation power.

## Deployment boundary

Provide sealed `MATRIX_AS_TOKEN` and operator settings `MATRIX_SERVER_URL`
(the homeserver's HTTP API origin) and `MATRIX_SERVER_NAME` (its federated server
name, optionally with a port). Provision the matching Synapse application-service
registration with sender `thura_bot`, `rate_limited: false`, exclusive user
namespace `@thura_.*:<escaped-server-name>`, and exclusive alias namespace
`#thura_room_.*:<escaped-server-name>`. Use a distinct random `hs_token`; this
outbound-only service uses `url: null`, so it does not expose an inbound
application-service webhook. Enable TLS, public federation discovery, and
Synapse network restrictions in production. Keep the service token off public
logs and out of the browser. Back up registrations, signing keys, Synapse state
and media with Postgres/object backups. Rotating the application-service token
requires updating both sealed credentials and registration.

Integration fixtures use Element Synapse **v1.139.0** (AGPL-3.0), image digest
`sha256:76ffaa19418923ab7b70fa0947c1ae16c07b45a65f596c1ff0424ce5eac8b185`.
This is a compatibility pin, not a claim that it is the newest security release.
Review current advisories and upgrade through the same federation suite before
a production deployment. No Matrix SDK is shipped to the browser in this
initial design; the Matrix client-server protocol is isolated in
`internal/providers/matrix` to preserve the authorization boundary.

The dedicated real-service test passed and runs with two isolated homeservers,
`matrix-a.thura.test:8448` and `matrix-b.thura.test:8448`, client ports 8108/8109.
It runs with `go test -tags matrixintegration -run TestMatrixRealFederationAndBan -v .`.
These are synthetic fixture credentials and data. Self-signed test certificates
and private-network federation allowances must never be copied to production.

Start the disposable fixtures with `python3 scripts/matrix-fixtures.py` before
running the dedicated Go test. The script binds client ports to localhost, clears
cloud proxy settings only inside those test containers, and uses test-only TLS
and network policy. Stop/remove the two `thura-matrix-a`/`thura-matrix-b`
containers and `thura-matrix-test` network when finished. Data is under
`/tmp/thura-matrix-fixtures` by default.

The real browser suite uses `integration/chat.playwright.config.ts` against an
app on port 3002, with the test database/browser seed and server-A fixture
settings. It exercises persistence, safe external text, replies, Drive-file
copy/download and a read marker through the production frontend.
