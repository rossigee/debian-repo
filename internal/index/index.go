// Package index provides in-memory index management for Debian package distributions,
// with atomic snapshot serialization and thread-safe concurrent access via per-distribution locking.
package index

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// FeedEntry represents a single package version for feed syndication
type FeedEntry struct {
	Package       string
	Version       string
	Architecture  string
	Component     string
	Filename      string
	UploadedAt    time.Time
	ControlFields map[string]string
}

// Manager handles the in-memory index with atomic rendering.
//
// Locking discipline (global order, never inverted):
//  1. m.mu guards the idx pointer and the rendered map itself
//  2. idx.Mu guards the Distributions map
//  3. dist.Mu guards a single distribution's contents
type Manager struct {
	mu  sync.RWMutex
	idx *model.Index

	// Per-distribution rendered artifacts, swapped atomically
	rendered map[string]*atomic.Pointer[model.RenderedDist]
}

// NewManager creates a fresh index manager
func NewManager() *Manager {
	return &Manager{
		idx:      model.NewIndex(),
		rendered: make(map[string]*atomic.Pointer[model.RenderedDist]),
	}
}

// Load populates the manager from an existing index (e.g. from a snapshot)
func (m *Manager) Load(idx *model.Index) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idx = idx
	m.rendered = make(map[string]*atomic.Pointer[model.RenderedDist])
	for suite := range idx.Distributions {
		m.rendered[suite] = &atomic.Pointer[model.RenderedDist]{}
	}
}

// GetDistribution returns a distribution by suite name (read-only)
func (m *Manager) GetDistribution(suite string) (*model.Distribution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.idx.Mu.RLock()
	defer m.idx.Mu.RUnlock()
	dist, ok := m.idx.Distributions[suite]
	return dist, ok
}

// GetOrCreateDistribution lazily creates a distribution if it doesn't exist
func (m *Manager) GetOrCreateDistribution(suite, codename, origin, label, description string) *model.Distribution {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idx.Mu.Lock()
	defer m.idx.Mu.Unlock()

	if dist, ok := m.idx.Distributions[suite]; ok {
		return dist
	}

	dist := model.NewDistribution(suite, codename, origin, label, description)
	m.idx.Distributions[suite] = dist

	// Create atomic pointer for rendered artifacts
	m.rendered[suite] = &atomic.Pointer[model.RenderedDist]{}

	return dist
}

// ListDistributions returns all known suite names
func (m *Manager) ListDistributions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.idx.Mu.RLock()
	defer m.idx.Mu.RUnlock()

	suites := make([]string, 0, len(m.idx.Distributions))
	for suite := range m.idx.Distributions {
		suites = append(suites, suite)
	}
	return suites
}

// GetSnapshotGen returns the current snapshot generation number
func (m *Manager) GetSnapshotGen() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.idx.Mu.RLock()
	defer m.idx.Mu.RUnlock()
	return m.idx.SnapshotGen
}

// IncrementSnapshot bumps the generation counter and returns the new value
func (m *Manager) IncrementSnapshot() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idx.Mu.Lock()
	defer m.idx.Mu.Unlock()
	m.idx.SnapshotGen++
	return m.idx.SnapshotGen
}

// GetIndex returns a snapshot of the current index (shallow copy of distributions map)
func (m *Manager) GetIndex() *model.Index {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.idx.Mu.RLock()
	defer m.idx.Mu.RUnlock()

	// Return a copy of the index for snapshot purposes
	indexCopy := &model.Index{
		Distributions: make(map[string]*model.Distribution),
		SnapshotGen:   m.idx.SnapshotGen,
		LoadedFrom:    m.idx.LoadedFrom,
	}

	for suite, dist := range m.idx.Distributions {
		indexCopy.Distributions[suite] = dist
	}

	return indexCopy
}

// AddPackageVersion adds or updates a package version in the index
// This is the primary mutation point, and it handles atomic rendering
func (m *Manager) AddPackageVersion(suite, component, pkgName string, ver *model.PackageVersion) error {
	// Get or create distribution
	dist := m.GetOrCreateDistribution(suite, suite, "myorgname.com", "golder.tech Debian Repository", "")

	dist.Mu.Lock()
	defer dist.Mu.Unlock()

	// Get or create component
	comp, ok := dist.Components[component]
	if !ok {
		comp = model.NewComponent(component)
		dist.Components[component] = comp
	}

	// Get or create package
	pkg, ok := comp.Packages[pkgName]
	if !ok {
		pkg = model.NewPackage(pkgName)
		comp.Packages[pkgName] = pkg
	}

	// Store this version using composite key of version and architecture
	// This allows multiple architectures for the same package version
	key := ver.Version + ":" + ver.Architecture
	pkg.Versions[key] = ver

	// Update distribution's architecture list (keep unique, sorted)
	m.addArch(dist, ver.Architecture)

	// Mark as needing re-rendering
	dist.Dirty = true

	return nil
}

// addArch adds an architecture to the distribution's list if not already present
func (m *Manager) addArch(dist *model.Distribution, arch string) {
	for _, a := range dist.Architectures {
		if a == arch {
			return
		}
	}
	dist.Architectures = append(dist.Architectures, arch)
}

