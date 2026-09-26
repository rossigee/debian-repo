package integration

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"
)

// TestExtractControlFromDeb tests extracting and decompressing control.tar.gz
func TestExtractControlFromDeb(t *testing.T) {
	deb := generateMinimalDeb(t, "test-pkg", "1.0.0", "amd64")

	// Manually extract control.tar.gz from ar archive
	offset := 8 // Skip "!<arch>\n"

	// Skip debian-binary member
	header1 := deb[offset : offset+60]
	offset += 60
	// Parse size from header
	sizeStr := string(bytes.TrimSpace(header1[48:58]))
	var size1 int
	for _, b := range []byte(sizeStr) {
		if b >= '0' && b <= '9' {
			size1 = size1*10 + int(b-'0')
		}
	}
	t.Logf("Member 1 (debian-binary): %d bytes", size1)
	offset += size1
	if size1%2 == 1 {
		offset++ // Skip padding
	}

	// Parse control.tar.gz header
	header2 := deb[offset : offset+60]
	offset += 60
	sizeStr2 := string(bytes.TrimSpace(header2[48:58]))
	var size2 int
	for _, b := range []byte(sizeStr2) {
		if b >= '0' && b <= '9' {
			size2 = size2*10 + int(b-'0')
		}
	}
	t.Logf("Member 2 (control.tar.gz): %d bytes", size2)

	controlTarGz := deb[offset : offset+size2]
	t.Logf("Extracted control.tar.gz: %d bytes", len(controlTarGz))
	t.Logf("First 20 bytes: %v", controlTarGz[:min(20, len(controlTarGz))])

	// Try to decompress
	gz, err := gzip.NewReader(bytes.NewReader(controlTarGz))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gz.Close()

	// Read all decompressed data
	data := new(bytes.Buffer)
	n, err := data.ReadFrom(gz)
	if err != nil {
		t.Fatalf("Failed to read gzip data: %v (read %d bytes before error)", err, n)
	}

	t.Logf("✓ Successfully decompressed: %d bytes", n)
	t.Logf("Content sample: %q", string(data.Bytes()[:min(100, data.Len())]))

	// Save for manual inspection
	os.WriteFile("/tmp/control.tar.gz", controlTarGz, 0644)
	t.Logf("Saved to /tmp/control.tar.gz")
}
