// Package gpgtest provides throwaway PGP keys for tests.
//
// The key is generated on demand rather than committed. Committing a private key
// to a public repository puts it in permanent history, trips GitHub secret
// scanning (enabled on this repo along with push protection), and violates the
// repository's own pre-commit hook, which rejects any staged *.asc, *.gpg,
// *.key or *.pem file.
package gpgtest

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Filename is the name the generated key is written under. It deliberately does
// not use a .asc extension outside a temp directory, so a stray copy can never be
// mistaken for a fixture that belongs in the repository.
const Filename = "gpg-test-key"

// keyConfig selects Ed25519 rather than the library default of RSA-2048.
// Signature and key generation for Ed25519 is effectively free, so a fresh key
// per test costs nothing, while RSA-2048 would add a noticeable stall to every
// test that needs to sign something.
func keyConfig() *packet.Config {
	return &packet.Config{
		Algorithm: packet.PubKeyAlgoEdDSA,
		Curve:     packet.Curve25519,
	}
}

// KeyPath generates a throwaway, unencrypted PGP key and returns the path to it.
// The key lives under t's temp directory, so it is removed when the test and its
// subtests finish. It carries no passphrase, which is what gpgsign.NewSigner
// expects when called with an empty passphrase.
//
// The identity is fixed so that failures are reproducible; the key material is
// not, so each test gets a distinct fingerprint.
func KeyPath(t *testing.T) string {
	t.Helper()

	cfg := keyConfig()
	entity, err := openpgp.NewEntity("Test User", "", "test@example.com", cfg)
	if err != nil {
		t.Fatalf("gpgtest: generating key: %v", err)
	}

	var armored bytes.Buffer
	w, err := armor.Encode(&armored, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("gpgtest: starting armor: %v", err)
	}
	if err := entity.SerializePrivate(w, cfg); err != nil {
		t.Fatalf("gpgtest: serializing key: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gpgtest: finishing armor: %v", err)
	}

	path := filepath.Join(t.TempDir(), Filename)
	if err := os.WriteFile(path, armored.Bytes(), 0o600); err != nil {
		t.Fatalf("gpgtest: writing key: %v", err)
	}
	return path
}
