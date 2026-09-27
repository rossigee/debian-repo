package repo

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/aptmeta"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// SnapshotLoader fetches the most recent index snapshot for a repository.
// *minio.SnapshotStore satisfies it; tests substitute a stub so hydration can be
// exercised without MinIO.
type SnapshotLoader interface {
	GetLatestSnapshot(ctx context.Context) (*model.SnapshotV1, error)
}

// BucketEnsurer verifies a repository's bucket exists, creating it if needed.
// *minio.Client satisfies it.
type BucketEnsurer interface {
	EnsureBucket(ctx context.Context) error
}

// HydrateDeps are the storage dependencies hydration needs. Both are interfaces
// so tests can substitute stubs; the concrete MinIO types satisfy them as-is.
type HydrateDeps struct {
	Loader  SnapshotLoader
	Ensurer BucketEnsurer
}

// HydrateAll loads and renders every repository concurrently, so one slow
// repository does not delay the others, retrying any that fail until they are
// hydrated or ctx is cancelled. It is intended to run in the background after the
// listener is up: until it finishes, repos report themselves unhydrated and the
// metadata routes answer 503 with Retry-After.
func HydrateAll(ctx context.Context, repos []*Repo) {
	HydrateAllWithInterval(ctx, repos, HydrationRetryInterval)
}

// HydrationRetryInterval is how long HydrateAll waits before retrying a
// repository that could not be hydrated.
const HydrationRetryInterval = 5 * time.Second

// HydrateAllWithInterval retries failed repositories on the given interval. It
// returns once every repository is hydrated or ctx is cancelled.
func HydrateAllWithInterval(ctx context.Context, repos []*Repo, interval time.Duration) {
	retryUntilHydrated(ctx, repos, interval, func(rp *Repo) error {
		// Convert the concrete store to the interface only when it is non-nil:
		// passing a nil *minio.SnapshotStore directly would produce a non-nil
		// interface holding a nil pointer, which defeats the nil check in
		// loadSnapshot.
		var loader SnapshotLoader
		if rp.SnapshotStore != nil {
			loader = rp.SnapshotStore
		}
		var ensurer BucketEnsurer
		if rp.MinioClient != nil {
			ensurer = rp.MinioClient
		}
		return Hydrate(ctx, rp, HydrateDeps{Loader: loader, Ensurer: ensurer})
	})
}

