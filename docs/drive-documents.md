# Drive documents and conversion

Drive embeds a self-hosted ONLYOFFICE Docs service on Thura's authenticated origin. It does not send files to a public document converter. The original file and its immutable versions remain in private Drive storage.

| File family | Read inside Drive | Edit and save |
| --- | --- | --- |
| DOCX, XLSX, PPTX | Embedded editor | Save creates a new version of the same file |
| PDF | Embedded PDF editor | Save PDF changes, or Convert to Word and save a separate DOCX |
| DOC, ODT, RTF, TXT, Markdown, EPUB, Pages and document templates | Embedded viewer | Convert to Word, then save/edit a DOCX copy |
| XLS, ODS, CSV, TSV, Numbers and spreadsheet templates | Embedded viewer | Convert to Excel, then save/edit an XLSX copy |
| PPT, ODP, Keynote and presentation templates | Embedded viewer | Convert to PowerPoint, then save/edit a PPTX copy |
| HTML | Existing Thura rich text editor | Save creates a new HTML version |

Import fidelity depends on the document service and source file. Password-protected, malformed or unsupported files report a failure instead of pretending to convert. Legacy files remain read-only until converted: DOCX bytes must never be saved under a DOC filename. Macros are disabled in the editor configuration. Existing session signatures, current membership, revocation, quota checks and optimistic version checks apply to saves.

## Opening files

Double-click a file or press Enter to open its full-screen editor on desktop. The X returns to Drive browse. Mobile/coarse-pointer devices open a read-only rendering with a signed viewer configuration. HTML and text render directly; Office documents use the embedded viewer. Right-click or Shift+F10 opens Thura’s shared resource menu; File details keeps labels, history, sharing and management available.

## PDF to Word

Open a PDF’s **File details**, then **Manage file → Convert to Word**. Thura polls the document service's conversion operation, downloads its result only from the configured service and checks that the source version has not changed. **Save Word copy** creates a separate editable DOCX; it never overwrites the PDF. Check layout after conversion.

For scans, choose **Manage file → Extract scanned PDF**. A bounded worker uses Poppler to render the pages and Tesseract to recognize English text locally. Review and correct the text, optionally choose **Reconstruct with AI**, then save a Word copy. The AI receives the reviewed recognized text through the official Līdza LLM pack. It does not currently inspect page images: Līdza v0.1.123 has a text-only Message interface. AI reconstruction preserves supplied facts and marks uncertainty, but users must verify names, numbers and tables. An incomplete model response is refused rather than replacing the reviewed text.

Recognition supports PDF uploads up to the existing 10 MiB upload limit, at most 12 pages, a 1,800-pixel longest page dimension and 64 KiB of recognized text. Worker scratch files are private and removed on return. Durable previews use the storage pack, with the same file/version and workspace access checks as other previews. AI requests accept at most 20,000 characters; split longer documents or save the recognized text without AI. Reconstruction produces editable paragraphs, not a replica of the PDF's page geometry.

## Operator setup

Install ONLYOFFICE Docs and set its JWT secret to the same value used by Thura. Configure the three existing Office fields in server setup, publish and restart the application:

- `OFFICE_SERVER_URL`: the fixed document-service origin.
- `OFFICE_APP_URL`: the Thura origin reachable by the service for signed source downloads and save callbacks.
- `OFFICE_JWT_SECRET`: a private secret of at least 32 characters.

Normal deployments use HTTPS origins. An optional `OFFICE_CA_FILE` environment setting supplies a PEM trust bundle for a private certificate authority; TLS verification remains enabled. The document service separately needs to trust Thura's certificate when downloading sources. The local Wi-Fi demonstration uses the existing trusted demo certificate and a private VM-to-host callback address.

Browser assets and sockets pass through `/api/v1/office/editor/` behind Thura sign-in. The proxy strips application cookies and Authorization before contacting the fixed document server, preserves the Office virtual path and permits framing only by the same origin. Signed save callbacks may contain a proxy cache URL; only that cache subtree is mapped back to the fixed document service. Foreign URLs, redirects and traversal are rejected.

For recognition, install Poppler (`pdfinfo`, `pdftoppm`, `pdftotext`) and Tesseract with English language data on every worker host. Configure AI separately in Settings. No paid provider is required; the local demo uses Ollama. ONLYOFFICE’s dedicated mobile web editor requires Enterprise or Developer edition. The local Community demonstration therefore uses the read-only embedded renderer; do not put the desktop ribbon on a phone. See https://api.onlyoffice.com/docs/docs-api/get-started/how-it-works/mobile-integration.
