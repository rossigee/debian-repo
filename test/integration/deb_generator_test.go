// Package integration contains end-to-end integration tests
package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateMinimalDeb creates a minimal but valid .deb file for testing
func generateMinimalDeb(t *testing.T, pkgName, version, arch string) []byte {
	// Create control.tar.gz
	controlTar := new(bytes.Buffer)
	controlGz := gzip.NewWriter(controlTar)
	controlTarWriter := tar.NewWriter(controlGz)

	controlContent := fmt.Sprintf(`Package: %s
Version: %s
Architecture: %s
Maintainer: Test <test@example.com>
Description: Test package
 This is a test package for debian-repo integration tests.
`, pkgName, version, arch)

	controlHeader := &tar.Header{
		Name:    "./control",
		Size:    int64(len(controlContent)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := controlTarWriter.WriteHeader(controlHeader); err != nil {
		t.Fatalf("Failed to write control header: %v", err)
	}
	if _, err := controlTarWriter.Write([]byte(controlContent)); err != nil {
		t.Fatalf("Failed to write control content: %v", err)
	}

	if err := controlTarWriter.Close(); err != nil {
		t.Fatalf("Failed to close control tar: %v", err)
	}
	if err := controlGz.Close(); err != nil {
		t.Fatalf("Failed to close control gzip: %v", err)
	}

	// Create empty data.tar.gz
	dataTar := new(bytes.Buffer)
	dataGz := gzip.NewWriter(dataTar)
	dataTarWriter := tar.NewWriter(dataGz)

	if err := dataTarWriter.Close(); err != nil {
		t.Fatalf("Failed to close data tar: %v", err)
	}
	if err := dataGz.Close(); err != nil {
		t.Fatalf("Failed to close data gzip: %v", err)
	}

	// Create ar archive
	debFile := new(bytes.Buffer)

	// ar magic header
	if _, err := debFile.WriteString("!<arch>\n"); err != nil {
		t.Fatalf("Failed to write ar magic: %v", err)
	}

	// debian-binary member
	debianBinaryContent := "2.0\n"
	writeArMember(t, debFile, "debian-binary", debianBinaryContent)

	// control.tar.gz member
	writeArMember(t, debFile, "control.tar.gz", string(controlTar.Bytes()))

	// data.tar.gz member
	writeArMember(t, debFile, "data.tar.gz", string(dataTar.Bytes()))

	return debFile.Bytes()
}

func writeArMember(t *testing.T, w io.Writer, name, content string) {
	// ar member header is exactly 60 bytes:
	// name (16), mtime (12), uid (6), gid (6), mode (8), size (10), magic (2: backtick + newline)
	modTime := time.Now().Unix()
	size := len(content)

	// Build header with exact spacing/padding
	header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n",
		name,
		modTime,
		0,
		0,
		0644,
		size,
	)

	if len(header) != 60 {
		t.Fatalf("ar header wrong size: expected 60, got %d", len(header))
	}

	if _, err := w.Write([]byte(header)); err != nil {
		t.Fatalf("Failed to write ar member header: %v", err)
	}

	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("Failed to write ar member content: %v", err)
	}

	// ar format: files padded to even byte offset with newline
	if size%2 == 1 {
		if _, err := w.Write([]byte("\n")); err != nil {
			t.Fatalf("Failed to write ar padding: %v", err)
		}
	}
}

// TestDebGeneration verifies that the test .deb generator produces valid files
func TestDebGeneration(t *testing.T) {
	deb := generateMinimalDeb(t, "test-pkg", "1.0.0", "amd64")

	if len(deb) == 0 {
		t.Fatal("Generated deb is empty")
	}

	// Verify ar magic header
	if !bytes.HasPrefix(deb, []byte("!<arch>\n")) {
		t.Fatal("Generated deb missing ar magic header")
	}

	// Debug: print first part of deb to verify structure
	t.Logf("First 200 bytes of .deb: %q", string(deb[:min(len(deb), 200)]))

	// Verify we can read the first ar member header
	magicEnd := bytes.Index(deb, []byte("!<arch>\n")) + 8
	if magicEnd+60 > len(deb) {
		t.Fatalf("Not enough bytes for first ar member header")
	}
	header1 := deb[magicEnd : magicEnd+60]
	t.Logf("First member header: %q", string(header1))
	t.Logf("Trailer bytes: %02x %02x (expected 60 0a)", header1[58], header1[59])

	// Save to temp file for manual inspection if needed
	tmpFile := filepath.Join(t.TempDir(), "test-pkg_1.0.0_amd64.deb")
	if err := os.WriteFile(tmpFile, deb, 0644); err != nil {
		t.Fatalf("Failed to write temp deb file: %v", err)
	}

	t.Logf("Generated test .deb: %s (%d bytes)", tmpFile, len(deb))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
