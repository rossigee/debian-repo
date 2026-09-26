// Package model defines the core data structures for Debian package repository management.
package model

import (
	"time"
)

// SnapshotV1 is the serializable format for the index, persisted to MinIO
type SnapshotV1 struct {
	FormatVersion int64                  `json:"format_version"` // = 1 for this schema
	GeneratedAt   time.Time              `json:"generated_at"`
	GeneratedBy   string                 `json:"generated_by"` // hostname/version string
	SnapshotGen   uint64                 `json:"snapshot_gen"`
	Distributions []DistributionSnapshot `json:"distributions"`
}

// DistributionSnapshot is a serializable distribution (without the sync.RWMutex or rendered bytes)
type DistributionSnapshot struct {
	Suite         string              `json:"suite"`
	Codename      string              `json:"codename"`
	Origin        string              `json:"origin"`
	Label         string              `json:"label"`
	Description   string              `json:"description"`
	Architectures []string            `json:"architectures"`
	Components    []ComponentSnapshot `json:"components"`
}

// ComponentSnapshot is a serializable component
type ComponentSnapshot struct {
	Name     string            `json:"name"`
	Packages []PackageSnapshot `json:"packages"`
}

// PackageSnapshot is a serializable package (all versions)
type PackageSnapshot struct {
	Name     string                   `json:"name"`
	Versions []PackageVersionSnapshot `json:"versions"`
}

// PackageVersionSnapshot is a serializable version
type PackageVersionSnapshot struct {
	Version       string            `json:"version"`
	Architecture  string            `json:"architecture"`
	Filename      string            `json:"filename"`
	Checksums     Checksums         `json:"checksums"`
	ControlFields map[string]string `json:"control_fields"`
	UploadedAt    time.Time         `json:"uploaded_at"`
	UploadedBy    string            `json:"uploaded_by"`
}

// ToSnapshot converts an in-memory Index to a SnapshotV1
func (idx *Index) ToSnapshot(generatedBy string) *SnapshotV1 {
	idx.Mu.RLock()
	defer idx.Mu.RUnlock()

	snap := &SnapshotV1{
		FormatVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		GeneratedBy:   generatedBy,
		SnapshotGen:   idx.SnapshotGen,
		Distributions: make([]DistributionSnapshot, 0, len(idx.Distributions)),
	}

	for _, dist := range idx.Distributions {
		dist.Mu.RLock()
		distSnap := DistributionSnapshot{
			Suite:         dist.Suite,
			Codename:      dist.Codename,
			Origin:        dist.Origin,
			Label:         dist.Label,
			Description:   dist.Description,
			Architectures: append([]string{}, dist.Architectures...),
			Components:    make([]ComponentSnapshot, 0, len(dist.Components)),
		}

		for _, comp := range dist.Components {
			compSnap := ComponentSnapshot{
				Name:     comp.Name,
				Packages: make([]PackageSnapshot, 0, len(comp.Packages)),
			}

			for _, pkg := range comp.Packages {
				pkgSnap := PackageSnapshot{
					Name:     pkg.Name,
					Versions: make([]PackageVersionSnapshot, 0, len(pkg.Versions)),
				}

				for _, ver := range pkg.Versions {
					verSnap := PackageVersionSnapshot{
						Version:       ver.Version,
						Architecture:  ver.Architecture,
						Filename:      ver.Filename,
						Checksums:     ver.Checksums,
						ControlFields: ver.ControlFields,
						UploadedAt:    ver.UploadedAt,
						UploadedBy:    ver.UploadedBy,
					}
					pkgSnap.Versions = append(pkgSnap.Versions, verSnap)
				}
				compSnap.Packages = append(compSnap.Packages, pkgSnap)
			}
			distSnap.Components = append(distSnap.Components, compSnap)
		}
		snap.Distributions = append(snap.Distributions, distSnap)
		dist.Mu.RUnlock()
	}

	return snap
}

// FromSnapshot converts a SnapshotV1 back to an in-memory Index
func FromSnapshot(snap *SnapshotV1, sourceKey string) *Index {
	idx := NewIndex()
	idx.SnapshotGen = snap.SnapshotGen
	idx.LoadedFrom = sourceKey

	for _, distSnap := range snap.Distributions {
		dist := NewDistribution(distSnap.Suite, distSnap.Codename, distSnap.Origin, distSnap.Label, distSnap.Description)
		dist.Architectures = append([]string{}, distSnap.Architectures...)

		for _, compSnap := range distSnap.Components {
			comp := NewComponent(compSnap.Name)

			for _, pkgSnap := range compSnap.Packages {
				pkg := NewPackage(pkgSnap.Name)

				for _, verSnap := range pkgSnap.Versions {
					ver := &PackageVersion{
						Version:       verSnap.Version,
						Architecture:  verSnap.Architecture,
						Filename:      verSnap.Filename,
						Checksums:     verSnap.Checksums,
						ControlFields: verSnap.ControlFields,
						UploadedAt:    verSnap.UploadedAt,
						UploadedBy:    verSnap.UploadedBy,
					}
					key := ver.Version + ":" + ver.Architecture
					pkg.Versions[key] = ver
				}
				comp.Packages[pkg.Name] = pkg
			}
			dist.Components[comp.Name] = comp
		}
		idx.Distributions[dist.Suite] = dist
	}

	return idx
}
