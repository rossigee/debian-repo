package gpgsign

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// Signer handles GPG signing operations using go-crypto/openpgp
type Signer struct {
	entity        *openpgp.Entity
	keyPath       string // Path to the key file for GPG export
	keyInfo       *KeyInfo
	passphrase    string
	keepDecrypted bool
}

// NewSigner creates a new GPG signer from a key file
func NewSigner(keyPath, passphrase string, keepDecrypted bool) (*Signer, error) {
	// Read the armored private key
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Parse the key
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(keyData))
	if err != nil {
		return nil, fmt.Errorf("failed to parse key: %w", err)
	}

	if len(entities) == 0 {
		return nil, fmt.Errorf("no keys found in key file")
	}

	entity := entities[0]

	// Decrypt the key if needed
	if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
		if err := entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("failed to decrypt private key: %w", err)
		}
		// Also decrypt subkeys
		for _, subkey := range entity.Subkeys {
			if subkey.PrivateKey != nil && subkey.PrivateKey.Encrypted {
				if err := subkey.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
					return nil, fmt.Errorf("failed to decrypt subkey: %w", err)
				}
			}
		}
	}

	s := &Signer{
		entity:        entity,
		keyPath:       keyPath, // Store the key path for GPG export
		passphrase:    passphrase,
		keepDecrypted: keepDecrypted,
	}

	// Derive key info
	s.keyInfo = s.deriveKeyInfo()

	return s, nil
}

// DetachSign creates a detached ASCII-armored signature
func (s *Signer) DetachSign(data []byte) ([]byte, error) {
	if s.entity == nil || s.entity.PrivateKey == nil {
		return nil, fmt.Errorf("no private key available for signing")
	}

	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, s.entity, bytes.NewReader(data), nil); err != nil {
		return nil, fmt.Errorf("failed to create detached signature: %w", err)
	}

	return buf.Bytes(), nil
}

// ClearSign creates a clearsigned (inline) signature
func (s *Signer) ClearSign(data []byte) ([]byte, error) {
	if s.entity == nil || s.entity.PrivateKey == nil {
		return nil, fmt.Errorf("no private key available for signing")
	}

	var buf bytes.Buffer
	w, err := clearsign.Encode(&buf, s.entity.PrivateKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create clearsign writer: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write data for clearsigning: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close clearsign writer: %w", err)
	}

	return buf.Bytes(), nil
}

// KeyInfo returns information about the signing key
func (s *Signer) KeyInfo() *KeyInfo {
	return s.keyInfo
}

// deriveKeyInfo extracts key information from the entity
func (s *Signer) deriveKeyInfo() *KeyInfo {
	ki := &KeyInfo{
		KeyID:       fmt.Sprintf("%X", s.entity.PrimaryKey.KeyId),
		Fingerprint: fmt.Sprintf("%X", s.entity.PrimaryKey.Fingerprint[:]),
		CreatedAt:   time.Now(), // go-crypto doesn't expose creation time, use now
	}

	// Collect user IDs
	for _, id := range s.entity.Identities {
		ki.UIDs = append(ki.UIDs, id.Name)
	}

	// Export public key by re-reading from file and writing packet stream natively
	// This preserves all metadata: key, user IDs, signatures, subkeys
	if keyData, err := os.ReadFile(s.keyPath); err == nil {
		entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(keyData))
		if err == nil && len(entities) > 0 {
			entity := entities[0]
			var buf bytes.Buffer
			armorWriter, err := armor.Encode(&buf, "PGP PUBLIC KEY BLOCK", nil)
			if err == nil {
				// Write primary key packet
				_ = entity.PrimaryKey.Serialize(armorWriter)

				// Write all identities with their signatures
				for _, identity := range entity.Identities {
					if identity.UserId != nil {
						_ = identity.UserId.Serialize(armorWriter)
					}
					if identity.SelfSignature != nil {
						_ = identity.SelfSignature.Serialize(armorWriter)
					}
					for _, sig := range identity.Signatures {
						_ = sig.Serialize(armorWriter)
					}
				}

				// Write all subkeys with their signatures
				for _, subkey := range entity.Subkeys {
					_ = subkey.PublicKey.Serialize(armorWriter)
					if subkey.Sig != nil {
						_ = subkey.Sig.Serialize(armorWriter)
					}
					for _, sig := range subkey.Revocations {
						_ = sig.Serialize(armorWriter)
					}
				}

				// Write any entity-level revocations
				for _, sig := range entity.Revocations {
					_ = sig.Serialize(armorWriter)
				}

				_ = armorWriter.Close()
				ki.ArmoredPublicKey = buf.String()
			}
		}
	}

	return ki
}
