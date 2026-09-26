package index

import (
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

func TestAddPackageVersion(t *testing.T) {
	m := NewManager()

	ver := &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Checksums:    model.Checksums{Size: 100},
		ControlFields: map[string]string{
			"Package": "test-pkg",
		},
		UploadedAt: time.Now(),
	}

	if err := m.AddPackageVersion("stable", "main", "test-pkg", ver); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	dist, ok := m.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution 'stable' not found")
	}

	comp := dist.Components["main"]
	if len(comp.Packages) != 1 {
		t.Fatalf("Expected 1 package, got %d", len(comp.Packages))
	}

	pkg := comp.Packages["test-pkg"]
	if len(pkg.Versions) != 1 {
		t.Fatalf("Expected 1 version, got %d", len(pkg.Versions))
	}

	t.Logf("✓ Package added correctly")
}

func TestAddPackageVersionMultipleArch(t *testing.T) {
	m := NewManager()

	versions := []struct {
		ver  string
		arch string
	}{
		{"1.0.0", "amd64"},
		{"1.0.0", "arm64"},
		{"2.0.0", "amd64"},
	}

	for _, v := range versions {
		pv := &model.PackageVersion{
			Version:       v.ver,
			Architecture:  v.arch,
			Checksums:     model.Checksums{Size: 100},
			ControlFields: map[string]string{"Package": "multiarch-pkg"},
			UploadedAt:    time.Now(),
		}
		if err := m.AddPackageVersion("stable", "main", "multiarch-pkg", pv); err != nil {
			t.Fatalf("AddPackageVersion failed for %s/%s: %v", v.ver, v.arch, err)
		}
	}

	dist, _ := m.GetDistribution("stable")
	comp := dist.Components["main"]
	pkg := comp.Packages["multiarch-pkg"]
	if len(pkg.Versions) != 3 {
		t.Fatalf("Expected 3 versions, got %d", len(pkg.Versions))
	}

	archs := m.ListArchitectures("stable")
	if len(archs) != 2 {
		t.Fatalf("Expected 2 architectures, got %v", archs)
	}

	t.Logf("✓ Multiple architectures handled correctly")
}

func TestRemovePackageVersion(t *testing.T) {
	m := NewManager()

	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 100},
		ControlFields: map[string]string{"Package": "removetest-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := m.AddPackageVersion("stable", "main", "removetest-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	// Verify it exists
	dist, _ := m.GetDistribution("stable")
	if len(dist.Components["main"].Packages["removetest-pkg"].Versions) != 1 {
		t.Fatal("Package should exist before removal")
	}

	// Remove it
	if err := m.RemovePackageVersion("stable", "main", "removetest-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion failed: %v", err)
	}

	// Verify it's gone
	dist, _ = m.GetDistribution("stable")
	if len(dist.Components) != 0 {
		t.Fatalf("Expected 0 components after removal, got %d", len(dist.Components))
	}

	// Verify architecture list is empty
	archs := m.ListArchitectures("stable")
	if len(archs) != 0 {
		t.Fatalf("Expected 0 architectures, got %v", archs)
	}

	t.Logf("✓ Package removed successfully, architecture list recalculated")
}

func TestRemovePackageVersionPartial(t *testing.T) {
	m := NewManager()

	versions := []struct {
		ver  string
		arch string
	}{
		{"1.0.0", "amd64"},
		{"1.0.0", "arm64"},
	}
	for _, v := range versions {
		pv := &model.PackageVersion{
			Version:       v.ver,
			Architecture:  v.arch,
			Checksums:     model.Checksums{Size: 100},
			ControlFields: map[string]string{"Package": "partial-pkg"},
			UploadedAt:    time.Now(),
		}
		if err := m.AddPackageVersion("stable", "main", "partial-pkg", pv); err != nil {
			t.Fatalf("AddPackageVersion failed: %v", err)
		}
	}

	// Remove only amd64
	if err := m.RemovePackageVersion("stable", "main", "partial-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion failed: %v", err)
	}

	// Verify arm64 still exists
	dist, _ := m.GetDistribution("stable")
	pkg := dist.Components["main"].Packages["partial-pkg"]
	if _, ok := pkg.Versions["1.0.0:amd64"]; ok {
		t.Fatal("amd64 version should be removed")
	}
	if _, ok := pkg.Versions["1.0.0:arm64"]; !ok {
		t.Fatal("arm64 version should still exist")
	}

	// Verify only arm64 architecture remains
	archs := m.ListArchitectures("stable")
	if len(archs) != 1 || archs[0] != "arm64" {
		t.Fatalf("Expected only arm64, got %v", archs)
	}

	t.Logf("✓ Partial removal works, architecture list recalculated")
}

