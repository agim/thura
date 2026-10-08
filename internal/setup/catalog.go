package setup

// Only this allowlist can enter the published server configuration.
type Field struct {
	Name, Label, Kind, Default, Help string
	Options                          []string
	Value                            string
	Saved                            bool
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
			field("MAIL_PROVIDER", "Email provider", "select", "smtp", "SMTP includes Debian Postfix; other choices use the official mail pack.", "smtp", "mailgun", "sendgrid", "postmark", "resend"),
			field("MAIL_FROM", "Sender address", "email", "", "A verified sender; creates the initial shared mailbox."),
			field("MAIL_API_KEY", "Email API key", "secret", "", "Required for API providers. Blank preserves the saved key."),
			field("MAIL_DOMAIN", "Mailgun sending domain", "text", "", "Verify this domain in the provider console."),
			field("MAIL_REGION", "Email API region", "select", "us", "Select the provider's regional endpoint.", "us", "eu"),
			field("MAIL_SMTP_HOST", "SMTP hostname", "text", "", "For SMTP; localhost is allowed for a local Postfix relay."),
			field("MAIL_SMTP_PORT", "SMTP port", "number", "587", "1–65535."),
			field("MAIL_SMTP_SECURITY", "SMTP encryption", "select", "starttls", "Plain transport is allowed only to loopback Postfix.", "starttls", "tls", "none"),
			field("MAIL_SMTP_USERNAME", "SMTP username", "text", "", "Leave both username/password empty for a trusted local relay."),
			field("MAIL_SMTP_PASSWORD", "SMTP password", "secret", "", "Blank preserves the saved password."),
			field("MAIL_INBOUND_SECRET", "Inbound relay signing secret", "secret", "", "At least 32 characters. Configure the raw-MIME relay separately; selecting an outbound provider does not configure its inbound webhooks."),
		}},
		{ID: "storage", Title: "Private file storage", Fields: []Field{
			field("STORAGE_PROVIDER", "Storage backend", "select", "local", "Private local disk or an S3-compatible backend.", "local", "s3"),
			field("STORAGE_DIR", "Local storage directory", "text", "storage", "Persistent private directory owned by the app's system user."),
			field("STORAGE_ENDPOINT", "S3 HTTPS endpoint", "url", "https://s3.amazonaws.com", "Explicit bucket service origin; private objects. AWS uses virtual hosts; other endpoints use path-style requests."),
			field("STORAGE_REGION", "S3 region", "text", "us-east-1", "For AWS or your compatible service."),
			field("STORAGE_BUCKET", "Private bucket", "text", "", "Pre-create the bucket and restrict the key to the required bucket/prefix."),
			field("STORAGE_PREFIX", "Object prefix", "text", "", "Optional prefix within the bucket."),
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
		{ID: "integrations", Title: "Optional integrations", Fields: []Field{
			field("MATRIX_SERVER_URL", "Matrix HTTPS URL", "url", "", "Optional; configure the application service on the homeserver."),
			field("MATRIX_SERVER_NAME", "Matrix server identity", "text", "", "Homeserver DNS identity."),
			field("MATRIX_AS_TOKEN", "Matrix application-service token", "secret", "", "Required when Matrix is enabled."),
			field("OFFICE_SERVER_URL", "ONLYOFFICE HTTPS URL", "url", "", "Optional document server; configure the same JWT secret there."),
			field("OFFICE_APP_URL", "ONLYOFFICE-accessible Thura HTTPS URL", "url", "", "Usually the public Thura address."),
			field("OFFICE_JWT_SECRET", "ONLYOFFICE JWT secret", "secret", "", "Required when Office is enabled."),
			field("LIVEKIT_SERVER_URL", "LiveKit HTTPS API URL", "url", "", "Optional SFU; TURN and device checks remain deployment tasks."),
			field("LIVEKIT_PUBLIC_URL", "LiveKit public WSS URL", "text", "", "Browser-reachable secure WebSocket endpoint."),
			field("LIVEKIT_API_KEY", "LiveKit API key", "secret", "", "Required with LiveKit."),
			field("LIVEKIT_API_SECRET", "LiveKit API secret", "secret", "", "Required with LiveKit."),
			field("SMIP_ENABLED", "Enable experimental SMIP", "select", "false", "Requires explicit operator-paired configuration on both servers.", "false", "true"),
			field("SMIP_CONFIG", "SMIP pairing JSON", "secret", "", "Private signing seed, pinned peer keys and origins; stored encrypted."),
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
