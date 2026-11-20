# Installable app and notifications

Thura is an installable web app (PWA) and sends Web Push notifications to the
devices a member turns them on for. There is no native app, no vendor push SDK
and no third-party service besides the browser's own push service.

## The installable app

- **Manifest**: `public/manifest.webmanifest`, served by the binary as
  `/manifest.webmanifest` (`application/manifest+json`, registered in
  `app/pwa.go`). Name "Thura", start URL `/app/mail`, scope `/`, standalone,
  theme `#f6f3eb` and background `#fffdf9` from the brief's palette (the HTML
  head adds a dark `theme-color`, and the workspace follows the saved theme).
  Icons in `public/icons/`: 192 and 512 px, a maskable 512 px, an Apple touch
  icon, a notification badge and a favicon, drawn as the header's "t" mark.
- **Service worker**: `scripts/sw.js` is the source; `npm run build` (via
  `lidza build`) runs `scripts/service-worker.mjs`, which writes `dist/sw.js`
  with the precache list (the app entry's hashed script and styles with their
  static imports, the offline page, the manifest and icons) and a version that
  changes with any of them. It is served at `/sw.js` with `Cache-Control:
  no-cache`, scope `/`, and registered after the page loads (`src/pwa.ts`,
  production builds only).
  - It never caches `/api`, `/admin` or any page: navigations go to the
    network, and only when there is none does it answer with the offline page
    (`public/offline.html`, with its own stylesheet and script, no inline code).
  - Other hashed files under `/assets/` (lazy app chunks) are public and
    immutable; it caches them as they are fetched.
  - Updates: a new deploy changes `sw.js`; the browser installs the new worker,
    which waits. The page shows **Update available · Reload**
    (`src/components/UpdatePrompt.tsx`): fixed at the bottom, a status (it never
    takes focus), and it never reloads by itself, so a draft in progress stays
    the member's to finish. Reload tells the waiting worker to take over and the
    page reloads once it has. The page checks for a new version when the tab
    becomes visible and every 30 minutes; a failed check shows nothing.
  - CSP: unchanged in strictness. `worker-src 'self'` and `manifest-src 'self'`
    are named explicitly; scripts stay `'self'`, styles stay hashed.
