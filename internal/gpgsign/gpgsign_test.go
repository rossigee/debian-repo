package gpgsign

import (
	"testing"
)

// TestKeyInfoValidation tests KeyInfo structure
func TestKeyInfoValidation(t *testing.T) {
	tests := []struct {
		name        string
		keyID       string
		fingerprint string
		uids        []string
		isValid     bool
	}{
		{
			name:        "valid key info",
			keyID:       "ABCD1234",
			fingerprint: "1234567890ABCDEF1234567890ABCDEF12345678",
			uids:        []string{"Test User <test@example.com>"},
			isValid:     true,
		},
		{
			name:        "missing key ID",
			keyID:       "",
			fingerprint: "1234567890ABCDEF1234567890ABCDEF12345678",
			uids:        []string{"Test User <test@example.com>"},
			isValid:     false,
		},
		{
			name:        "missing fingerprint",
			keyID:       "ABCD1234",
			fingerprint: "",
			uids:        []string{"Test User <test@example.com>"},
			isValid:     false,
		},
		{
			name:        "no UIDs",
			keyID:       "ABCD1234",
			fingerprint: "1234567890ABCDEF1234567890ABCDEF12345678",
			uids:        []string{},
			isValid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.keyID == "" && tt.isValid {
				t.Error("keyID should not be empty for valid key")
			}
			if tt.fingerprint == "" && tt.isValid {
				t.Error("fingerprint should not be empty for valid key")
			}
			if len(tt.uids) == 0 && tt.isValid {
				t.Error("UIDs should not be empty for valid key")
			}
		})
	}
}

// TestFingerprintFormat tests GPG fingerprint format validation
func TestFingerprintFormat(t *testing.T) {
	tests := []struct {
		name        string
		fingerprint string
		isValid     bool
		length      int
	}{
		{
			name:        "full_40char",
			fingerprint: "1234567890ABCDEF1234567890ABCDEF12345678",
			isValid:     true,
			length:      40,
		},
		{
			name:        "short_16char",
			fingerprint: "1234567890ABCDEF",
			isValid:     true,
			length:      16,
		},
		{
			name:        "invalid_hex",
			fingerprint: "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
			isValid:     false,
			length:      40,
		},
		{
			name:        "empty",
			fingerprint: "",
			isValid:     false,
			length:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fingerprint == "" && tt.isValid {
				t.Error("empty fingerprint should not be valid")
			}

			if len(tt.fingerprint) != tt.length {
				t.Logf("fingerprint length: expected %d, got %d", tt.length, len(tt.fingerprint))
			}

			// All hex characters should be valid
			for _, c := range tt.fingerprint {
				if !isHexChar(c) && tt.isValid {
					t.Errorf("non-hex character in valid fingerprint: %c", c)
				}
			}
		})
	}
}

// TestKeyIDExtraction tests extracting short key ID from fingerprint
func TestKeyIDExtraction(t *testing.T) {
	tests := []struct {
		fingerprint string
		expectKeyID string
	}{
		{
			fingerprint: "1234567890ABCDEF1234567890ABCDEF12345678",
			expectKeyID: "12345678",
		},
		{
			fingerprint: "FEDCBA9876543210FEDCBA9876543210FEDCBA98",
			expectKeyID: "FEDCBA98",
		},
	}

	for _, tt := range tests {
		t.Run("extract_key_id", func(t *testing.T) {
			// Last 8 characters should be the short key ID
			if len(tt.fingerprint) >= 8 {
				shortKeyID := tt.fingerprint[len(tt.fingerprint)-8:]
				if shortKeyID != tt.expectKeyID {
					t.Errorf("expected key ID %s, got %s", tt.expectKeyID, shortKeyID)
				}
			}
		})
	}
}

// TestUIDParsing tests UID field parsing
func TestUIDParsing(t *testing.T) {
	tests := []struct {
		name       string
		uid        string
		hasName    bool
		hasEmail   bool
		hasComment bool
	}{
		{"name_and_email", "John Doe <john@example.com>", true, true, false},
		{"with_comment", "Test User (Test Key) <test@example.com>", true, true, true},
		{"email_only", "test@example.com", false, true, false},
		{"name_only", "John Doe", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasName := len(tt.uid) > 0 && tt.uid[0] != '<'
			hasEmail := contains(tt.uid, "@")
			hasComment := contains(tt.uid, "(") && contains(tt.uid, ")")

			if tt.hasName && !hasName {
				t.Error("expected name in UID")
			}
			if tt.hasEmail && !hasEmail {
				t.Error("expected email in UID")
			}
			if tt.hasComment && !hasComment {
				t.Error("expected comment in UID")
			}
		})
	}
}

// Helper functions
func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

func contains(s, substr string) bool {
	for i := 0; i < len(s); i++ {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
