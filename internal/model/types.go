package model

import (
	"sync"
	"time"
)

// Checksums holds multiple checksum algorithms for a package file
type Checksums struct {
	MD5    string
	SHA1   string
	SHA256 string
	Size   int64
}

// PackageVersion represents a single version of a package for a specific architecture
type PackageVersion struct {
	// Identity
	Version      string `json:"version"`      // e.g. "0.4.4-1"
	Architecture string `json:"architecture"` // e.g. "amd64", "arm64", "all"

	// File location
	Filename string `json:"filename"` // pool object key, e.g. "pool/main/foo_1.0_amd64.deb"

	// Verification
	Checksums Checksums `json:"checksums"`

	// Control file fields (source of truth for package metadata)
	ControlFields map[string]string `json:"control_fields"` // Package, Depends, Recommends, Description, Section, Priority, etc.

	// Audit
	UploadedAt time.Time `json:"uploaded_at"`
	UploadedBy string    `json:"uploaded_by"` // CI token identity
}

// Package represents all versions of a package across all architectures
type Package struct {
	Name     string                     `json:"name"`
	Versions map[string]*PackageVersion `json:"versions"` // version -> PackageVersion (each arch is separate per version)
}

// Component represents a component (e.g. "main", "extra") within a distribution
type Component struct {
	Name     string              `json:"name"`
	Packages map[string]*Package `json:"packages"` // package name -> Package
}

// Distribution represents a suite/distribution (e.g. "stable", "testing")
type Distribution struct {
	// Metadata
	Suite       string `json:"suite"` // e.g. "stable"
	Codename    string `json:"codename"`
	Origin      string `json:"origin"`
	Label       string `json:"label"`
	Description string `json:"description"`

	// Structure
	Components    map[string]*Component `json:"components"`    // component name -> Component
	Architectures []string              `json:"architectures"` // union of all archs seen

	// Rendering
	RenderedPackages   map[string][]byte `json:"-"` // per-arch: binary-amd64 -> Packages bytes
	RenderedRelease    []byte            `json:"-"` // Release file plaintext
	RenderedReleaseGPG []byte            `json:"-"` // Release.gpg detached signature
	RenderedInRelease  []byte            `json:"-"` // InRelease clearsigned

	// Concurrency
	Mu    sync.RWMutex `json:"-"`
	Dirty bool         `json:"-"` // needs re-rendering
}

// Index is the root of the in-memory repository index
type Index struct {
	// Distributions indexed by suite name
	Distributions map[string]*Distribution `json:"distributions"`

	// Version tracking for snapshots
	SnapshotGen uint64 `json:"snapshot_gen"` // incremented on each commit
	LoadedFrom  string `json:"loaded_from"`  // snapshot object key at startup

	// Concurrency (guards the Distributions map itself)
	Mu sync.RWMutex `json:"-"`
}

// NewIndex creates a fresh empty index
func NewIndex() *Index {
	return &Index{
		Distributions: make(map[string]*Distribution),
		SnapshotGen:   0,
	}
}

// NewDistribution creates a new distribution
func NewDistribution(suite, codename, origin, label, description string) *Distribution {
	return &Distribution{
		Suite:            suite,
		Codename:         codename,
		Origin:           origin,
		Label:            label,
		Description:      description,
		Components:       make(map[string]*Component),
		Architectures:    []string{},
		RenderedPackages: make(map[string][]byte),
		Dirty:            true,
	}
}

// NewComponent creates a new component
func NewComponent(name string) *Component {
	return &Component{
		Name:     name,
		Packages: make(map[string]*Package),
	}
}

// NewPackage creates a new package
func NewPackage(name string) *Package {
	return &Package{
		Name:     name,
		Versions: make(map[string]*PackageVersion),
	}
}

// RenderedDist is an atomic snapshot of all rendered artifacts for a distribution
// This is what GET handlers read, never blocking on writes
type RenderedDist struct {
	Suite            string
	ReleasePlain     []byte
	ReleaseGPG       []byte
	InRelease        []byte
	PackagesByArch   map[string][]byte // arch -> Packages bytes
	PackagesGzByArch map[string][]byte // arch -> Packages.gz bytes
	RenderedAt       time.Time
}