// retryUntilHydrated calls hydrate for each repository in pending, concurrently,
// and repeats with whichever of them reported an error. It returns as soon as
// every repository has succeeded, or when ctx is cancelled.
//
// Repositories are retried until they succeed rather than a fixed number of
// times: a storage outage must not require a process restart to recover from.
func retryUntilHydrated(ctx context.Context, pending []*Repo, interval time.Duration, hydrate func(*Repo) error) {
	for len(pending) > 0 && ctx.Err() == nil {
		var mu sync.Mutex
		var failed []*Repo
		var wg sync.WaitGroup

		for _, rp := range pending {
			wg.Add(1)
			go func(rp *Repo) {
				defer wg.Done()
				if err := hydrate(rp); err != nil {
					mu.Lock()
					failed = append(failed, rp)
					mu.Unlock()
				}
			}(rp)
		}
		wg.Wait()

		pending = failed
		if len(pending) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Hydrate loads a repository's snapshot, installs it as the live index, and
// renders Release/InRelease/Packages for every suite.
//
// A missing snapshot is not an error: the repository starts from an empty index
// and is marked hydrated, because a new repository legitimately has no snapshot
// yet. Unreachable storage is different. If the bucket cannot be verified, the
// repository is left unhydrated and an error is returned, so callers keep
// answering 503 rather than advertising an empty repository as ready — to an apt
// client that is indistinguishable from every package having been deleted.
func Hydrate(ctx context.Context, rp *Repo, deps HydrateDeps) error {
	started := time.Now()
	slog.Info("hydrating repo", "repo_id", rp.ID)

	if deps.Ensurer != nil {
		if err := deps.Ensurer.EnsureBucket(ctx); err != nil {
			return fmt.Errorf("repo %s: ensuring bucket: %w", rp.ID, err)
		}
	}

	loadSnapshot(ctx, rp, deps.Loader)
	renderDistributions(ctx, rp)

	rp.MarkHydrated()
	slog.Info("repo hydrated", "repo_id", rp.ID, "suites", len(rp.IndexMgr.ListDistributions()), "took", time.Since(started).String())
	return nil
}

// loadSnapshot installs the latest snapshot as the live index, falling back to an
// empty index when no snapshot exists or the loader fails.
func loadSnapshot(ctx context.Context, rp *Repo, loader SnapshotLoader) {
	if loader == nil {
		slog.Info("no snapshot loader configured, starting with empty index", "repo_id", rp.ID)
		rp.IndexMgr.SetIndex(model.NewIndex())
		return
	}

	snap, err := loader.GetLatestSnapshot(ctx)
	switch {
	case err != nil:
		slog.Warn("failed to load snapshot for repo, starting with empty index", "repo_id", rp.ID, "error", err)
		rp.IndexMgr.SetIndex(model.NewIndex())
	case snap == nil:
		slog.Info("no snapshot found for repo, starting with empty index", "repo_id", rp.ID)
		rp.IndexMgr.SetIndex(model.NewIndex())
	default:
		slog.Info("loaded snapshot for repo", "repo_id", rp.ID, "gen", snap.SnapshotGen, "distributions", len(snap.Distributions))
		rp.IndexMgr.SetIndex(model.FromSnapshot(snap, "_meta/index-snapshot.json.gz"))
	}
}

// renderDistributions renders and signs the repository's metadata.
//
// A failure rendering one suite is logged and skipped rather than aborting, so a
// single bad suite cannot leave the whole repository unhydrated.
func renderDistributions(ctx context.Context, rp *Repo) {
	for _, suite := range rp.IndexMgr.ListDistributions() {
		if ctx.Err() != nil {
			return
		}
		dist, ok := rp.IndexMgr.GetDistribution(suite)
		if !ok {
			continue
		}

		releaseData, err := aptmeta.RenderRelease(dist, rp.Metadata.Origin, rp.Metadata.Label, rp.Metadata.Description)
		if err != nil {
			slog.Warn("failed to render release", "repo_id", rp.ID, "suite", suite, "error", err)
			continue
		}

		releaseGPG, err := rp.Signer.DetachSign(releaseData)
		if err != nil {
			slog.Warn("failed to create detached signature", "repo_id", rp.ID, "suite", suite, "error", err)
			continue
		}

		inRelease, err := rp.Signer.ClearSign(releaseData)
		if err != nil {
			slog.Warn("failed to create clearsigned release", "repo_id", rp.ID, "suite", suite, "error", err)
			continue
		}

		packagesByArch, packagesGzByArch := renderPackages(rp, dist)

		rendered := &model.RenderedDist{
			Suite:            suite,
			ReleasePlain:     releaseData,
			ReleaseGPG:       releaseGPG,
			InRelease:        inRelease,
			PackagesByArch:   packagesByArch,
			PackagesGzByArch: packagesGzByArch,
			RenderedAt:       time.Now(),
		}
		if err := rp.IndexMgr.SetRenderedDist(suite, rendered); err != nil {
			slog.Warn("failed to set rendered distribution", "repo_id", rp.ID, "suite", suite, "error", err)
		}
	}
}

// renderPackages renders Packages for each architecture of a distribution, using
// the first component in sorted order.
func renderPackages(rp *Repo, dist *model.Distribution) (byArch, gzByArch map[string][]byte) {
	components := make([]string, 0, len(dist.Components))
	for compName := range dist.Components {
		components = append(components, compName)
	}
	sort.Strings(components)
	if len(components) == 0 {
		return nil, nil
	}
	compName := components[0]

	byArch = make(map[string][]byte)
	gzByArch = make(map[string][]byte)
	for _, arch := range dist.Architectures {
		uncompressed, compressed, err := aptmeta.RenderPackages(dist, compName, arch)
		if err != nil {
			slog.Warn("failed to render packages", "repo_id", rp.ID, "suite", dist.Suite, "component", compName, "arch", arch, "error", err)
			continue
		}
		byArch[arch] = uncompressed
		gzByArch[arch] = compressed
	}
	return byArch, gzByArch
}
