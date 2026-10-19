# Drive documents and conversion

Drive uses self-hosted Collabora CODE on Thura's authenticated origin. See [Collabora setup and deployment](collabora.md). Files and immutable versions remain in private Drive storage.

| File family | Read inside Drive | Edit and save |
| --- | --- | --- |
| DOCX, XLSX, PPTX, ODT, ODS, ODP, ODG and other advertised writable formats | Collabora Writer, Calc, Impress or Draw | Save creates a new version in the original file format |
| PDF | Collabora PDF viewer | Read-only; reviewed extraction can create a separate Word copy |
| Advertised view-only imports (including supported legacy, Visio, Publisher and Apple formats) | Collabora viewer | Conversion to a supported editable copy when available |
| HTML | Thura rich text editor | Save creates a new HTML version |
| Images | Native Thura renderer | View only |

Drive discovers supported extensions and MIME types from the installed Collabora server. Exact edit/view capabilities determine writable access. Not every desktop Collabora Office format is available online; unadvertised formats report an error or use the existing native renderer. Macros and arbitrary source fidelity are not established by the demo tests.

## Opening files

Double-click a file or press Enter to open its full-screen editor on desktop. The X returns to Drive browse. Phones use Collabora's slim mobile web interface: tap the pencil to edit. Right-click or Shift+F10 opens Thura's shared resource menu; File details keeps labels, version history, sharing and management available. There is no separate Edit action button.

## PDF to Word

Open a PDF’s **File details**, then **Manage file → Convert to Word**. Thura sends source bytes only to the configured private Collabora service and checks the source version again before returning a converted copy. This CODE version rejected the PDF-to-DOCX demo fixture; use the reviewed extraction workflow below if conversion is unavailable. **Save Word copy** creates a separate editable DOCX; it never overwrites the PDF. Check layout after conversion.

For scans, choose **Manage file → Extract scanned PDF**. A bounded worker uses Poppler to render the pages and Tesseract to recognize English text locally. Review and correct the text, optionally choose **Reconstruct with AI**, then save a Word copy. The AI receives the reviewed recognized text through the official Līdza LLM pack. It does not currently inspect page images: Līdza v0.1.123 has a text-only Message interface. AI reconstruction preserves supplied facts and marks uncertainty, but users must verify names, numbers and tables. An incomplete model response is refused rather than replacing the reviewed text.

Recognition supports PDF uploads up to the existing 10 MiB upload limit, at most 12 pages, a 1,800-pixel longest page dimension and 64 KiB of recognized text. Worker scratch files are private and removed on return. Durable previews use the storage pack, with the same file/version and workspace access checks as other previews. AI requests accept at most 20,000 characters; split longer documents or save the recognized text without AI. Reconstruction produces editable paragraphs, not a replica of the PDF's page geometry.

## Operator setup

Configure Collabora through [server setup](collabora.md), publish and restart. The document service requires a pinned image, an exact WOPI allowlist, the `/api/v1/collabora` service root and a trusted HTTPS/public application origin. Thura proxies browser assets and sockets on the authenticated origin and strips application credentials before forwarding. File-scoped grants and persistent locks protect saves, quotas, version history and current membership.

Install Poppler (`pdfinfo`, `pdftoppm`, `pdftotext`) and Tesseract with English language data on every recognition worker host. Configure AI separately in Settings; Ollama can run locally.
