package aptmeta

import (
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// aptHashedFields are the fields debListParser::VersionHash feeds into the
// version hash. apt compares that hash between the Packages entry and the
// entry dpkg recorded in /var/lib/dpkg/status; if they differ the two versions
// are not merged, the candidate is no longer pointer-identical to the current
// version, and apt reinstalls the same version on every run.
var aptHashedFields = []string{
	"Installed-Size",
	"Depends",
	"Pre-Depends",
	"Conflicts",
	"Breaks",
	"Replaces",
}

// parseLikeApt parses a Packages stanza the way apt's pkgTagSection::Scan does.
// The important quirk, and the reason this helper exists rather than strings.Split,
// is that a non-indented line starts a new field and apt then searches for the
// terminating colon across the *whole remaining buffer*, not just that line. So
// one unindented continuation line silently swallows every field up to the next
// colon instead of being reported as a parse error.
func parseLikeApt(stanza string) map[string]string {
	fields := map[string]string{}
	lines := strings.Split(strings.TrimRight(stanza, "\n"), "\n")
	isFold := func(s string) bool { return s != "" && (s[0] == ' ' || s[0] == '\t') }
	i := 0
	for i < len(lines) {
		if lines[i] == "" {
			i++
			continue
		}
		if isFold(lines[i]) {
			i++
			continue
		}
		colon := strings.Index(lines[i], ":")
		if colon < 0 {
			// apt would memchr forward into later lines and absorb them.
			for j := i + 1; j < len(lines); j++ {
				if k := strings.Index(lines[j], ":"); k >= 0 {
					fields[strings.Join(lines[i:j], "\n")] = strings.TrimSpace(lines[j][k+1:])
					i = j + 1
					break
				}
			}
			continue
		}
		name := lines[i][:colon]
		parts := []string{strings.TrimLeft(lines[i][colon+1:], " \t")}
		i++
		for i < len(lines) && isFold(lines[i]) {
			parts = append(parts, strings.TrimLeft(lines[i], " \t"))
			i++
		}
		fields[name] = strings.Join(parts, "\n")
	}
	return fields
}

func testDistribution(t *testing.T, control map[string]string) *model.Distribution {
	t.Helper()
	dist := model.NewDistribution("stable", "stable", "example.org", "Example", "")
	comp := model.NewComponent("main")
	pkg := model.NewPackage("testpkg")
	pkg.Versions["1.0-1"] = &model.PackageVersion{
		Version:      "1.0-1",
		Architecture: "amd64",
		Filename:     "pool/main/testpkg_1.0-1_amd64.deb",
		Checksums: model.Checksums{
			MD5:    "d41d8cd98f00b204e9800998ecf8427e",
			SHA1:   "da39a3ee5e6b4b0d3255bfef95601890afd80709",
			SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			Size:   1234,
		},
		ControlFields: control,
	}
	comp.Packages["testpkg"] = pkg
	dist.Components["main"] = comp
	dist.Architectures = []string{"amd64"}
	return dist
}

// realWorldControl mirrors what validate.parseControlFile produces for a
// package built by dpkg-buildpackage: continuation lines are stored with their
// leading fold whitespace already stripped.
func realWorldControl() map[string]string {
	return map[string]string{
		"Package":        "testpkg",
		"Version":        "1.0-1",
		"Architecture":   "amd64",
		"Section":        "misc",
		"Priority":       "optional",
		"Maintainer":     "Test <t@example.org>",
		"Installed-Size": "9487",
		"Homepage":       "https://example.org/testpkg",
		"Depends":        "libc6 (>= 2.34)",
		"Description": "Short synopsis\n" +
			"First extended line\n" +
			"Second extended line",
	}
}

func render(t *testing.T, control map[string]string) string {
	t.Helper()
	dist := testDistribution(t, control)
	out, _, err := RenderPackages(dist, "main", "amd64")
	if err != nil {
		t.Fatalf("RenderPackages: %v", err)
	}
	return string(out)
}

// Regression: the rendered Packages must survive apt's parser with every field
// intact. A stanza whose Description continuation lines are not indented makes
// apt absorb the following Depends field, which changes the version hash and
// sends apt into a permanent same-version reinstall loop.
func TestRenderPackagesSurvivesAptParser(t *testing.T) {
	control := realWorldControl()
	stanza := render(t, control)

	for i, line := range strings.Split(strings.TrimRight(stanza, "\n"), "\n") {
		if line == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' && !strings.Contains(line, ":") {
			t.Errorf("line %d is neither a field nor a continuation line: %q", i+1, line)
		}
	}

	fields := parseLikeApt(stanza)
	for _, want := range []string{
		"Package", "Version", "Architecture", "Section", "Priority",
		"Maintainer", "Homepage", "Installed-Size", "Depends", "Description",
		"Filename", "Size", "MD5sum", "SHA1", "SHA256",
	} {
		if _, ok := fields[want]; !ok {
			t.Errorf("field %q missing or unparseable; apt would mis-slice the stanza.\nparsed fields: %v",
				want, keys(fields))
		}
	}
}

