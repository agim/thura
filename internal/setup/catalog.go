package setup

import "os"

// Only this allowlist can enter the published server configuration.
type Field struct {
	Name, Label, Kind, Default, Help string
	Options                          []string
	Value                            string
	Saved                            bool
	Managed                          bool
	ManagedTitle                     string
}
type Step struct {
	ID, Title string
	Fields    []Field
}

func field(name, label, kind, def, help string, options ...string) Field {
	return Field{Name: name, Label: label, Kind: kind, Default: def, Help: help, Options: options}
}
func Catalog() []Step {
	return []Step{
		{ID: "server", Title: "Server and workspace", Fields: []Field{
			field("APP_URL", "Public HTTPS address", "url", "", "The address users open; no fixed Thura domain."),
			field("THURA_WORKSPACE_NAME", "Initial workspace name", "text", "My workspace", "Created with the administrator as owner on first publish."),
			field("THURA_OPERATOR_EMAIL", "Operator contact email", "email", "", "For deployment and incident ownership."),
			field("THURA_TIMEZONE", "Default server timezone", "text", "UTC", "IANA timezone; calendar users keep their own timezone."),
			field("THURA_BACKUP_PLAN", "Backup and retention plan", "textarea", "", "Describe protected database, object, provider and separate master-key backups; no automatic backup or deletion is implied."),
		}},
		{ID: "mail", Title: "Email service", Fields: []Field{
			field("THURA_MAIL_INBOUND", "Receive email through", "select", "relay", "Postfix uses the included Compose service. Relay keeps an independently managed signed MIME relay.", "postfix", "relay"),
			field("THURA_MAIL_OUTBOUND", "Send email through", "select", "provider", "Recommended: an API provider for outgoing delivery. Postfix remains available for direct SMTP sending.", "provider", "postfix"),
			field("THURA_POSTFIX_HOSTNAME", "Mail server hostname", "text", "", "For example mail.example.org. Point your inbox domain MX record here and this hostname A record at this server."),
			field("THURA_MAIL_ADDRESS", "Administrator inbox address", "email", "", "Private inbox for the first administrator; blank uses the sender address. New members receive personal inboxes through invitations."),
			field("MAIL_PROVIDER", "Outgoing provider", "select", "smtp", "API providers are recommended: Mailgun, SendGrid, Postmark or Resend. SMTP is available when needed.", "mailgun", "sendgrid", "postmark", "resend", "smtp"),
			field("MAIL_FROM", "Sender address", "email", "", "A verified outgoing sender; can differ from the inbox address."),
			field("MAIL_API_KEY", "Email API key", "secret", "", "Required for API providers. Blank preserves the saved key."),
			field("MAIL_DOMAIN", "Mailgun sending domain", "text", "", "Verify this domain in the provider console."),
			field("MAIL_REGION", "Email API region", "select", "us", "Select the provider's regional endpoint.", "us", "eu"),
			field("MAIL_SMTP_HOST", "SMTP hostname", "text", "", "For SMTP; localhost is allowed for a local Postfix relay."),
			field("MAIL_SMTP_PORT", "SMTP port", "number", "587", "1–65535."),
			field("MAIL_SMTP_SECURITY", "SMTP encryption", "select", "starttls", "Plain transport is allowed only to loopback Postfix.", "starttls", "tls", "none"),
			field("MAIL_SMTP_USERNAME", "SMTP username", "text", "", "Leave both username/password empty for a trusted local relay."),
			field("MAIL_SMTP_PASSWORD", "SMTP password", "secret", "", "Blank preserves the saved password."),
			field("MAIL_INBOUND_SECRET", "Inbound relay signing secret", "secret", "", "At least 32 characters. Automatically generated for managed Postfix. For an external relay, configure the same secret there."),
			field("MAIL_RSPAMD_URL", "Local spam filter", "text", "", "Compose supplies http://127.0.0.1:11333. Local rules and learned Junk corrections run before optional AI screening."),
			field("MAIL_RSPAMD_CONTROLLER_URL", "Spam filter learning endpoint", "text", "", "Compose supplies http://127.0.0.1:11334 alongside Postfix. Never publish the controller port."),
			field("MAIL_RSPAMD_PASSWORD", "Spam filter learning secret", "secret", "", "Compose supplies this automatically. Blank preserves the existing controller secret."),
			field("MAIL_AI_SCREENING", "AI-screen incoming email by default", "select", "true", "New workspace policies screen incoming messages using the model selected in AI settings. Unavailable required checks keep messages in quarantine. Workspace owners can turn screening off in Mail settings.", "true", "false"),
			field("MAIL_AI_PER_HOUR", "AI screening attempts per workspace per hour", "number", "120", "Bounds automatic model usage. Mail over this budget stays quarantined and is retried in the next hour."),
			field("MAIL_CLAMD_ADDRESS", "Private antivirus service", "text", "", "Host:port for the included ClamAV service, for example clamav:3310. Keep its port private. Blank disables signature scanning; static attachment checks still run."),
			field("SMIP_MAIL_ENABLED", "Prefer verified SMIP email", "select", "false", "Verified domains use authenticated SMIP. Unadvertised domains use the configured provider; verification failures hold delivery.", "false", "true"),
			field("SMIP_MAIL_CONFIG", "SMIP email identity", "secret", "", "Thura generates and encrypts the signing key when you save this step. Existing keys are preserved."),
		}},
		{ID: "storage", Title: "Private file storage", Fields: []Field{
			field("STORAGE_PROVIDER", "Storage backend", "select", "local", "Private local disk or an S3-compatible backend.", "local", "s3"),
			field("STORAGE_DIR", "Local storage directory", "text", "storage", "Persistent private directory owned by the app's system user."),
			field("STORAGE_ENDPOINT", "S3 HTTPS endpoint", "url", "https://s3.amazonaws.com", "Explicit bucket service origin; private objects. AWS uses virtual hosts; other endpoints use path-style requests."),
			field("STORAGE_REGION", "S3 region", "text", "us-east-1", "For AWS or your compatible service."),
			field("STORAGE_BUCKET", "Private bucket", "text", "", "Pre-create the bucket and restrict the key to the required bucket/prefix."),
			field("STORAGE_PREFIX", "Installation object prefix", "text", "", "Fresh installations receive a unique thura/<installation-id>/ prefix. Keep it when sharing a bucket; changing a published location requires an offline migration."),
			field("STORAGE_ACCESS_KEY", "S3 access key", "secret", "", "Never returned in setup responses."),
			field("STORAGE_SECRET_KEY", "S3 secret key", "secret", "", "Never returned in setup responses."),
		}},
		{ID: "policy", Title: "Quotas and access", Fields: []Field{
			field("DRIVE_QUOTA_BYTES", "Drive bytes per workspace", "number", "1073741824", "1 MiB–1 TiB; includes retained versions and active reservations."),
			field("THURA_MAX_MEMBERS", "Members and pending invitations per workspace", "number", "100", "1–1000, checked again during invitation acceptance."),
			field("THURA_ADMIN_INVITES", "Allow workspace admins to invite members", "select", "true", "Owners retain invitation authority.", "true", "false"),
			field("THURA_ALLOW_SHARES", "Allow external file sharing", "select", "true", "Disabling blocks existing external grants as well as new grants.", "true", "false"),
			field("THURA_ALLOW_ANONYMOUS_SHARES", "Allow anonymous file links", "select", "false", "Email-restricted shares still require the matching signed-in account.", "false", "true"),
			field("MAIL_MAX_RECIPIENTS", "Maximum recipients per email", "number", "50", "1–50 across To, Cc and Bcc."),
		}},
		{ID: "ai", Title: "AI service", Fields: []Field{
			field("THURA_AI_ENABLED", "AI assistance", "select", "false", "AI is a core service and must be enabled before publishing setup.", "true", "false"),
			field("LLM_PROVIDER", "AI provider", "select", "", "Use a local model service or a hosted API provider.", "", "ollama", "openai", "anthropic", "google", "compatible"),
			field("LLM_MODEL", "Model name", "text", "", "The model ID installed locally or available to your provider account."),
			field("LLM_BASE_URL", "Model service URL", "text", "", "Use the provider default or a server reachable from Thura. Container localhost is not the host machine."),
			field("LLM_API_KEY", "Model API key", "secret", "", "Required for hosted providers. Blank preserves the saved key."),
			field("LLM_MAX_TOKENS", "Maximum output tokens", "number", "1024", "128–4096 output tokens per request."),
		}},
		{ID: "integrations", Title: "Chat, documents, meetings and SMIP", Fields: []Field{
			field("MATRIX_SERVER_URL", "Matrix HTTPS URL", "url", "", "Required for chat; configure the application service on the homeserver."),
			field("MATRIX_SERVER_NAME", "Matrix server identity", "text", "", "Homeserver DNS identity."),
			field("MATRIX_AS_TOKEN", "Matrix application-service token", "secret", "", "Required when Matrix is enabled."),
			field("COLLABORA_SERVER_URL", "Collabora server URL", "url", "", "Public services require HTTPS; deployment-managed private origins are preconfigured. Use service root /api/v1/collabora and permit this Thura installation as a WOPI host."),
			field("COLLABORA_APP_URL", "Collabora-accessible Thura URL", "url", "", "Thura address reachable by the document server. Public origins require HTTPS; deployment-managed private origins are preconfigured."),
			field("COLLABORA_TOKEN_SECRET", "Collabora access-token secret", "secret", "", "At least 32 characters; used by Thura to sign file-scoped access."),
			field("LIVEKIT_SERVER_URL", "LiveKit server URL", "url", "", "Public services require HTTPS; deployment-managed private origins are preconfigured. TURN and device checks remain deployment tasks."),
			field("LIVEKIT_PUBLIC_URL", "LiveKit public WSS URL", "text", "", "Browser-reachable secure WebSocket endpoint."),
			field("LIVEKIT_API_KEY", "LiveKit API key", "secret", "", "Required with LiveKit."),
			field("LIVEKIT_API_SECRET", "LiveKit API secret", "secret", "", "Required with LiveKit."),
			field("SMIP_ENABLED", "SMIP chat and files", "select", "false", "Required before publishing. Thura generates this server’s identity; pair trusted servers explicitly when enabling cross-server exchange.", "false", "true"),
			field("SMIP_CONFIG", "Secure chat and file identity", "secret", "", "Generated and encrypted by Thura. Remote peers still require explicit pairing and consent; no remote server is trusted automatically."),
		}},
		{ID: "review", Title: "Review and publish"},
	}
}
func defaults() map[string]string {
	out := map[string]string{}
	for _, s := range Catalog() {
		for _, f := range s.Fields {
			out[f.Name] = f.Default
		}
	}
	return out
}
func stepByID(id string) (Step, bool) {
	for _, s := range Catalog() {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// New and reset wizard drafts recommend HTTP API delivery. Snapshot defaults
// stay unchanged so adding this preference never switches an existing server.
func draftDefaults() map[string]string {
	v := defaults()
	v["MAIL_PROVIDER"] = "mailgun"
	v["THURA_AI_ENABLED"] = "true"
	v["LLM_PROVIDER"] = "ollama"
	v["SMIP_ENABLED"] = "true"
	v["SMIP_MAIL_ENABLED"] = "true"
	return v
}

// Bootstrap uses explicit operator environment values, not browser input. This
// keeps managed infrastructure and the encrypted wizard snapshot in agreement.
func bootstrapDraft() map[string]string {
	values := draftDefaults()
	for _, step := range Catalog() {
		for _, field := range step.Fields {
			if value, present := os.LookupEnv(field.Name); present {
				values[field.Name] = value
			}
		}
	}
	fillManaged(values)
	localModelDefaults(values)
	return values
}

// A fresh random installation ID is generated on first-account creation and
// persisted in the encrypted draft. The singleton setup row ID is not unique
// across databases. Existing snapshots retain their original object keys.
func installationDraft(id string) map[string]string {
	values := bootstrapDraft()
	if _, explicit := os.LookupEnv("STORAGE_PREFIX"); !explicit {
		values["STORAGE_PREFIX"] = "thura/" + id + "/"
	}
	return values
}
