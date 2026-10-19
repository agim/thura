package correspondence

import (
	"net/mail"
	"strings"
)

// Matches uses mailbox identities, never display names or substring matches.
// Malformed headers are deliberately not inferred to identify a person.
func Matches(emails map[string]bool, headers ...string) bool {
	for _, header := range headers {
		addresses, err := mail.ParseAddressList(header)
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if emails[strings.ToLower(address.Address)] {
				return true
			}
		}
	}
	return false
}
