package aptmeta

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// RenderRelease generates the plaintext Release file for a distribution
func RenderRelease(dist *model.Distribution, origin, label, description string) ([]byte, error) {
	dist.Mu.RLock()
	defer dist.Mu.RUnlock()

	var buf bytes.Buffer

	// Metadata header
	buf.WriteString("Origin: ")
	buf.WriteString(origin)
	buf.WriteString("\n")

	buf.WriteString("Label: ")
	buf.WriteString(label)
	buf.WriteString("\n")

	buf.WriteString("Suite: ")
	buf.WriteString(dist.Suite)
	buf.WriteString("\n")

	buf.WriteString("Codename: ")
	buf.WriteString(dist.Codename)
	buf.WriteString("\n")

	buf.WriteString("Date: ")
	buf.WriteString(time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 MST"))
	buf.WriteString("\n")

	buf.WriteString("Architectures:")
	for _, arch := range dist.Architectures {
		buf.WriteString(" ")
		buf.WriteString(arch)
	}
	buf.WriteString("\n")

	// Components (hardcoded to "main" for now, but could be dynamic)
	buf.WriteString("Components:")
	for compName := range dist.Components {
		buf.WriteString(" ")
		buf.WriteString(compName)
	}
	buf.WriteString("\n")

	buf.WriteString("Description: ")
	buf.WriteString(description)
	buf.WriteString("\n")

	// Generate and list checksums for index files
	buf.WriteString("MD5Sum:\n")
	buf.WriteString(generateChecksumLines(dist, "MD5"))

	buf.WriteString("SHA1:\n")
	buf.WriteString(generateChecksumLines(dist, "SHA1"))

	buf.WriteString("SHA256:\n")
	buf.WriteString(generateChecksumLines(dist, "SHA256"))

	return buf.Bytes(), nil
}

// generateChecksumLines generates the checksum section for Release file
func generateChecksumLines(dist *model.Distribution, algorithm string) string {
	var buf bytes.Buffer

	// Collect all index files
	type checksumEntry struct {
		path     string
		content  []byte
		filename string
	}

	var entries []checksumEntry

	// Get all components (sorted) for deterministic ordering
	components := make([]string, 0, len(dist.Components))
	for compName := range dist.Components {
		components = append(components, compName)
	}
	sort.Strings(components)

	if len(components) == 0 {
		return buf.String()
	}

	// Add Packages files for all components
	for _, compName := range components {
		for _, arch := range dist.Architectures {
			// Uncompressed Packages
			content, _, err := RenderPackages(dist, compName, arch)
			if err == nil && len(content) > 0 {
				entries = append(entries, checksumEntry{
					path:     fmt.Sprintf("%s/binary-%s/Packages", compName, arch),
					content:  content,
					filename: fmt.Sprintf("%s/binary-%s/Packages", compName, arch),
				})

				// Compressed Packages.gz
				_, compressed, err := RenderPackages(dist, compName, arch)
				if err == nil && len(compressed) > 0 {
					entries = append(entries, checksumEntry{
						path:     fmt.Sprintf("%s/binary-%s/Packages.gz", compName, arch),
						content:  compressed,
						filename: fmt.Sprintf("%s/binary-%s/Packages.gz", compName, arch),
					})
				}
			}
		}
	}

	// Sort by path for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	// Generate checksum lines
	for _, entry := range entries {
		var hash string
		switch algorithm {
		case "MD5":
			hash = fmt.Sprintf("%x", md5.Sum(entry.content))
		case "SHA1":
			hash = fmt.Sprintf("%x", sha1.Sum(entry.content))
		case "SHA256":
			hash = fmt.Sprintf("%x", sha256.Sum256(entry.content))
		}

		fmt.Fprintf(&buf, " %s %10d %s\n", hash, len(entry.content), entry.filename)
	}

	return buf.String()
}

// FileChecksums computes checksums for a file content
// Used when computing Release file hashes
func FileChecksums(content []byte) (md5str, sha1str, sha256str string) {
	md5str = fmt.Sprintf("%x", md5.Sum(content))
	sha1str = fmt.Sprintf("%x", sha1.Sum(content))
	sha256str = fmt.Sprintf("%x", sha256.Sum256(content))
	return
}

// TODO: RenderDistribution - needs SignReleaseFile implementation
// Currently distributions are rendered on-demand in HTTP handlers
// func RenderDistribution(dist *model.Distribution, origin, label, description string, signer *gpgsign.Signer) (*model.RenderedDist, error) {
