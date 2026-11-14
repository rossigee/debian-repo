package index

import (
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

const sharedPoolKey = "pool/main/test-pkg_1.0.0_amd64.deb"

func addSharedVersion(t *testing.T, m *Manager, suite string) {
	t.Helper()
	err := m.AddPackageVersion(suite, "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     sharedPoolKey,
	})
	if err != nil {
		t.Fatalf("AddPackageVersion(%s): %v", suite, err)
	}
}

// A package indexed in two suites shares one pool object. Removing it from
// one suite must leave the object referenced by the other.
func TestPoolObjectKeptWhileAnotherSuiteReferencesIt(t *testing.T) {
	m := NewManager()
	addSharedVersion(t, m, "stable")
	addSharedVersion(t, m, "testing")

	if got := m.CountPoolReferences(sharedPoolKey); got != 2 {
		t.Fatalf("CountPoolReferences = %d, want 2", got)
	}

	if err := m.RemovePackageVersion("stable", "main", "test-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion: %v", err)
	}

	if got := m.CountPoolReferences(sharedPoolKey); got != 1 {
		t.Fatalf("after removing from stable, CountPoolReferences = %d, want 1", got)
	}
	if orphaned := m.UnreferencedPoolKeys([]string{sharedPoolKey}); len(orphaned) != 0 {
		t.Fatalf("object still referenced by testing must not be deletable, got %v", orphaned)
	}
}

// Once the last suite drops the package, the object becomes deletable.
func TestPoolObjectDeletableWhenNoSuiteReferencesIt(t *testing.T) {
	m := NewManager()
	addSharedVersion(t, m, "stable")
	addSharedVersion(t, m, "testing")

	if err := m.RemovePackageVersion("stable", "main", "test-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion(stable): %v", err)
	}
	if err := m.RemovePackageVersion("testing", "main", "test-pkg", "1.0.0", "amd64"); err != nil {
		t.Fatalf("RemovePackageVersion(testing): %v", err)
	}

	orphaned := m.UnreferencedPoolKeys([]string{sharedPoolKey})
	if len(orphaned) != 1 || orphaned[0] != sharedPoolKey {
		t.Fatalf("UnreferencedPoolKeys = %v, want [%s]", orphaned, sharedPoolKey)
	}
}
