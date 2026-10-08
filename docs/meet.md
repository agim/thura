# Workspace meetings

Meetings belong to a workspace. Current members can create them and request
short-lived access; no client-supplied SFU room name is trusted. The server maps
a meeting UUID to `thura_<UUID>` and creates that room before joining. Guests are
not enabled: a meeting link conveys no access beyond workspace membership.
Tokens name one identity and room, permit camera/microphone/screen sharing and
subscription, and have no room-administration, recording, or room-creation
permissions. Recording, transcription, and AI summaries are disabled.

Camera and microphone start off; the participant enables them explicitly in
the LiveKit controls. Screen sharing requests browser permission. The interface
shows connection/reconnect state and supports camera/mic controls, screen
sharing, leaving, and ephemeral meeting chat. The creator and workspace managers
can end a meeting for everyone. Copying its URL allows a workspace member to
open the same meeting from a Calendar location/description or Matrix message.

## Configuration and lifecycle

Seal `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` (at least 32 characters). Configure
`LIVEKIT_SERVER_URL` as the Go control-plane HTTP(S) origin and
`LIVEKIT_PUBLIC_URL` as the browser's WS(S) origin. Use HTTPS/WSS publicly. The
app permits connections only to that configured origin, with same-origin camera,
microphone and display-capture permissions. It permits style attributes needed
by the media UI, while keeping inline scripts blocked. App API calls use the
generated Līdza client; the media SDK speaks LiveKit's explicit service protocol.

Configure LiveKit `room.auto_create: false`, so possession of an old join token
cannot recreate a deleted meeting room. Route signed webhooks to
`/api/v1/meet/webhook`, using the same API key/secret. The official LiveKit
verifier checks the JWT and the exact body checksum, with a 64 KB input bound.
An event ledger deduplicates delivery. A session ID prevents a delayed leave
from clearing a newer join. Unknown/nonmember identities and joins on ended
meetings are removed. Operators must monitor webhook and job failures.

Join tokens expire after two minutes; expiration alone does **not** disconnect
an established media session. Workspace revocation immediately denies new Thura
join-token requests. Signed join webhooks and a minute reconciliation job remove
former members from the SFU. Self-hosted LiveKit may accept an already-issued
token until its expiry, so this architecture has a bounded revocation window
and depends on working webhook/job delivery to terminate connected participants.
Do not claim instantaneous media revocation or a passed restrictive-network gate.
The reconciliation batch is 100 participants per tick. Webhook ledgers expire
after 30 days. Each workspace has at most 100 active meetings and each room at
most 100 participants. Meeting metadata and provider operations are separate
transactions; a provider failure is reported and can be retried.

Deploy SFU UDP/TCP media ports and a properly configured TURN service for
restrictive networks. TURN/TLS needs valid certificates and externally reachable
advertised addresses. Browser permission, device selection, autoplay, firewall
and network conditions remain real deployment acceptance checks; local synthetic
media cannot establish physical-device or public-network readiness.

## Compatibility tests

Frontend pins: `livekit-client` 2.22.3, `@livekit/components-react` 2.9.24,
`@livekit/components-styles` 1.1.6 (Apache-2.0). The Go token/webhook dependency is
`github.com/livekit/protocol` v1.49.0. The room-service protocol client is isolated
in `internal/providers/livekit`, uses the framework HTTP client, refuses redirects,
bounds responses and applies deadlines. The local SFU compatibility pin is
LiveKit server v1.13.9, image digest
`sha256:d0c04791bf63ca8dcea123571827d56ba9504bd57c8d5de60ee03b5408cc3154`.
Review current security advisories before upgrading/deploying.

`integration/meet.playwright.config.ts` selects a dedicated two-browser suite
against an app on port 3002 and the browser-test database. It uses Chromium's
synthetic camera/microphone devices. The isolated service binds signaling/TCP/UDP
ports 7880/7881/7882 to localhost, disables room auto-creation, and sends signed
webhooks to the test app. These credentials and media are synthetic, never
production configuration. The ordinary browser suite verifies that an
unconfigured service is reported truthfully without simulating a call.

Start the isolated SFU with `python3 scripts/livekit-fixtures.py` on Linux Docker.
The fixture uses host networking with loopback bind/advertised addresses to keep
RTP paths deterministic. Start the test app with `LIDZA_MODE=test`,
`LIDZA_ADDR=0.0.0.0:3002`, `LIVEKIT_SERVER_URL=http://127.0.0.1:7880`,
`LIVEKIT_PUBLIC_URL=ws://127.0.0.1:7880`, `LIVEKIT_API_KEY=thura-local-test-key`,
and `LIVEKIT_API_SECRET=thura-livekit-local-service-fixture-secret-981734`.
These are public synthetic fixtures; they grant no access to a production system.
Then run `BASE_URL=http://127.0.0.1:3002 npx playwright test --config integration/meet.playwright.config.ts`.
Stop/remove `thura-livekit-test` and the temporary app when finished.

Media dependencies load on demand. The selected upstream client is a bundled
module (~518 KB minified, ~135 KB gzip) and is isolated from the initial app
bundle; the React media controls are a separate ~133 KB minified chunk.
The bundle groups exclude recursive dependency capture so shared React code cannot pull media chunks into ordinary page loads. Strict execution order preserves module initialization; the real two-browser media suite validates the resulting split. The upstream client triggers Vite's generic 500 KB chunk warning; it is not
silenced by raising the warning threshold. Review these sizes when upgrading.
A fixed media viewport supports adaptive subscriptions before remote tracks arrive.
