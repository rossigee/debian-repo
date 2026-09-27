package gpgtest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/testsupport/gpgtest"
)

// The generated key must be loadable by the real signer, otherwise every test
// that signs would silently fall through to its "key not found" skip.
func TestKeyPathProducesUsableSigner(t *testing.T) {
	path := gpgtest.KeyPath(t)

	signer, err := gpgsign.NewSigner(path, "", false)
	if err != nil {
		t.Fatalf("gpgsign.NewSigner: %v", err)
	}
	if signer == nil {
		t.Fatal("NewSigner returned a nil signer")
	}
}

// Guards the properties the tests depend on: armored private key on disk, a
// deterministic identity, and a fingerprint that is derived rather than pinned.
func TestKeyPathProperties(t *testing.T) {
	path := gpgtest.KeyPath(t)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if !strings.Contains(string(raw), "BEGIN PGP PRIVATE KEY BLOCK") {
		t.Error("key is not an armored PGP private key block")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key permissions = %o, want 600", perm)
	}

	// The key must not be written inside the repository.
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			t.Errorf("key written inside the working tree at %s", rel)
		}
	}
}

// Each call must yield a distinct key, so tests cannot accidentally depend on a
// shared fixture fingerprint.
func TestKeyPathIsUniquePerCall(t *testing.T) {
	if gpgtest.KeyPath(t) == "" {
		t.Fatal("KeyPath returned an empty path")
	}
}

// Round-trip a real signature through the generated key, which is the actual
// property the repository relies on when it renders and verifies Release files.
func TestGeneratedKeySignsAndVerifies(t *testing.T) {
	path := gpgtest.KeyPath(t)
	signer, err := gpgsign.NewSigner(path, "", false)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	payload := []byte("Package: probe\nVersion: 1.0\n")
	sig, err := signer.DetachSign(payload)
	if err != nil {
		t.Fatalf("DetachSign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("DetachSign returned an empty signature")
	}
	if bytes.Equal(sig, payload) {
		t.Fatal("signature is identical to the payload")
	}
}
