# AI assistance

Open **Settings → AI** as the server administrator. Choose Ollama, OpenAI,
Anthropic, Google Gemini, or an OpenAI-compatible service. Enter a model ID
available to that service, save the configuration, then use **Test saved
connection**. Enable assistance when the connection works. The test makes a
small real model request; hosted providers can charge for it.

For Ollama, install a model on the machine running Ollama and use its installed
model name. The default service address is `http://127.0.0.1:11434`. That address
is resolved by the Thura server, not by the user's phone. A remote Ollama server
can use a different service URL. Hosted providers use their normal service
address when the URL is blank and require a key. Blank key fields preserve the
current provider's saved key; changing provider clears the previous provider's
key unless a replacement is supplied. Keys are encrypted in server settings
and never returned to browsers. Process environment variables override saved
settings; conflicting saves explain that deployment configuration must change.

The shared **AI** action offers drafts, rewrites, summaries and next steps for
visible mail, calendar descriptions, contacts, Loop activity, supported Drive
text/documents and loaded chat messages. Meet can prepare an agenda from the
meeting name and text you provide; it does not record or transcribe a call.
PDFs and office documents are not silently extracted or uploaded to a model.

Review and edit **Text included** before generating. Only that text and your
instructions go to the configured provider. Requests are limited to 20,000 input
characters and a configurable output limit. Generated text is a suggestion:
copy it or explicitly insert it. Mail and chat never send automatically;
calendar changes still need saving; Drive suggestions append to the open
native document and still require saving a version. Model output is rendered
as text. Verify facts before using it.

AI starts disabled. Fake model replies are available only in tests. Requests
require workspace membership, have a rate limit and timeout, and expose no
model tools for retrieving other resources or changing data. Token usage is
recorded by the Līdza LLM pack under each feature. There is one server-wide
model configuration; a multi-node deployment must restart other nodes after a
configuration change so they load the encrypted saved settings.
