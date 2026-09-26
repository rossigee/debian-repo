package validate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// DebInfo holds extracted metadata from a .deb file
type DebInfo struct {
	Package       string
	Version       string
	Architecture  string
	Depends       []string
	ControlFields map[string]string
	MD5           string
	SHA1          string
	SHA256        string
	Size          int64
}

// Deb validates a .deb file structure and extracts control fields.
// Reads the entire file for hashing, then parses ar structure.
func Deb(r io.Reader) (*DebInfo, error) {
	// Read entire file into memory to compute hashes on complete data
	fullData, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read .deb file: %w", err)
	}

	// Compute checksums on the entire file
	md5h := md5.New()
	sha1h := sha1.New()
	sha256h := sha256.New()
	multiWriter := io.MultiWriter(md5h, sha1h, sha256h)
	_, _ = io.Copy(multiWriter, bytes.NewReader(fullData))

	// Try to parse as ar archive
	arReader := NewArReader(bytes.NewReader(fullData))

	// Look for control.tar.gz or control.tar member
	var controlData []byte
	for {
		header, err := arReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read ar member: %w", err)
		}

		if header.Name == "control.tar.gz" || header.Name == "control.tar" || header.Name == "control.tar.zst" {
			// Read exactly header.Size bytes (ar reader will handle this)
			controlData = make([]byte, header.Size)
			n, err := io.ReadFull(arReader, controlData)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to read control member: %w", err)
			}
			if int64(n) < header.Size {
				return nil, fmt.Errorf("incomplete control member: got %d bytes, expected %d", n, header.Size)
			}
			// Continue reading remaining members (data.tar.gz, etc.) through teeReader
			// so their content gets included in the hash
			continue
		}
		// Skip this member by reading and discarding its content through arReader
		// This ensures padding and member tracking are handled correctly
		buf := make([]byte, 8192)
		for {
			n, err := arReader.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("failed to skip ar member: %w", err)
			}
			// Data goes through teeReader to hash writers automatically
			if n == 0 {
				break
			}
		}
	}

	if len(controlData) == 0 {
		return nil, fmt.Errorf("no control.tar found in .deb")
	}

	// Decompress based on compression method
	var tarData []byte
	if bytes.HasPrefix(controlData, []byte{0x1f, 0x8b}) {
		// Gzip magic number detected
		gz, err := gzip.NewReader(bytes.NewReader(controlData))
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func() {
			_ = gz.Close()
		}()

		tarData, err = io.ReadAll(gz)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress control.tar.gz: %w", err)
		}
	} else if bytes.HasPrefix(controlData, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		// Zstandard magic number detected
		decoder, err := zstd.NewReader(bytes.NewReader(controlData))
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd reader: %w", err)
		}
		defer decoder.Close()

		tarData, err = io.ReadAll(decoder)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress control.tar.zst: %w", err)
		}
	} else {
		// Already uncompressed (control.tar)
		tarData = controlData
	}

	// Extract control file from uncompressed tar
	controlContent, err := extractControlFromTar(tarData)
	if err != nil {
		return nil, fmt.Errorf("failed to extract control file: %w", err)
	}

	// Parse control fields
	fields, err := parseControlFile(controlContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse control file: %w", err)
	}

	// Get size after reading the full stream
	size := int64(0)
	// Note: we can't get the exact size from this streaming approach,
	// but that's okay for validation; size will be set from checksums or MinIO StatObject

	info := &DebInfo{
		Package:       fields["Package"],
		Version:       fields["Version"],
		Architecture:  fields["Architecture"],
		ControlFields: fields,
		MD5:           fmt.Sprintf("%x", md5h.Sum(nil)),
		SHA1:          fmt.Sprintf("%x", sha1h.Sum(nil)),
		SHA256:        fmt.Sprintf("%x", sha256h.Sum(nil)),
		Size:          size,
	}

	// Parse dependencies
	if depends, ok := fields["Depends"]; ok {
		info.Depends = parseDependencies(depends)
	}

	// Validate required fields
	if info.Package == "" {
		return nil, fmt.Errorf("missing Package field")
	}
	if info.Version == "" {
		return nil, fmt.Errorf("missing Version field")
	}
	if info.Architecture == "" {
		return nil, fmt.Errorf("missing Architecture field")
	}

	return info, nil
}

// parseControlFile extracts the control file from a tar stream
func extractControlFromTar(tarData []byte) ([]byte, error) {
	tr := tar.NewReader(bytes.NewReader(tarData))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Name == "./control" || header.Name == "control" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("control file not found in control.tar")
}

// parseControlFile parses Debian control format (RFC 822 style)
func parseControlFile(data []byte) (map[string]string, error) {
	// Use pault.ag/go/debian's Paragraph parser
	// The control file is a series of RFC 822 paragraphs
	// We'll use a simpler line-by-line parser since the library API is complex

	fields := make(map[string]string)
	lines := strings.Split(string(data), "\n")

	var currentKey string
	var currentValue strings.Builder

	for _, line := range lines {
		if len(line) == 0 {
			// End of paragraph
			if currentKey != "" {
				fields[currentKey] = currentValue.String()
			}
			break
		}

		if line[0] == ' ' || line[0] == '\t' {
			// Continuation line
			currentValue.WriteString("\n")
			currentValue.WriteString(strings.TrimLeft(line, " \t"))
		} else {
			// New field
			if currentKey != "" {
				fields[currentKey] = currentValue.String()
			}

			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentKey = strings.TrimSpace(parts[0])
				currentValue.Reset()
				currentValue.WriteString(strings.TrimSpace(parts[1]))
			}
		}
	}

	// Don't forget the last field
	if currentKey != "" {
		fields[currentKey] = currentValue.String()
	}

	return fields, nil
}

// parseDependencies parses a Depends field, returning just package names
func parseDependencies(depends string) []string {
	var pkgs []string
	// Split by comma-separated list of alternative groups
	pkgRegex := regexp.MustCompile(`([a-zA-Z0-9][a-zA-Z0-9+.-]*)\b`)
	matches := pkgRegex.FindAllString(depends, -1)
	for _, match := range matches {
		// Avoid duplicates
		found := false
		for _, p := range pkgs {
			if p == match {
				found = true
				break
			}
		}
		if !found {
			pkgs = append(pkgs, match)
		}
	}
	return pkgs
}
