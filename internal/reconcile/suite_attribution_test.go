package reconcile

import (
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

func seedVersion(filename, sha string) *model.PackageVersion {
	return &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Filename:      filename,
		Checksums:     model.Checksums{SHA256: sha, Size: 100},
		ControlFields: map[string]string{"Package": "x"},
		UploadedAt:    time.Now(),
	}
}

// Files already indexed in a non-default suite must keep that attribution
// so a reconcile does not drag moved packages back to the default suite.
func TestAttributionPreservesNonDefaultSuite(t *testing.T) {
	mgr := index.NewManager()
	if err := mgr.AddPackageVersion("libvirtd", "main", "libvirtd-tools",
		seedVersion("pool/main/libvirtd-tools_1.0.0_amd64.deb", "aaa")); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}
	if err := mgr.AddPackageVersion("stable", "main", "tree",
		seedVersion("pool/main/tree_2.0.0_amd64.deb", "bbb")); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	attr := attributionByFilename(mgr.GetIndex())

	if got := attr["pool/main/libvirtd-tools_1.0.0_amd64.deb"]; len(got) != 1 || got[0] != "libvirtd" {
		t.Fatalf("expected [libvirtd] attribution, got %v", got)
	}
	if got := attr["pool/main/tree_2.0.0_amd64.deb"]; len(got) != 1 || got[0] != "stable" {
		t.Fatalf("expected [stable] attribution, got %v", got)
	}
	if _, ok := attr["pool/main/unknown_1.0_amd64.deb"]; ok {
		t.Fatal("unindexed file must have no attribution")
	}
}

// A file indexed in several suites (copy mode) must be attributed to all
// of them so reconcile preserves the copy instead of collapsing it.
func TestAttributionKeepsCopiesInAllSuites(t *testing.T) {
	mgr := index.NewManager()
	ver := seedVersion("pool/main/shared_1.0.0_amd64.deb", "ccc")
	if err := mgr.AddPackageVersion("stable", "main", "shared", ver); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}
	if err := mgr.AddPackageVersion("libvirtd", "main", "shared", ver); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	attr := attributionByFilename(mgr.GetIndex())
	got := attr["pool/main/shared_1.0.0_amd64.deb"]
	if len(got) != 2 || got[0] != "libvirtd" || got[1] != "stable" {
		t.Fatalf("expected [libvirtd stable] attribution, got %v", got)
	}
}

// Empty index yields empty attribution (everything lands in default suite).
func TestAttributionEmptyIndex(t *testing.T) {
	mgr := index.NewManager()
	if attr := attributionByFilename(mgr.GetIndex()); len(attr) != 0 {
		t.Fatalf("expected empty attribution, got %v", attr)
	}
}
