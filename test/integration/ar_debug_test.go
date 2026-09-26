package integration

import (
	"bytes"
	"testing"
)

// TestArFormat tests ar archive format compliance
func TestArFormat(t *testing.T) {
	deb := generateMinimalDeb(t, "test-pkg", "1.0.0", "amd64")

	// Manual ar parsing
	offset := 0

	// Check magic
	magic := deb[offset : offset+8]
	if !bytes.Equal(magic, []byte("!<arch>\n")) {
		t.Fatalf("Invalid magic: %q", string(magic))
	}
	offset += 8
	t.Logf("✓ Magic: %q", string(magic))

	// Parse first member header
	if offset+60 > len(deb) {
		t.Fatalf("Not enough bytes for member 1 header (have %d, need %d)", len(deb)-offset, 60)
	}
	header1 := deb[offset : offset+60]
	t.Logf("Member 1 header: %q", string(header1))
	t.Logf("  Name: %q", string(header1[0:16]))
	t.Logf("  ModTime: %q", string(header1[16:28]))
	t.Logf("  UID: %q", string(header1[28:34]))
	t.Logf("  GID: %q", string(header1[34:40]))
	t.Logf("  Mode: %q", string(header1[40:48]))
	t.Logf("  Size: %q", string(header1[48:58]))
	t.Logf("  Trailer: %02x %02x", header1[58], header1[59])

	if string(header1[58:60]) != "`\n" {
		t.Fatalf("Invalid trailer for member 1: expected 60 0a, got %02x %02x", header1[58], header1[59])
	}
	t.Logf("✓ Member 1 header OK")

	// Parse member 1 size and content
	sizeStr := string(bytes.TrimSpace(header1[48:58]))
	var size int
	if _, err := bytes.NewReader([]byte(sizeStr)).Read([]byte(sizeStr)); err == nil {
		// Parse decimal
		for _, b := range []byte(sizeStr) {
			if b >= '0' && b <= '9' {
				size = size*10 + int(b-'0')
			}
		}
	}
	t.Logf("Member 1 size: %d bytes", size)

	offset += 60
	if offset+size > len(deb) {
		t.Fatalf("Not enough bytes for member 1 content (have %d, need %d)", len(deb)-offset, size)
	}

	content1 := deb[offset : offset+size]
	t.Logf("Member 1 content: %q", string(content1))
	offset += size

	// Check padding
	if size%2 == 1 {
		if offset >= len(deb) {
			t.Fatalf("Expected padding byte for odd-sized member 1")
		}
		paddingByte := deb[offset]
		t.Logf("Member 1 padding: %02x", paddingByte)
		offset++
	}

	// Parse second member header
	if offset+60 > len(deb) {
		t.Fatalf("Not enough bytes for member 2 header at offset %d (have %d, need %d)", offset, len(deb)-offset, 60)
	}
	header2 := deb[offset : offset+60]
	t.Logf("Member 2 header: %q", string(header2))
	t.Logf("  Name: %q", string(header2[0:16]))
	t.Logf("  Trailer: %02x %02x", header2[58], header2[59])

	if string(header2[58:60]) != "`\n" {
		t.Fatalf("Invalid trailer for member 2: expected 60 0a, got %02x %02x", header2[58], header2[59])
	}
	t.Logf("✓ Member 2 header OK")

	t.Logf("\n✓ All ar headers valid")
}