func TestRemovePackageVersionErrors(t *testing.T) {
	m := NewManager()

	// Remove from non-existent distribution
	if err := m.RemovePackageVersion("nonexistent", "main", "pkg", "1.0.0", "amd64"); err == nil {
		t.Fatal("Expected error for non-existent distribution")
	}

	// Add package to stable
	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 100},
		ControlFields: map[string]string{"Package": "test-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := m.AddPackageVersion("stable", "main", "test-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	// Remove non-existent component
	if err := m.RemovePackageVersion("stable", "nonexistent", "test-pkg", "1.0.0", "amd64"); err == nil {
		t.Fatal("Expected error for non-existent component")
	}

	// Remove non-existent package
	if err := m.RemovePackageVersion("stable", "main", "nonexistent", "1.0.0", "amd64"); err == nil {
		t.Fatal("Expected error for non-existent package")
	}

	// Remove non-existent version/arch
	if err := m.RemovePackageVersion("stable", "main", "test-pkg", "2.0.0", "amd64"); err == nil {
		t.Fatal("Expected error for non-existent version/arch")
	}

	t.Logf("✓ All error cases handled correctly")
}

func TestRemovePackageVersionCascadeCleanup(t *testing.T) {
	m := NewManager()

	// Add package
	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 100},
		ControlFields: map[string]string{"Package": "cleanup-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := m.AddPackageVersion("stable", "main", "cleanup-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	// Remove it - should cascade: remove version → remove package → remove component → remove dist
	if err := m.RemovePackageVersion("stable", "main", "cleanup-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion failed: %v", err)
	}

	// Distribution should still exist but be empty
	dist, ok := m.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should still exist")
	}
	if len(dist.Components) != 0 {
		t.Fatalf("Expected 0 components after cleanup, got %d", len(dist.Components))
	}

	t.Logf("✓ Cascade cleanup works correctly")
}

