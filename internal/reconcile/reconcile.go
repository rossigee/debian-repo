// Package reconcile provides index reconciliation from pool file contents.
package reconcile

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	minios3 "github.com/minio/minio-go/v7"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"git.golder.lan/rossgolderltd/debian-repo/internal/validate"
)

// Reconciler rebuilds the index from pool contents (source of truth)
type Reconciler struct {
	minioClient  *minio.Client
	indexMgr     *index.Manager
	defaultSuite string
}

// NewReconciler creates a new reconciler
func NewReconciler(minioClient *minio.Client, indexMgr *index.Manager, defaultSuite string) *Reconciler {
	if defaultSuite == "" {
		defaultSuite = "stable" // backward compatibility
	}
	return &Reconciler{
		minioClient:  minioClient,
		indexMgr:     indexMgr,
		defaultSuite: defaultSuite,
	}
}

// Discrepancy represents a difference between index and pool
type Discrepancy struct {
	Kind    string // "orphan_pool_file", "missing_from_index", "checksum_mismatch"
	Message string
}

// ProgressFunc reports the progress of a long-running operation.
type ProgressFunc func(processed, total int)

// sidecarIsFresh checks if a sidecar's ETag matches the current object's ETag
func sidecarIsFresh(sidecar *model.ChecksumSidecarV1, stat minios3.ObjectInfo) bool {
	if sidecar == nil {
		return false
	}
	return sidecar.ETag == stat.ETag
}

// buildPackageVersionFromDebInfo constructs a PackageVersion from DebInfo and object stat
func buildPackageVersionFromDebInfo(debInfo *validate.DebInfo, objKey string, stat minios3.ObjectInfo) *model.PackageVersion {
	return &model.PackageVersion{
		Version:      debInfo.Version,
		Architecture: debInfo.Architecture,
		Filename:     objKey,
		Checksums: model.Checksums{
			MD5:    debInfo.MD5,
			SHA1:   debInfo.SHA1,
			SHA256: debInfo.SHA256,
			Size:   debInfo.Size,
		},
		ControlFields: debInfo.ControlFields,
		UploadedAt:    stat.LastModified,
		UploadedBy:    "reconcile",
	}
}

// attributionByFilename maps pool object keys to the suites that already
// index them. Pool paths contain no suite segment, so a file already
// attributed to a suite must keep that attribution across reconciliations
// instead of being collapsed into the default suite. Suites are listed in
// sorted order for determinism.
func attributionByFilename(idx *model.Index) map[string][]string {
	out := make(map[string][]string)

	idx.Mu.RLock()
	suites := make([]string, 0, len(idx.Distributions))
	for suite := range idx.Distributions {
		suites = append(suites, suite)
	}
	sort.Strings(suites)

	for _, suite := range suites {
		dist := idx.Distributions[suite]
		dist.Mu.RLock()
		for _, comp := range dist.Components {
			for _, pkg := range comp.Packages {
				for _, ver := range pkg.Versions {
					existing := out[ver.Filename]
					if len(existing) > 0 && existing[len(existing)-1] == suite {
						continue
					}
					out[ver.Filename] = append(existing, suite)
				}
			}
		}
		dist.Mu.RUnlock()
	}
	idx.Mu.RUnlock()

	return out
}