// Every field apt feeds into the version hash must round-trip byte-identically
// to the value dpkg holds, or apt decides the installed package is not the one
// the repository describes.
func TestRenderPackagesPreservesAptHashedFields(t *testing.T) {
	control := realWorldControl()
	fields := parseLikeApt(render(t, control))

	for _, name := range aptHashedFields {
		want, present := control[name]
		if !present {
			continue
		}
		got, ok := fields[name]
		if !ok {
			t.Errorf("apt-hashed field %q absent from rendered Packages; apt will reinstall this package forever", name)
			continue
		}
		if got != want {
			t.Errorf("apt-hashed field %q = %q, want %q", name, got, want)
		}
	}
}

// Installed-Size is optional per Debian Policy 5.3/5.6.20, but a Packages entry
// that omits it makes apt compare 0 against the value dpkg recorded and
// reinstall the package on every run, so it must be rendered when the .deb
// carried it.
func TestRenderPackagesIncludesInstalledSize(t *testing.T) {
	fields := parseLikeApt(render(t, realWorldControl()))
	if got := fields["Installed-Size"]; got != "9487" {
		t.Errorf("Installed-Size = %q, want %q", got, "9487")
	}
}

// Continuation lines of a folded field must be indented with a space on the
// wire, and stripping that indent again must recover the stored value exactly.
func TestRenderPackagesFoldsContinuationLines(t *testing.T) {
	control := realWorldControl()
	stanza := render(t, control)

	for _, want := range []string{
		"Description: Short synopsis\n First extended line\n Second extended line\n",
	} {
		if !strings.Contains(stanza, want) {
			t.Errorf("rendered stanza missing properly indented fold:\n%q\n\ngot:\n%s", want, stanza)
		}
	}

	if got := parseLikeApt(stanza)["Description"]; got != control["Description"] {
		t.Errorf("Description round trip = %q, want %q", got, control["Description"])
	}
}

// A Description blank line is encoded as " ." in Debian control files. It must
// survive the round trip rather than being emitted as a bare "." line, which
// would terminate the stanza.
func TestRenderPackagesPreservesBlankDescriptionLine(t *testing.T) {
	control := realWorldControl()
	control["Description"] = "Short synopsis\n.\nMore text"
	fields := parseLikeApt(render(t, control))
	want := "Short synopsis\n.\nMore text"
	if got := fields["Description"]; got != want {
		t.Errorf("Description = %q, want %q", got, want)
	}
}

func TestRenderPackagesOmitsAbsentOptionalFields(t *testing.T) {
	control := realWorldControl()
	delete(control, "Homepage")
	delete(control, "Multi-Arch")
	fields := parseLikeApt(render(t, control))
	for _, name := range []string{"Homepage", "Multi-Arch"} {
		if _, ok := fields[name]; ok {
			t.Errorf("field %q should be omitted when the control file lacks it", name)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
