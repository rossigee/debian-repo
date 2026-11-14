package index

import (
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

var exampleMeta = Metadata{
	Origin:      "example.org",
	Label:       "Example Repository",
	Description: "Example packages",
}

func addExampleVersion(t *testing.T, m *Manager, suite string) {
	t.Helper()
	err := m.AddPackageVersion(suite, "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/test-pkg_1.0.0_amd64.deb",
	})
	if err != nil {
		t.Fatalf("AddPackageVersion: %v", err)
	}
}

// A distribution takes its identity from the repository that owns it, not a
// constant.
func TestNewDistributionCarriesRepositoryMetadata(t *testing.T) {
	m := NewManagerWithMetadata(exampleMeta)
	addExampleVersion(t, m, "stable")

	dist, ok := m.GetDistribution("stable")
	if !ok {
		t.Fatal("stable distribution missing")
	}
	if dist.Origin != exampleMeta.Origin || dist.Label != exampleMeta.Label || dist.Description != exampleMeta.Description {
		t.Errorf("distribution identity = (%q, %q, %q), want (%q, %q, %q)",
			dist.Origin, dist.Label, dist.Description,
			exampleMeta.Origin, exampleMeta.Label, exampleMeta.Description)
	}
}

// A non-default repository's snapshot round-trips its own identity.
func TestSnapshotRoundTripsRepositoryMetadata(t *testing.T) {
	m := NewManagerWithMetadata(exampleMeta)
	addExampleVersion(t, m, "stable")

	snap := m.ToSnapshot("test")
	restored := NewManagerWithMetadata(exampleMeta)
	restored.SetIndex(model.FromSnapshot(snap, "test-snapshot"))

	dist, ok := restored.GetDistribution("stable")
	if !ok {
		t.Fatal("stable distribution missing after restore")
	}
	if dist.Origin != exampleMeta.Origin || dist.Label != exampleMeta.Label {
		t.Errorf("restored identity = (%q, %q), want (%q, %q)",
			dist.Origin, dist.Label, exampleMeta.Origin, exampleMeta.Label)
	}
}

// A snapshot carrying another repository's identity is corrected on load, so
// the next snapshot write persists this repository's values.
func TestLoadCorrectsStaleSnapshotMetadata(t *testing.T) {
	stale := NewManagerWithMetadata(Metadata{
		Origin: "stale.example",
		Label:  "stale label",
	})
	addExampleVersion(t, stale, "stable")
	snap := stale.ToSnapshot("test")

	m := NewManagerWithMetadata(exampleMeta)
	m.SetIndex(model.FromSnapshot(snap, "test-snapshot"))

	dist, ok := m.GetDistribution("stable")
	if !ok {
		t.Fatal("stable distribution missing after load")
	}
	if dist.Origin != exampleMeta.Origin || dist.Label != exampleMeta.Label {
		t.Errorf("loaded identity = (%q, %q), want (%q, %q)",
			dist.Origin, dist.Label, exampleMeta.Origin, exampleMeta.Label)
	}

	persisted := m.ToSnapshot("test")
	if got := persisted.Distributions[0].Origin; got != exampleMeta.Origin {
		t.Errorf("re-persisted origin = %q, want %q", got, exampleMeta.Origin)
	}
}
