package apiserver

import (
	"context"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

const lockTestPoolKey = "pool/main/test-pkg_1.0.0_amd64.deb"

// An upload holds the pool lock from writing the object until it is
// registered. A removal that reaches the reaper in that window must wait, and
// once the upload has registered the object it must not delete it.
func TestReapPoolObjectsWaitsForInFlightUpload(t *testing.T) {
	rp := &repo.Repo{IndexMgr: index.NewManager()}

	// The upload has stored the object and holds the lock; not yet registered.
	rp.PoolMu.Lock()

	var removed []string
	done := make(chan struct{})
	go func() {
		reapPoolObjects(context.Background(), rp, []string{lockTestPoolKey},
			func(_ context.Context, key string) error {
				removed = append(removed, key)
				return nil
			})
		close(done)
	}()

	// The upload registers the object while still holding the lock.
	if err := rp.IndexMgr.AddPackageVersion("stable", "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     lockTestPoolKey,
	}); err != nil {
		t.Fatalf("AddPackageVersion: %v", err)
	}
	rp.PoolMu.Unlock()

	<-done
	if len(removed) != 0 {
		t.Fatalf("object registered by an in-flight upload was deleted: %v", removed)
	}
}

// With no remaining references the reaper deletes the object.
func TestReapPoolObjectsDeletesUnreferenced(t *testing.T) {
	rp := &repo.Repo{IndexMgr: index.NewManager()}

	var removed []string
	reapPoolObjects(context.Background(), rp, []string{lockTestPoolKey},
		func(_ context.Context, key string) error {
			removed = append(removed, key)
			return nil
		})

	if len(removed) != 1 || removed[0] != lockTestPoolKey {
		t.Fatalf("removed = %v, want [%s]", removed, lockTestPoolKey)
	}
}
