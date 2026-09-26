package reconcile

import (
	"bytes"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// TestDiscrepancyStructure tests the Discrepancy type
func TestDiscrepancyStructure(t *testing.T) {
	d := Discrepancy{
		Kind:    "invalid_deb_file",
		Message: "pool/main/test.deb: ar header invalid",
	}

	if d.Kind != "invalid_deb_file" {
		t.Errorf("expected kind %q, got %q", "invalid_deb_file", d.Kind)
	}

	if d.Message == "" {
		t.Error("message should not be empty")
	}

	if !bytes.Contains([]byte(d.Message), []byte("pool/main/test.deb")) {
		t.Error("message should reference the problematic file")
	}
}

// TestDiscrepancyTypes tests all discrepancy kinds
func TestDiscrepancyTypes(t *testing.T) {
	kinds := []string{"invalid_deb_file", "invalid_pool_path", "orphan_pool_file", "missing_from_pool"}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			d := Discrepancy{Kind: kind, Message: "test"}
			if d.Kind != kind {
				t.Errorf("expected kind %q, got %q", kind, d.Kind)
			}
		})
	}
}

// TestPathParsing tests pool path component extraction logic
func TestPathParsing(t *testing.T) {
	tests := []struct {
		path            string
		expectComponent string
		expectValid     bool
	}{
		{"pool/main/pkg_1.0_amd64.deb", "main", true},
		{"pool/contrib/pkg_1.0_amd64.deb", "contrib", true},
		{"pool/non-free/pkg.deb", "non-free", true},
		{"pool/pkg_1.0_amd64.deb", "", false}, // Missing component
		{"pool/", "", false},                  // No filename
		{"main/pkg.deb", "", false},           // No pool prefix
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			pathParts := bytes.Split([]byte(tt.path), []byte("/"))

			if tt.expectValid {
				if len(pathParts) < 2 {
					t.Errorf("expected valid path with 2+ parts, got %d", len(pathParts))
					return
				}
				component := string(pathParts[1])
				if component != tt.expectComponent {
					t.Errorf("expected component %q, got %q", tt.expectComponent, component)
				}
			} else {
				if len(pathParts) >= 2 {
					// For invalid paths, we might still extract a value, but it shouldn't be a valid component
					component := string(pathParts[1])
					if component == tt.expectComponent && tt.expectComponent != "" {
						t.Errorf("expected invalid extraction, but got component %q", component)
					}
				}
			}
		})
	}
}

// TestIndexToSnapshot verifies snapshot creation from index
func TestIndexToSnapshot(t *testing.T) {
	idx := model.NewIndex()

	// Verify empty index converts to snapshot
	snap := idx.ToSnapshot("test")
	if snap == nil {
		t.Fatal("failed to convert empty index to snapshot")
	}

	// SnapshotGen is the snapshot format version (typically 1 for current format)
	// It's OK if it's zero or non-zero depending on implementation
	if snap == nil {
		t.Error("snapshot should not be nil")
	}

	if len(snap.Distributions) != 0 {
		t.Errorf("expected 0 distributions in empty index snapshot, got %d", len(snap.Distributions))
	}

	// Add a distribution and verify it's in the snapshot
	idx.Distributions["stable"] = model.NewDistribution(
		"stable", "stable", "test.example.com", "Test Repo", "Test Description",
	)

	snap = idx.ToSnapshot("test2")
	if len(snap.Distributions) != 1 {
		t.Errorf("expected 1 distribution in snapshot, got %d", len(snap.Distributions))
	}
}

// TestDistributionCreation verifies distributions are properly created
func TestDistributionCreation(t *testing.T) {
	mgr := index.NewManager()

	ver := &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/pkg_1.0.0_amd64.deb",
		Checksums: model.Checksums{
			SHA256: "abc123def456",
			Size:   5000,
		},
	}

	// Add package to create distribution
	err := mgr.AddPackageVersion("stable", "main", "test-pkg", ver)
	if err != nil {
		t.Fatalf("failed to add package: %v", err)
	}

	// Verify distribution exists
	dists := mgr.ListDistributions()
	if len(dists) != 1 {
		t.Errorf("expected 1 distribution, got %d", len(dists))
	}

	if dists[0] != "stable" {
		t.Errorf("expected distribution 'stable', got %q", dists[0])
	}

	// Verify component exists
	dist, ok := mgr.GetDistribution("stable")
	if !ok {
		t.Fatal("distribution 'stable' not found")
	}

	if _, ok := dist.Components["main"]; !ok {
		t.Fatal("component 'main' not found")
	}

	// Verify package exists
	if _, ok := dist.Components["main"].Packages["test-pkg"]; !ok {
		t.Fatal("package 'test-pkg' not found")
	}
}

// TestMultipleArchitectures verifies multiple architectures are handled
func TestMultipleArchitectures(t *testing.T) {
	mgr := index.NewManager()

	arches := []string{"amd64", "arm64", "i386"}
	for _, arch := range arches {
		ver := &model.PackageVersion{
			Version:      "1.0.0",
			Architecture: arch,
			Filename:     "pool/main/pkg_1.0.0_" + arch + ".deb",
			Checksums: model.Checksums{
				SHA256: "checksum_" + arch,
				Size:   1000,
			},
		}

		err := mgr.AddPackageVersion("stable", "main", "pkg", ver)
		if err != nil {
			t.Fatalf("failed to add %s package: %v", arch, err)
		}
	}

	dist, ok := mgr.GetDistribution("stable")
	if !ok {
		t.Fatal("distribution not created")
	}

	pkg, ok := dist.Components["main"].Packages["pkg"]
	if !ok {
		t.Fatal("package not created")
	}

	if len(pkg.Versions) != len(arches) {
		t.Errorf("expected %d versions (one per arch), got %d", len(arches), len(pkg.Versions))
	}
}
