// Package gpgsign provides GPG signing capabilities for Debian Release metadata.
package gpgsign

import "time"

// KeyInfo holds information about the GPG key
// Derived dynamically from the actual key, fixing the hardcoded-fingerprint bug
type KeyInfo struct {
	KeyID            string    // Hex-encoded key ID
	Fingerprint      string    // Hex-encoded fingerprint
	UIDs             []string  // User IDs
	CreatedAt        time.Time // Key creation time
	ExpiresAt        time.Time // Key expiration (zero if no expiry)
	ArmoredPublicKey string    // Full armored public key block
}