- **Install prompt**: the account menu shows **Install Thura** where the
  browser offers installation (`beforeinstallprompt`: Chrome, Edge, Samsung
  Internet on Android and desktop), where it opens the browser's own dialog.
  On a phone it is always there, even before (or without) that event, with
  the browser's own steps: Chrome on Android ⋮ → Install app (or Add to Home
  screen → Install), Samsung Internet ☰ → Add page to → Home screen, Safari on
  iPhone and iPad Share → Add to Home Screen. It is dismissible (remembered on
  that device) and hidden in the installed app (`display-mode: standalone` or
  Safari's `navigator.standalone`).

## Notifications

### What notifies

Each event is a checkbox in **Settings → Notifications**, per member and per
workspace (table `notification_preference`; no row means the defaults, all on):

| Event | Who | Default | Notes |
| --- | --- | --- | --- |
| New mail in Inbox | Owner of a personal mailbox; every member for a shared one | **Only mail from real people** | A choice of three: All new Inbox mail, Only mail from real people, No mail (stored as `mail` + `mailPeopleOnly`). Only mail that reaches the Inbox: clean at intake, or released to the Inbox by screening. Never Spam, quarantined or held mail. "Only mail from real people" uses the Real person decision (header rules, model, replied-to or saved contact). |
| New chat messages | Members who joined the room, except the sender | On | Messages and files sent through Thura. Messages that arrive from another Matrix server are not seen by Thura's server and do not notify. Messages in accepted external (SMIP) conversations notify without a preview. |
| External chat requests | Owner of the external address; every member for a shared one | On | Once per new request; no preview of the unreviewed remote text. |
| Calendar reminders | The member who set the reminder | On | The existing reminder job sends the push alongside the email, deduplicated by the same notice ledger. If the member has no personal mailbox, the push still goes out. |
| Meetings starting now | Attendees whose address is a workspace account, except the organizer | On | When invitations are sent for an event with a Thura Meet room that is under way or starts within 15 minutes. |

A member is never notified about their own action (the actor is dropped), and
the service worker shows nothing while a focused, visible tab already shows that
view (the same chat room, the Inbox of that mailbox, the same meeting).
Reminders always show.

### Privacy

- Payloads are a short title and preview, at most 80 and 140 characters, and a
  link inside the app. Mail shows the sender's name and the subject, never the
  body; external chat shows the remote address only; chat shows the first 140
  characters of a message from a workspace member.
- Every payload is encrypted for the receiving browser (RFC 8291, `aes128gcm`):
  the push service (Google, Mozilla, Apple, Microsoft) carries it but cannot
  read it. It does see the endpoint, the time and the size.
- Personal mailboxes notify only their owner, as Mail shows them only to their
  owner. Leaving a workspace stops delivery: each send rechecks membership and
  removes the subscription of a former member.
- The device list shows a label ("Chrome on Android") and dates, never the
  endpoint (a capability URL); the page finds "This device" by comparing a
  SHA-256 hash of its own endpoint.
- On a shared computer, turn notifications off before handing it over: the
  browser keeps its subscription until a member removes it here.

### Links

Each payload carries a URL with the app's view-state parameters, so a tap opens
the right view: `/app/mail?workspace=…&mailbox=…&folder=inbox&message=…`,
`/app/chat?workspace=…&room=…` (or `external=…`),
`/app/calendar?workspace=…&calendar=…&event=…&instance=…`,
`/app/meet?workspace=…&meeting=…`. An open tab routes there itself; otherwise a
window opens. Links outside `/app/` are replaced by `/app/mail`.

### Permission

Thura never asks on page load. **Turn on notifications on this device** in
Settings → Notifications asks the browser (a click is required). The section
shows the state: on, off, blocked by the browser (with how to allow it again in
the site settings), unsupported, or, on iPhone and iPad outside the Home Screen
app, that Thura must be added to the Home Screen first.

### Diagnostics

Settings → Notifications answers "why didn't I get one":

- **Devices**: each row shows "This device" or its label, "Last notification:
  3 min ago (New mail)" or "Never sent", how many were delivered and failed,
  and the last error in plain words when it is newer than the last delivery
  (the push service's status and reason, a network, VAPID or encryption
  failure, an endpoint the policy refuses, or a delivery job that gave up).
  **Send test** sends to that device in the request and answers in place:
  "Sent · delivered by Google's push service" (the service accepted it: if
  nothing shows, the phone or Android held it back) or the exact error. A
  device the server removed (404/410, policy, a member who left) stays listed
  under the devices with why, until the browser subscribes again (at most 10,
  `push_removed_device`).
- **Not getting notifications?**: the phone's notification settings for Thura
  or Chrome, battery optimisation, the current mail choice, Send test.
- **Recent notifications**: the last 10 decisions for the member: the kind,
  the time, and delivered, failed (with the error), waiting, or skipped with
  the reason: not from a real person (Only from real people is on), you had it
  open (the service worker reports a notification it did not show because a
  focused tab showed the view), spam/held, your own action, or turned off.
  Table `notification_log`, a ring buffer of 50 rows per member and
  workspace, pruned on every insert; it keeps the kind and the item link,
  never the title or preview. Only members with a device get rows.
- Each subscription keeps `last_attempt_at`, `last_used_at` (last delivered),
  `last_kind`, `last_error_at`, `last_error_kind`, `last_error`,
  `delivered_count` and `failed_count`; a retried attempt records its error,
  and the failure counts once, on the queue's last attempt. Failures are
  logged (`push notification failed`, warn while retried, error when final)
  with the error kind, status, event kind and subscription ID, never the
  endpoint or keys.
- The page refreshes every 30 seconds while visible; a slow or failed refresh
  keeps what is on screen and says so beside it.

### Delivery

- Server: `internal/push`. Events call `push.Notify` (inside the transaction of
  the change when there is one), which filters recipients by preference and
  queues one `thura.push.send` job per device (jobs pack, 5 attempts).
- The job signs a VAPID JWT and encrypts the payload with
  [`github.com/SherClockHolmes/webpush-go`](https://github.com/SherClockHolmes/webpush-go)
  (MIT). 2xx records the delivery; 404 and 410 delete the subscription (the
  reason stays for Settings); 429, 5xx and network errors are retried by the
  queue; other refusals are recorded, logged and dropped.
- Endpoints must be `https` URLs of a known browser push service
  (`fcm.googleapis.com`, `push.services.mozilla.com`, `push.apple.com`,
  `notify.windows.com`), and the dialer refuses private addresses, so a
  subscription cannot make the server call arbitrary hosts. In test mode only,
  a loopback `http` endpoint is allowed for the browser suite's push service.
- Limits: 20 devices per member per workspace; subscribe and test routes are
  rate limited per user.

### VAPID keys

Generated automatically on first use (P-256), stored in `push_vapid_key` with
the private key sealed by the server master key (`LIDZA_MASTER_KEY` or
`config/master.key`), the same sealing setup uses for its secrets. Every node
and every restart reads the same row; concurrent first use is resolved by the
unique key name. No configuration is needed. Without a master key the
Notifications section says so and nothing is sent. Rotating the master key
without re-sealing makes the key unreadable: delete the row to generate a new
pair, and members turn notifications on again (old subscriptions are bound to
the old public key).

The VAPID contact (`sub`) is the operator contact email from setup
(`THURA_OPERATOR_EMAIL`), else `MAIL_FROM`, else `postmaster@` the `APP_URL`
host.

## Platform notes

- **Android** (Chrome, Edge, Firefox, Samsung Internet): works in the browser
  and installed. The badge icon is a white "t" on transparency.
- **iOS and iPadOS 16.4+**: Web Push works only for an app added to the Home
  Screen (Share → Add to Home Screen) and opened from there; Settings explains
  this in Safari. Each installed copy subscribes on its own.
- **Desktop** (Chrome, Edge, Firefox, Safari 16+): works in the browser; the
  operating system's notification settings must allow the browser.
- A browser may replace an expired subscription; the server learns of it by a
  404/410 and removes the old one. The member turns notifications on again in
  Settings.

## Not verified without real devices

The browser suite delivers to a local push service and checks encryption
headers, the VAPID signature header and size; delivery through FCM, Mozilla
autopush, Apple and WNS, the system notification itself, the notification tap,
and iOS Home Screen behaviour need a real device and a public deployment.