// RemovePackageVersion removes a package version from a distribution
// Returns error if the package/version/architecture combination is not found
//
// Locking: the distribution map is looked up under idx.Mu (released
// immediately), then all mutations happen under dist.Mu — the same lock
// every other accessor uses. Holding idx.Mu across the mutation (as before)
// is not sufficient and races with concurrent readers.
func (m *Manager) RemovePackageVersion(suite, component, pkgName, version, architecture string) error {
	// Look up the distribution (released before taking the dist lock to
	// match AddPackageVersion's lock discipline and avoid nesting)
	m.mu.RLock()
	m.idx.Mu.RLock()
	dist, ok := m.idx.Distributions[suite]
	m.idx.Mu.RUnlock()
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("distribution %s not found", suite)
	}

	dist.Mu.Lock()
	defer dist.Mu.Unlock()

	// Get or create component
	comp, ok := dist.Components[component]
	if !ok {
		return fmt.Errorf("component %s not found in distribution %s", component, suite)
	}

	// Get or create package
	pkg, ok := comp.Packages[pkgName]
	if !ok {
		return fmt.Errorf("package %s not found in component %s of distribution %s", pkgName, component, suite)
	}

	// Remove the specific version/architecture entry
	key := version + ":" + architecture
	if _, ok := pkg.Versions[key]; !ok {
		return fmt.Errorf("package %s version %s architecture %s not found in component %s of distribution %s", pkgName, version, architecture, component, suite)
	}

	delete(pkg.Versions, key)

	// If no more versions for this package, remove the package
	if len(pkg.Versions) == 0 {
		delete(comp.Packages, pkgName)
	}

	// If no more packages in this component, remove the component
	if len(comp.Packages) == 0 {
		delete(dist.Components, component)
	}

	// Update distribution's architecture list (recalculate from remaining packages)
	dist.Architectures = nil
	for _, c := range dist.Components {
		for _, p := range c.Packages {
			for _, v := range p.Versions {
				m.addArch(dist, v.Architecture)
			}
		}
	}

	dist.Dirty = true

	return nil
}

// GetRenderedDist returns the current rendered artifacts for a distribution (non-blocking)
// Returns nil if nothing has been rendered yet
func (m *Manager) GetRenderedDist(suite string) *model.RenderedDist {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ptr, ok := m.rendered[suite]
	if !ok {
		return nil
	}
	return ptr.Load()
}

// SetRenderedDist atomically updates the rendered artifacts for a distribution
func (m *Manager) SetRenderedDist(suite string, rd *model.RenderedDist) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ptr, ok := m.rendered[suite]
	if !ok {
		return fmt.Errorf("distribution %s not found", suite)
	}
	ptr.Store(rd)
	return nil
}

// ToSnapshot serializes the entire index to a model.SnapshotV1
func (m *Manager) ToSnapshot(generatedBy string) *model.SnapshotV1 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.idx.ToSnapshot(generatedBy)
}

// SetIndex replaces the entire index (used when loading from a snapshot)
func (m *Manager) SetIndex(idx *model.Index) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idx = idx
	m.rendered = make(map[string]*atomic.Pointer[model.RenderedDist])
	for suite := range idx.Distributions {
		m.rendered[suite] = &atomic.Pointer[model.RenderedDist]{}
	}
}

// CountPackages returns the total number of unique packages in a distribution
func (m *Manager) CountPackages(suite string) int {
	dist, ok := m.GetDistribution(suite)
	if !ok {
		return 0
	}

	dist.Mu.RLock()
	defer dist.Mu.RUnlock()

	total := 0
	for _, comp := range dist.Components {
		total += len(comp.Packages)
	}
	return total
}

// ListArchitectures returns the architectures in a distribution
func (m *Manager) ListArchitectures(suite string) []string {
	dist, ok := m.GetDistribution(suite)
	if !ok {
		return nil
	}

	dist.Mu.RLock()
	defer dist.Mu.RUnlock()
	return append([]string{}, dist.Architectures...)
}

// SnapshotGen returns the current snapshot generation number (for metrics interface)
func (m *Manager) SnapshotGen() uint64 {
	return m.GetSnapshotGen()
}

// RecentVersions returns the most recently uploaded package versions for a suite,
// limited to the given count and sorted by upload time (newest first).
func (m *Manager) RecentVersions(suite string, limit int) []FeedEntry {
	dist, ok := m.GetDistribution(suite)
	if !ok {
		return nil
	}

	dist.Mu.RLock()
	defer dist.Mu.RUnlock()

	var entries []FeedEntry
	for compName, comp := range dist.Components {
		for _, pkg := range comp.Packages {
			for _, ver := range pkg.Versions {
				entries = append(entries, FeedEntry{
					Package:       pkg.Name,
					Version:       ver.Version,
					Architecture:  ver.Architecture,
					Component:     compName,
					Filename:      ver.Filename,
					UploadedAt:    ver.UploadedAt,
					ControlFields: ver.ControlFields,
				})
			}
		}
	}

	// Sort by upload time, newest first
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].UploadedAt.After(entries[i].UploadedAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Limit to requested count
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	return entries
}