// Reconcile rebuilds the index from pool contents (slow but authoritative)
// Returns the rebuilt index and a list of discrepancies found
// onProgress is called with (examined, total) for each object processed, or nil if progress updates are not needed
func (r *Reconciler) Reconcile(ctx context.Context, onProgress ProgressFunc) (*model.Index, []Discrepancy, error) {
	newIndex := model.NewIndex()
	newMgr := index.NewManager()
	newMgr.SetIndex(newIndex)

	// Snapshot current suite attribution before scanning: files already
	// indexed in a suite stay in that suite; only new files land in the
	// default suite.
	currentIndex := r.indexMgr.GetIndex()
	attribution := attributionByFilename(currentIndex)

	var discrepancies []Discrepancy
	pkgCount := 0

	// First pass: count total .deb files in pool/
	slog.Info("reconcile: counting pool packages")
	total := 0
	for obj := range r.minioClient.ListObjects(ctx, "pool/") {
		if obj.Err != nil {
			return nil, nil, fmt.Errorf("failed to list pool objects: %w", obj.Err)
		}
		if bytes.HasSuffix([]byte(obj.Key), []byte(".deb")) {
			total++
		}
	}
	slog.Info("reconcile: pool scan starting", "total_packages", total)

	// Second pass: process all .deb files with progress tracking
	examined := 0
	lastProgressLog := time.Now()
	startTime := time.Now()

	for obj := range r.minioClient.ListObjects(ctx, "pool/") {
		if obj.Err != nil {
			return nil, nil, fmt.Errorf("failed to list pool objects: %w", obj.Err)
		}

		if !bytes.HasSuffix([]byte(obj.Key), []byte(".deb")) {
			continue
		}

		examined++
		if onProgress != nil {
			onProgress(examined, total)
		}

		// Time-throttled progress logging (every 5 seconds or on final item)
		now := time.Now()
		elapsed := now.Sub(startTime)
		shouldLog := elapsed-lastProgressLog.Sub(startTime) >= 5*time.Second || examined == total
		if shouldLog && total > 0 {
			pct := float64(examined) / float64(total) * 100
			remaining := total - examined
			var eta time.Duration
			if examined > 0 {
				eta = time.Duration(float64(elapsed) / float64(examined) * float64(remaining))
			}
			slog.Info("reconcile: progress", "processed", examined, "total", total,
				"percent", fmt.Sprintf("%.1f", pct), "elapsed", elapsed.Round(time.Second), "eta", eta.Round(time.Second))
			lastProgressLog = now
		}

		// Get object metadata
		stat, err := r.minioClient.StatObject(ctx, obj.Key)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to stat object %s: %w", obj.Key, err)
		}

		// Extract component from path: pool/{component}/{filename}
		pathParts := bytes.Split([]byte(obj.Key), []byte("/"))
		if len(pathParts) < 2 {
			discrepancies = append(discrepancies, Discrepancy{
				Kind:    "invalid_pool_path",
				Message: fmt.Sprintf("%s: unexpected path structure", obj.Key),
			})
			continue
		}
		component := string(pathParts[1])

		// Preserve existing suite attribution; only new files go to the
		// default suite. A file indexed in several suites is kept in all
		// of them.
		suites := attribution[obj.Key]
		if len(suites) == 0 {
			suites = []string{r.defaultSuite}
		}

		// Try sidecar cache first (fast path)
		sidecarStore := minio.NewChecksumSidecarStore(r.minioClient)
		sidecar, err := sidecarStore.GetChecksumSidecar(ctx, obj.Key)
		if err != nil {
			slog.Warn("failed to read checksum sidecar", "object", obj.Key, "error", err)
			// Fall through to full-hash path
			sidecar = nil
		}

		var debInfo *validate.DebInfo

		// Check if sidecar cache is fresh (ETag hasn't changed)
		if sidecar != nil && sidecarIsFresh(sidecar, stat) {
			// Cache hit: reconstruct DebInfo from sidecar
			debInfo = &validate.DebInfo{
				Package:       sidecar.Package,
				Version:       sidecar.Version,
				Architecture:  sidecar.Architecture,
				ControlFields: sidecar.ControlFields,
				MD5:           sidecar.MD5,
				SHA1:          sidecar.SHA1,
				SHA256:        sidecar.SHA256,
				Size:          sidecar.Size,
			}
		} else {
			// Cache miss or stale: full-hash path
			// Fetch and parse .deb control data
			// NOTE: validate.Deb hashes the entire input stream (all ar members),
			// so we must provide the full reader. It extracts control.tar but continues
			// reading to hash data.tar.gz and other members.
			debReader, err := r.minioClient.GetObject(ctx, obj.Key)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get object %s: %w", obj.Key, err)
			}

			debInfo, err = validate.Deb(debReader)
			_ = debReader.Close()
			if err != nil {
				discrepancies = append(discrepancies, Discrepancy{
					Kind:    "invalid_deb_file",
					Message: fmt.Sprintf("%s: %v", obj.Key, err),
				})
				continue
			}

			// Correct the size to the actual object size
			debInfo.Size = stat.Size

			// Fire-and-forget sidecar write for next reconciliation (use background context)
			go func(key string, info *validate.DebInfo, objectStat minios3.ObjectInfo) {
				newSidecar := &model.ChecksumSidecarV1{
					FormatVersion: 1,
					Package:       info.Package,
					Version:       info.Version,
					Architecture:  info.Architecture,
					ControlFields: info.ControlFields,
					MD5:           info.MD5,
					SHA1:          info.SHA1,
					SHA256:        info.SHA256,
					Size:          info.Size,
					ETag:          objectStat.ETag,
					WrittenAt:     time.Now().Format(time.RFC3339),
				}
				if err := sidecarStore.PutChecksumSidecar(context.Background(), key, newSidecar); err != nil {
					slog.Warn("failed to write checksum sidecar during reconcile", "object", key, "error", err)
				}
			}(obj.Key, debInfo, stat)
		}

		// Note: We trust the checksums calculated by validate.Deb which streams the entire file.
		// MinIO's ETag is not reliable: it's only SHA256 for single-part uploads, and
		// MD5(concatenated MD5s) for multi-part uploads. We never override with ETag.

		// Build PackageVersion using shared helper
		ver := buildPackageVersionFromDebInfo(debInfo, obj.Key, stat)

		// Add to index (in every attributed suite)
		for _, suite := range suites {
			if err := newMgr.AddPackageVersion(suite, component, debInfo.Package, ver); err != nil {
				return nil, nil, fmt.Errorf("failed to add package version: %w", err)
			}
		}
		pkgCount++
	}

	slog.Info("reconcile: pool scan complete", "packages_found", pkgCount, "discrepancies", len(discrepancies))

	// Compare with current index to find discrepancies
	discrepancies = append(discrepancies, r.compareIndexes(currentIndex, newIndex)...)

	slog.Info("reconcile: complete", "packages_registered", pkgCount, "total_discrepancies", len(discrepancies),
		"distributions", len(newIndex.Distributions))

	return newIndex, discrepancies, nil
}