func TestListDistributions(t *testing.T) {
	m := NewManager()

	if err := m.AddPackageVersion("stable", "main", "pkg1", &model.PackageVersion{
		Version: "1.0.0", Architecture: "amd64", Checksums: model.Checksums{Size: 10},
		ControlFields: map[string]string{"Package": "pkg1"}, UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	if err := m.AddPackageVersion("testing", "main", "pkg2", &model.PackageVersion{
		Version: "2.0.0", Architecture: "amd64", Checksums: model.Checksums{Size: 10},
		ControlFields: map[string]string{"Package": "pkg2"}, UploadedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	dists := m.ListDistributions()
	if len(dists) != 2 {
		t.Fatalf("Expected 2 distributions, got %d: %v", len(dists), dists)
	}

	t.Logf("✓ ListDistributions works correctly")
}

func TestGetDistributionNotFound(t *testing.T) {
	m := NewManager()
	_, ok := m.GetDistribution("nonexistent")
	if ok {
		t.Fatal("Expected false for non-existent distribution")
	}
	t.Logf("✓ GetDistribution returns false for non-existent")
}

func TestCountPackages(t *testing.T) {
	m := NewManager()

	for i := 0; i < 3; i++ {
		pv := &model.PackageVersion{
			Version:       "1.0.0",
			Architecture:  "amd64",
			Checksums:     model.Checksums{Size: 100},
			ControlFields: map[string]string{"Package": "pkg"},
			UploadedAt:    time.Now(),
		}
		if err := m.AddPackageVersion("stable", "main", "pkg", pv); err != nil {
			t.Fatalf("AddPackageVersion failed: %v", err)
		}
	}

	// Only 1 unique package but 3 versions
	count := m.CountPackages("stable")
	if count != 1 {
		t.Fatalf("Expected 1 unique package, got %d", count)
	}

	t.Logf("✓ CountPackages works correctly")
}

func TestConcurrentAccess(t *testing.T) {
	m := NewManager()

	// Add packages concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			pv := &model.PackageVersion{
				Version:       "1.0.0",
				Architecture:  "amd64",
				Checksums:     model.Checksums{Size: 100},
				ControlFields: map[string]string{"Package": "concurrent-pkg"},
				UploadedAt:    time.Now(),
			}
			_ = m.AddPackageVersion("stable", "main", "concurrent-pkg", pv)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify we can still read
	dist, ok := m.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should exist")
	}
	_ = dist.Components["main"].Packages["concurrent-pkg"]

	t.Logf("✓ Concurrent access safe")
}

func TestAddPackageVersionCreatesDistribution(t *testing.T) {
	m := NewManager()

	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 100},
		ControlFields: map[string]string{"Package": "newdist-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := m.AddPackageVersion("newdist", "main", "newdist-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	dist, ok := m.GetDistribution("newdist")
	if !ok {
		t.Fatal("Distribution 'newdist' should have been created")
	}
	if dist.Suite != "newdist" {
		t.Fatalf("Expected Suite='newdist', got '%s'", dist.Suite)
	}

	t.Logf("✓ Distribution created on-demand")
}

func TestRecentVersions_SortedAndLimited(t *testing.T) {
	m := NewManager()

	now := time.Now()

	// Add three versions with distinct timestamps
	versions := []struct {
		name      string
		version   string
		arch      string
		timestamp time.Time
	}{
		{"pkg1", "1.0", "amd64", now.Add(-5 * time.Minute)},
		{"pkg1", "2.0", "amd64", now.Add(-2 * time.Minute)},
		{"pkg1", "3.0", "amd64", now},
		{"pkg2", "1.0", "arm64", now.Add(-1 * time.Minute)},
	}

	for _, v := range versions {
		ver := &model.PackageVersion{
			Version:       v.version,
			Architecture:  v.arch,
			Filename:      "pool/main/" + v.name + "_" + v.version + "_" + v.arch + ".deb",
			ControlFields: map[string]string{"Package": v.name},
			UploadedAt:    v.timestamp,
		}
		if err := m.AddPackageVersion("stable", "main", v.name, ver); err != nil {
			t.Fatalf("AddPackageVersion failed: %v", err)
		}
	}

	// Get all entries
	entries := m.RecentVersions("stable", 0)
	if len(entries) != 4 {
		t.Errorf("Expected 4 entries, got %d", len(entries))
	}

	// Verify they're sorted by upload time (newest first)
	for i := 0; i < len(entries)-1; i++ {
		if entries[i].UploadedAt.Before(entries[i+1].UploadedAt) {
			t.Errorf("Entry %d should be newer than entry %d", i, i+1)
		}
	}

	// Verify newest is pkg1 v3.0
	if entries[0].Package != "pkg1" || entries[0].Version != "3.0" {
		t.Errorf("Newest entry should be pkg1 v3.0, got %s %s", entries[0].Package, entries[0].Version)
	}

	// Test limiting
	limited := m.RecentVersions("stable", 2)
	if len(limited) != 2 {
		t.Errorf("Expected 2 limited entries, got %d", len(limited))
	}

	// Verify limit still maintains order
	if limited[0].UploadedAt.Before(limited[1].UploadedAt) {
		t.Error("Limited entries should be sorted by upload time (newest first)")
	}

	t.Logf("✓ RecentVersions sorted and limited correctly")
}
