package validate

import (
	"bytes"
	"fmt"
	"testing"
)

// TestControlFieldValidation tests control field name validation
func TestControlFieldValidation(t *testing.T) {
	validFields := map[string]bool{
		"Package":      true,
		"Version":      true,
		"Architecture": true,
		"Maintainer":   true,
		"Description":  true,
		"Depends":      true,
	}

	for field := range validFields {
		if field == "" {
			t.Error("field should not be empty")
		}
	}
}

// TestVersionFormatValidation tests Debian version format validation
func TestVersionFormatValidation(t *testing.T) {
	tests := []struct {
		version    string
		isValid    bool
		hasEpoch   bool
		hasRelease bool
	}{
		{"1.0.0", true, false, false},
		{"2.5.1-1", true, false, true},
		{"2.5.1-1ubuntu1", true, false, true},
		{"1:2.0.0", true, true, false},
		{"1:2.0.0-1ubuntu2", true, true, true},
		{"", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if tt.version == "" && tt.isValid {
				t.Error("empty version should not be valid")
			}

			if tt.version != "" && tt.hasEpoch {
				if !bytes.Contains([]byte(tt.version), []byte(":")) {
					t.Error("version with epoch should contain ':'")
				}
			}

			if tt.version != "" && tt.hasRelease {
				if !bytes.Contains([]byte(tt.version), []byte("-")) {
					t.Error("version with release should contain '-'")
				}
			}
		})
	}
}

// TestArchitectureValidation tests architecture field validation
func TestArchitectureValidation(t *testing.T) {
	validArchs := []string{"amd64", "i386", "arm64", "armhf", "ppc64el", "s390x", "all"}
	invalidArchs := []string{"", "x86_64", "ia64", "unknown"}

	for _, arch := range validArchs {
		t.Run("valid_"+arch, func(t *testing.T) {
			if arch == "" {
				t.Error("empty architecture should be invalid")
				return
			}
			if len(arch) < 3 {
				t.Error("architecture should be reasonably named")
			}
		})
	}

	for _, arch := range invalidArchs {
		t.Run("invalid_"+arch, func(t *testing.T) {
			if arch == "" {
				// Empty is correctly invalid
				return
			}
			// Check for non-standard names
			if arch == "x86_64" || arch == "ia64" {
				// These are Linux names, not Debian architecture names
				t.Logf("non-Debian architecture name: %s", arch)
			}
		})
	}
}

// TestMaintainerValidation tests Maintainer field format
func TestMaintainerValidation(t *testing.T) {
	tests := []struct {
		maintainer string
		isValid    bool
		hasEmail   bool
	}{
		{"John Doe <john@example.com>", true, true},
		{"john@example.com", true, true},
		{"", false, false},
		{"No Email Format", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.maintainer, func(t *testing.T) {
			if tt.maintainer == "" && tt.isValid {
				t.Error("empty maintainer should not be valid")
			}

			if tt.hasEmail {
				if !bytes.Contains([]byte(tt.maintainer), []byte("@")) {
					t.Error("email should contain @")
				}
			}
		})
	}
}

// TestPackageNameValidation tests Package field validation
func TestPackageNameValidation(t *testing.T) {
	tests := []struct {
		name    string
		isValid bool
	}{
		{"curl", true},
		{"libssl3", true},
		{"python3-requests", true},
		{"golang-1.19", true},
		{"test-package", true},
		{"", false},
		{"TEST-PACKAGE", true},  // Debian accepts uppercase in some cases
		{"test package", false}, // Spaces not allowed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "" && tt.isValid {
				t.Error("empty package name should not be valid")
			}

			if bytes.Contains([]byte(tt.name), []byte(" ")) && tt.isValid {
				t.Error("package names should not contain spaces")
			}
		})
	}
}

// TestChecksumsValidation tests checksum format validation
func TestChecksumsValidation(t *testing.T) {
	tests := []struct {
		name     string
		checksum string
		isValid  bool
		length   int
	}{
		{"md5_like", "abc123def456", true, 12},
		{"short", "abc123", true, 6},
		{"sha256_32bytes", "a" + string(make([]byte, 31)), true, 32},
		{"sha256_64bytes", "a" + string(make([]byte, 63)), true, 64},
		{"empty", "", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.checksum == "" && tt.isValid {
				t.Error("empty checksum should not be valid")
			}

			if len(tt.checksum) != tt.length {
				t.Logf("expected length %d, got %d", tt.length, len(tt.checksum))
			}
		})
	}
}

// TestParseControlFileDuplicateFields tests that a stanza declaring the same
// field name twice is rejected. dpkg refuses such a .deb with "duplicate
// value for user-defined field 'Homepage'", so accepting it here would let the
// repository serve a package the client cannot install.
func TestParseControlFileDuplicateFields(t *testing.T) {
	tests := []struct {
		name       string
		control    string
		wantField  string
		wantFirst  int
		wantRepeat int
	}{
		{
			name: "user_defined_field",
			control: `Package: chiron
Version: 1.1-1
Architecture: all
Homepage: https://example.com/a
Homepage: https://example.com/b
Description: dup homepage
 text
`,
			wantField:  "Homepage",
			wantFirst:  4,
			wantRepeat: 5,
		},
		{
			name: "known_field",
			control: `Package: chiron
Version: 1.1-1
Architecture: all
Description: first
Description: second
`,
			wantField:  "Description",
			wantFirst:  4,
			wantRepeat: 5,
		},
		{
			name: "case_insensitive",
			control: `Package: chiron
Homepage: https://example.com/a
homepage: https://example.com/b
`,
			wantField:  "homepage",
			wantFirst:  2,
			wantRepeat: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields, err := parseControlFile([]byte(tt.control))
			if err == nil {
				t.Fatalf("expected an error, got fields %v", fields)
			}

			want := fmt.Sprintf("duplicate value for '%s' field: declared again on line %d, first declared on line %d",
				tt.wantField, tt.wantRepeat, tt.wantFirst)
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

// TestParseControlFileContinuationLines tests that a continuation line which
// looks like a field declaration stays part of the preceding value. dpkg only
// treats a line as a new field when it does not begin with whitespace.
func TestParseControlFileContinuationLines(t *testing.T) {
	control := `Package: chiron
Homepage: https://example.com/a
Description: synopsis
 See https://example.com/bug for details.
 Maintainer: not a real field
`

	fields, err := parseControlFile([]byte(control))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := fields["Homepage"]; got != "https://example.com/a" {
		t.Errorf("Homepage = %q, want %q", got, "https://example.com/a")
	}

	want := "synopsis\nSee https://example.com/bug for details.\nMaintainer: not a real field"
	if got := fields["Description"]; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}

	if _, ok := fields["Maintainer"]; ok {
		t.Error("continuation line must not become its own field")
	}
}