// compareIndexes finds differences between two indexes
func (r *Reconciler) compareIndexes(old, updated *model.Index) []Discrepancy {
	var discrepancies []Discrepancy

	// Find packages in old but not in updated (orphans)
	old.Mu.RLock()
	for oldSuite, oldDist := range old.Distributions {
		oldDist.Mu.RLock()
		for oldComp, oldComponent := range oldDist.Components {
			for oldPkg, oldPackage := range oldComponent.Packages {
				// Check if this exists in updated index
				newDist, ok := updated.Distributions[oldSuite]
				if !ok {
					discrepancies = append(discrepancies, Discrepancy{
						Kind:    "missing_from_pool",
						Message: fmt.Sprintf("%s/%s/%s: entire suite missing from pool", oldSuite, oldComp, oldPkg),
					})
					continue
				}

				newDist.Mu.RLock()
				newComponent, ok := newDist.Components[oldComp]
				if !ok {
					discrepancies = append(discrepancies, Discrepancy{
						Kind:    "missing_from_pool",
						Message: fmt.Sprintf("%s/%s/%s: component missing from pool", oldSuite, oldComp, oldPkg),
					})
					newDist.Mu.RUnlock()
					continue
				}

				newPackage, ok := newComponent.Packages[oldPkg]
				if !ok {
					discrepancies = append(discrepancies, Discrepancy{
						Kind:    "missing_from_pool",
						Message: fmt.Sprintf("%s/%s/%s: package missing from pool", oldSuite, oldComp, oldPkg),
					})
					newDist.Mu.RUnlock()
					continue
				}

				// Check each version/arch
				for _, oldVer := range oldPackage.Versions {
					key := oldVer.Version + ":" + oldVer.Architecture
					newVer, ok := newPackage.Versions[key]
					if !ok {
						discrepancies = append(discrepancies, Discrepancy{
							Kind:    "missing_from_pool",
							Message: fmt.Sprintf("%s: version %s/%s missing from pool", oldPkg, oldVer.Version, oldVer.Architecture),
						})
					} else if oldVer.Checksums.SHA256 != newVer.Checksums.SHA256 {
						discrepancies = append(discrepancies, Discrepancy{
							Kind:    "checksum_mismatch",
							Message: fmt.Sprintf("%s: %s/%s SHA256 mismatch (old=%s, new=%s)", oldPkg, oldVer.Version, oldVer.Architecture, oldVer.Checksums.SHA256, newVer.Checksums.SHA256),
						})
					}
				}
				newDist.Mu.RUnlock()
			}
		}
		oldDist.Mu.RUnlock()
	}
	old.Mu.RUnlock()

	return discrepancies
}
