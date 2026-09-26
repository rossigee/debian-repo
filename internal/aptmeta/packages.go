// Package aptmeta renders Debian apt repository metadata (Packages and Release files)
package aptmeta

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"sort"
	"strings"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// writeField writes a control field in RFC 822 form.
//
// validate.parseControlFile stores field values with the fold whitespace
// stripped from continuation lines, so a multi-line value such as
// Description is held as "first line\nsecond line\n...". Continuation lines
// must begin with a space when written back out (Debian Policy 5.1); without
// it apt's parser stops treating the remainder of the field as part of it and
// silently mis-slices every field that follows, which makes apt consider the
// installed and candidate versions different and reinstall forever.
func writeField(buf *bytes.Buffer, name, value string) {
	buf.WriteString(name)
	buf.WriteString(": ")
	for i, line := range strings.Split(value, "\n") {
		if i > 0 {
			buf.WriteString("\n ")
		}
		buf.WriteString(line)
	}
	buf.WriteString("\n")
}

// RenderPackages renders the Packages file for a given component/arch in a distribution
func RenderPackages(dist *model.Distribution, component, arch string) (uncompressed, compressed []byte, err error) {
	dist.Mu.RLock()
	defer dist.Mu.RUnlock()

	comp, ok := dist.Components[component]
	if !ok {
		return []byte{}, []byte{}, nil
	}

	var buf bytes.Buffer

	// Collect and sort packages by name for deterministic output
	var pkgNames []string
	for pkgName := range comp.Packages {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)

	for _, pkgName := range pkgNames {
		pkg := comp.Packages[pkgName]
		// Iterate through all versions for this package (sorted) and filter by architecture
		var verKeys []string
		for key := range pkg.Versions {
			verKeys = append(verKeys, key)
		}
		sort.Strings(verKeys)

		for _, verKey := range verKeys {
			ver := pkg.Versions[verKey]
			if ver.Architecture != arch {
				continue // Skip versions not for this architecture
			}

			// Write package stanza
			buf.WriteString("Package: ")
			buf.WriteString(ver.ControlFields["Package"])
			buf.WriteString("\n")

			buf.WriteString("Version: ")
			buf.WriteString(ver.ControlFields["Version"])
			buf.WriteString("\n")

			buf.WriteString("Architecture: ")
			buf.WriteString(ver.ControlFields["Architecture"])
			buf.WriteString("\n")

			// Optional fields from control file. Installed-Size must be included:
			// apt compares it against the value dpkg recorded in
			// /var/lib/dpkg/status and reinstalls the package on every run when
			// the two disagree, including when the field is absent.
			optionalFields := []string{"Section", "Priority", "Maintainer", "Homepage", "Installed-Size", "Multi-Arch", "Description", "Depends", "Recommends", "Suggests", "Enhances", "Pre-Depends", "Breaks", "Conflicts", "Provides", "Replaces"}
			for _, field := range optionalFields {
				if val, ok := ver.ControlFields[field]; ok && val != "" {
					writeField(&buf, field, val)
				}
			}

			// Add checksum fields
			buf.WriteString("Filename: ")
			buf.WriteString(ver.Filename)
			buf.WriteString("\n")

			fmt.Fprintf(&buf, "Size: %d\n", ver.Checksums.Size)

			if ver.Checksums.MD5 != "" {
				buf.WriteString("MD5sum: ")
				buf.WriteString(ver.Checksums.MD5)
				buf.WriteString("\n")
			}

			if ver.Checksums.SHA1 != "" {
				buf.WriteString("SHA1: ")
				buf.WriteString(ver.Checksums.SHA1)
				buf.WriteString("\n")
			}

			if ver.Checksums.SHA256 != "" {
				buf.WriteString("SHA256: ")
				buf.WriteString(ver.Checksums.SHA256)
				buf.WriteString("\n")
			}

			// End of stanza
			buf.WriteString("\n")
		}
	}

	uncompressed = buf.Bytes()

	// Gzip compress
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(uncompressed); err != nil {
		return nil, nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, nil, err
	}

	compressed = gzBuf.Bytes()

	return uncompressed, compressed, nil
}

// RenderPackagesFromIndex generates all Packages files for a distribution
// Returns a map of "binary-{arch}" -> Packages bytes
func RenderPackagesFromIndex(dist *model.Distribution) (map[string][]byte, error) {
	result := make(map[string][]byte)

	for _, arch := range dist.Architectures {
		uncompressed, _, err := RenderPackages(dist, "main", arch)
		if err != nil {
			return nil, err
		}
		result[fmt.Sprintf("binary-%s", arch)] = uncompressed
	}

	return result, nil
}

// ExtractFieldsFromPackagesFile parses an uncompressed Packages file
// Used for comparison/verification against rendered output
func ExtractFieldsFromPackagesFile(content []byte) []map[string]string {
	var packages []map[string]string
	currentPkg := make(map[string]string)

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			if len(currentPkg) > 0 {
				packages = append(packages, currentPkg)
				currentPkg = make(map[string]string)
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			currentPkg[key] = val
		}
	}

	if len(currentPkg) > 0 {
		packages = append(packages, currentPkg)
	}

	return packages
}
