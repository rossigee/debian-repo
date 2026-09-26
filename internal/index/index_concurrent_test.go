package index

import (
	"fmt"
	"sync"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// TestConcurrentMutations hammers Add/Remove against concurrent readers and
// rendered-dist access under the race detector. It guards the lock
// discipline: dist.Mu for distribution contents, m.mu for the manager maps.
func TestConcurrentMutations(t *testing.T) {
	mgr := NewManager()

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				pkg := fmt.Sprintf("pkg-%d", (w+i)%16)
				ver := &model.PackageVersion{
					Version:       fmt.Sprintf("1.%d", i),
					Architecture:  "amd64",
					Filename:      fmt.Sprintf("pool/main/%s_1.%d_amd64.deb", pkg, i),
					ControlFields: map[string]string{"Package": pkg},
				}
				_ = mgr.AddPackageVersion("stable", "main", pkg, ver)
				_ = mgr.CountPackages("stable")
				_ = mgr.ListArchitectures("stable")
				_ = mgr.GetRenderedDist("stable")
				_ = mgr.GetIndex()
				_ = mgr.RemovePackageVersion("stable", "main", pkg, ver.Version, ver.Architecture)
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = mgr.ListDistributions()
			_ = mgr.GetSnapshotGen()
			_ = mgr.IncrementSnapshot()
			_ = mgr.ToSnapshot("race-test")
			_ = mgr.SetRenderedDist("stable", nil)
		}
	}()
	wg.Wait()

	// Manager must still be usable afterwards.
	if err := mgr.AddPackageVersion("stable", "main", "final", &model.PackageVersion{
		Version: "9.9", Architecture: "amd64", Filename: "pool/main/final_9.9_amd64.deb",
	}); err != nil {
		t.Fatalf("AddPackageVersion after hammering failed: %v", err)
	}
}

// TestConcurrentLoadSwap races index replacement (reconcile-apply path)
// against readers. Load must not race with GetDistribution/ListDistributions.
func TestConcurrentLoadSwap(t *testing.T) {
	mgr := NewManager()
	if err := mgr.AddPackageVersion("stable", "main", "a", &model.PackageVersion{
		Version: "1.0", Architecture: "amd64", Filename: "pool/main/a_1.0_amd64.deb",
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, _ = mgr.GetDistribution("stable")
				_ = mgr.ListDistributions()
				_ = mgr.GetRenderedDist("stable")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			mgr.Load(model.NewIndex())
		}
	}()
	wg.Wait()
}
