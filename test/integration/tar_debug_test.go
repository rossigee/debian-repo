package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
	"time"
)

// TestTarGeneration tests tar.gz generation for deb control files
func TestTarGeneration(t *testing.T) {
	// Create control.tar.gz like the deb generator does
	controlTar := new(bytes.Buffer)
	controlGz := gzip.NewWriter(controlTar)
	controlTarWriter := tar.NewWriter(controlGz)

	controlContent := "Package: test-pkg\nVersion: 1.0.0\nArchitecture: amd64\n"

	controlHeader := &tar.Header{
		Name:    "./control",
		Size:    int64(len(controlContent)),
		Mode:    0644,
		ModTime: time.Now(),
	}

	if err := controlTarWriter.WriteHeader(controlHeader); err != nil {
		t.Fatalf("Failed to write tar header: %v", err)
	}
	if _, err := controlTarWriter.Write([]byte(controlContent)); err != nil {
		t.Fatalf("Failed to write tar content: %v", err)
	}

	if err := controlTarWriter.Close(); err != nil {
		t.Fatalf("Failed to close tar: %v", err)
	}
	if err := controlGz.Close(); err != nil {
		t.Fatalf("Failed to close gzip: %v", err)
	}

	compressedData := controlTar.Bytes()
	t.Logf("Compressed tar.gz: %d bytes", len(compressedData))
	t.Logf("First 20 bytes: %q", string(compressedData[:min(20, len(compressedData))]))

	// Try to decompress and extract
	gz, err := gzip.NewReader(bytes.NewReader(compressedData))
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("Failed to read tar header: %v", err)
	}

	t.Logf("✓ Tar header: name=%q, size=%d", header.Name, header.Size)

	// Read content
	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("Failed to read tar content: %v", err)
	}

	if string(content) != controlContent {
		t.Fatalf("Content mismatch: expected %q, got %q", controlContent, string(content))
	}

	t.Logf("✓ Content verified")
}
