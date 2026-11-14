package app

import "mime"

// The web app manifest (public/manifest.webmanifest) is served from the
// frontend build; Go's built-in table has no type for its extension, and a
// browser expects application/manifest+json.
func init() {
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		panic(err)
	}
}
