package apiserver

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// PackageSummary represents a single package's latest version info
type PackageSummary struct {
	LatestVersion          string   `json:"latest_version"`
	LatestArchitecture     string   `json:"latest_architecture"`
	AvailableArchitectures []string `json:"available_architectures"`
	LatestSizeBytes        int64    `json:"latest_size_bytes"`
	UploadedAt             string   `json:"uploaded_at"`
	Description            string   `json:"description,omitempty"`
	Maintainer             string   `json:"maintainer,omitempty"`
	Section                string   `json:"section,omitempty"`
}

// IndexJSON represents the complete package index in JSON format
type IndexJSON struct {
	Packages      map[string]map[string]PackageSummary `json:"packages"` // [suite][package_name]
	Distributions []string                             `json:"distributions"`
	Components    []string                             `json:"components"`
	GeneratedAt   string                               `json:"generated_at"`
}

// handleIndexJSON serves a JSON summary of the latest package versions
func (s *Server) handleIndexJSON(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	result := IndexJSON{
		Packages:      make(map[string]map[string]PackageSummary),
		Distributions: []string{},
		Components:    []string{},
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	componentSet := make(map[string]bool)

	// Iterate through all distributions
	for _, suite := range rp.IndexMgr.ListDistributions() {
		dist, ok := rp.IndexMgr.GetDistribution(suite)
		if !ok {
			continue
		}

		result.Distributions = append(result.Distributions, suite)
		result.Packages[suite] = make(map[string]PackageSummary)

		dist.Mu.RLock()

		// Iterate through components and packages
		for componentName, component := range dist.Components {
			componentSet[componentName] = true

			for packageName, pkg := range component.Packages {
				// Find the latest version (by upload time, then version string)
				var latestVersion *model.PackageVersion

				for _, ver := range pkg.Versions {
					if latestVersion == nil ||
						ver.UploadedAt.After(latestVersion.UploadedAt) ||
						(ver.UploadedAt.Equal(latestVersion.UploadedAt) && ver.Version > latestVersion.Version) {
						latestVersion = ver
					}
				}

				if latestVersion == nil {
					continue
				}

				// Collect all unique architectures for this package
				archs := make(map[string]bool)
				for _, ver := range pkg.Versions {
					archs[ver.Architecture] = true
				}
				archList := make([]string, 0, len(archs))
				for arch := range archs {
					archList = append(archList, arch)
				}
				sort.Strings(archList)

				summary := PackageSummary{
					LatestVersion:          latestVersion.Version,
					LatestArchitecture:     latestVersion.Architecture,
					AvailableArchitectures: archList,
					LatestSizeBytes:        latestVersion.Checksums.Size,
					UploadedAt:             latestVersion.UploadedAt.UTC().Format(time.RFC3339),
					Description:            latestVersion.ControlFields["Description"],
					Maintainer:             latestVersion.ControlFields["Maintainer"],
					Section:                latestVersion.ControlFields["Section"],
				}

				result.Packages[suite][packageName] = summary
			}
		}

		dist.Mu.RUnlock()
	}

	// Convert component set to sorted slice
	for comp := range componentSet {
		result.Components = append(result.Components, comp)
	}
	sort.Strings(result.Components)
	sort.Strings(result.Distributions)

	// Encode and write JSON
	w.Header().Set("Cache-Control", "public, max-age=60")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(result)
}
