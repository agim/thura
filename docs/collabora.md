# Collabora in Drive

Collabora CODE is Thura's sole document service, with desktop and slim mobile web editing. It runs as a separate container; the application includes the WOPI integration and deployment configuration. Installations must configure Collabora before opening Office files. Existing Drive files and versions do not require conversion or migration. The previous document service and its settings, routes, browser code and container are removed.

## Deployment

For this provider-removal upgrade, stop all old Thura nodes before applying migrations and start the new binary afterward. The small Office session migration removes obsolete provider bookkeeping; it does not touch Drive files, file versions or object bytes. This is a maintenance deployment, not a mixed-version rolling upgrade.

The default [compact deployment](compact-deployment.md) includes a bounded Collabora container. Run a separately maintained official Collabora image. `scripts/collabora/compose.yaml` is a loopback/private-network service example; set `COLLABORA_IMAGE` to a tested digest for the host architecture and `THURA_WOPI_ORIGIN` to the exact origin Collabora can reach. It sets the required `/api/v1/collabora` service root, an explicit WOPI allowlist and disables the administrator console. The distroless image takes command-line settings, not the historical shell entrypoint's `extra_params` or `aliasgroup1` variables. Stock health checks omit the service root; externally monitor the prefixed discovery URL.

Configure these fields under server setup, publish and restart every app node:

- `COLLABORA_SERVER_URL`: fixed private Collabora origin, with no path.
- `COLLABORA_APP_URL`: Thura origin reachable from Collabora for WOPI requests, with no path. Match the allowlisted origin exactly.
- `COLLABORA_TOKEN_SECRET`: a private random secret of at least 32 characters, stored through encrypted settings. Only Thura needs this signing secret.

Public app traffic uses HTTPS. The example container is HTTP on loopback behind Thura's authenticated same-origin proxy; use a private container network or TLS for a remote service. Optional infrastructure `COLLABORA_CA_FILE` supplies a PEM trust bundle for a private CA; verification stays enabled. Collabora separately must trust Thura's certificate. Set appropriate proxy timeouts and permit WebSocket upgrades. Do not expose the private Collabora port directly to the Internet. In a multi-node deployment, all nodes share the database, token secret and private object storage, and route a document consistently to the same Collabora instance.

The browser POSTs its eight-hour file-scoped token to a same-origin iframe. App cookies/Authorization are stripped before proxying assets and WebSockets. The proxy forwards only the editor's browser assets, WOPI discovery/capabilities and the document WebSocket; every other Collabora endpoint (converters, thumbnails, link extraction, the admin console) answers 404 because Collabora trusts Thura's own address. WOPI requests verify the token audience, expiry, file binding, current membership, trash state and read-only mode. Saves require the current document lock and base version. New versions are immutable, bounded by the 10 MiB upload limit, retained-version limit and workspace quota. An identical repeated save creates no additional version. Upload completion refuses to overwrite an active Collabora lock. Locks refresh for 30 minutes, and prior versions remain downloadable. A disconnected editor can retain already fetched data; membership removal blocks subsequent reads and saves.

## Formats

Drive reads the installed server's discovery document for extensions, MIME types and edit/view actions. It does not assume the desktop Collabora Office format list is identical to Collabora Online. Exact extension actions take precedence over a generic MIME match; view-only imports never receive writable grants. Unsupported formats show an error. Native images and HTML keep their existing Thura renderers/editor.

The tested CODE 26.04.5.1 advertises Writer, Calc, Impress and Draw formats: DOCX/DOC/DOCM/RTF/ODT/FODT/OTT, XLSX/XLS/XLSM/XLSB/ODS/FODS/OTS/CSV/TSV, PPTX/PPT/PPTM/ODP/FODP/OTP and ODG/FODG, plus template formats and additional legacy read-only imports including Pages, Numbers, Keynote, Visio and Publisher. Discovery also advertises MIME fallbacks for supported formats such as older Excel/PowerPoint templates. Being advertised does not establish fidelity for every source document or macro behavior. A writable import must save in its original format; conversion deliberately creates a separate editable copy.

PDF uses Collabora's `view_comment` discovery action as a read-only viewer. This version's PDF-to-DOCX conversion rejected the real demo fixture; do not claim a faithful direct conversion. **Manage file → Extract scanned PDF** remains available for local recognition, review, optional AI reconstruction and a separate Word copy. Other supported conversions use Collabora's private conversion endpoint, with bounded uploads/downloads and a final source-version check. All document conversions use Collabora; there is no fallback document service.

## Demo verification

The ARM64 demo pins `collabora/code@sha256:b4cc44e40658d662275bb955c683a254c86148165c587f881711ad4924b4be6b` (26.04.5.1). Desktop tests typed real DOCX text and changed XLSX B2 to 12; downloaded files retained the table and `=B2*C2`, with calculated result 300. The user tested the physical Android on trusted Wi-Fi HTTPS: Word edits reached version 4; Excel B3=7 reached version 3, preserving `=B3*C3` and cached result 70. Browser screenshots at Android dimensions are labeled as browser emulation, not physical-device captures. The dedicated real-service browser suite also passes both DOCX and XLSX tests: upload, edit, save, version-history checks, download, and inspection of persisted document XML. These fixtures establish the exercised workflows, not universal Office fidelity.

CODE is the development/community distribution. Keep its notices and review the licenses shipped in the exact image when redistributing. The default connector does not imply a production support subscription, production capacity guarantee, or trademark/rebranding rights. Operators may separately choose supported Collabora builds. See the [official CODE project](https://www.collaboraonline.com/code/) and [upstream source and licenses](https://github.com/CollaboraOnline/online.mirror).
