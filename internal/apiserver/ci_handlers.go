package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/acl"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/reconcile"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"git.golder.lan/rossgolderltd/debian-repo/internal/validate"
	"git.golder.lan/rossgolderltd/debian-repo/internal/version"
)

// isValidPathComponent validates that a string is safe for use in object paths
// (no path traversal, slashes, or other dangerous characters)
func isValidPathComponent(s string) bool {
	if s == "" {
		return false
	}
	// Reject path traversal attempts and directory separators
	if strings.Contains(s, "/") || strings.Contains(s, "\\") {
		return false
	}
	// Reject hidden files/parent directory references
	if strings.HasPrefix(s, ".") {
		return false
	}
	return true
}

// isValidFilename allows only safe staging filenames: a bare file name
// (no directories) with deb-name characters.
func isValidFilename(s string) bool {
	if s == "" || s != path.Base(s) {
		return false
	}
	// Reject hidden files/parent directory references
	if strings.HasPrefix(s, ".") {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		if strings.ContainsRune("._+~-", r) {
			continue
		}
		return false
	}
	return true
}

// isValidStagingKey requires a staging key to live directly under the
// configured staging prefix with a safe trailing file name. This keeps
// presigned register calls from reading, overwriting, or deleting
// arbitrary pool, snapshot, or other-repo objects.
func isValidStagingKey(key, stagingPrefix string) bool {
	if stagingPrefix == "" {
		stagingPrefix = "_staging/"
	}
	if !strings.HasPrefix(key, stagingPrefix) {
		return false
	}
	rest := strings.TrimPrefix(key, stagingPrefix)
	if rest == "" || strings.Contains(rest, "/") {
		return false
	}
	return isValidFilename(rest)
}

// handleUpload handles direct .deb uploads (direct mode only)
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.config.UploadMode != "direct" {
		http.Error(w, "Direct upload disabled", http.StatusMethodNotAllowed)
		return
	}

	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Parse query params for suite/component
	suite := r.URL.Query().Get("suite")
	component := r.URL.Query().Get("component")

	if suite == "" {
		suite = rp.DefaultSuite
	}
	if component == "" {
		component = rp.DefaultComponent
	}

	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}

	// Limit request body size (512MB max)
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024*1024)

	// Validate suite/component before proceeding
	if !isValidPathComponent(suite) || !isValidPathComponent(component) {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Check authorization
	if s.denyIfNotAuthorized(w, identity, acl.OpUpload, suite, component, r) {
		return
	}

	// Buffer the body once: validate.Deb drains its reader, so the bytes
	// must be re-readable for the MinIO put below.
	data, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}
	size := int64(len(data))

	// Validate .deb from the buffered bytes
	debInfo, err := validate.Deb(bytes.NewReader(data))
	if err != nil {
		// Return generic error to avoid information leakage
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Validate extracted package metadata before using in path
	if !isValidPathComponent(debInfo.Package) || !isValidPathComponent(debInfo.Version) || !isValidPathComponent(debInfo.Architecture) {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Object key for final pool location
	objectKey := fmt.Sprintf("pool/%s/%s_%s_%s.deb", component, debInfo.Package, debInfo.Version, debInfo.Architecture)

	// Upload to MinIO pool (no staging)
	if rp.MinioClient != nil {
		if err := rp.MinioClient.PutObject(r.Context(), objectKey, bytes.NewReader(data), size); err != nil {
			http.Error(w, "Failed to store package", http.StatusInternalServerError)
			return
		}
	}

	// Create PackageVersion
	ver := &model.PackageVersion{
		Version:      debInfo.Version,
		Architecture: debInfo.Architecture,
		Filename:     objectKey,
		Checksums: model.Checksums{
			MD5:    debInfo.MD5,
			SHA1:   debInfo.SHA1,
			SHA256: debInfo.SHA256,
			Size:   size,
		},
		ControlFields: debInfo.ControlFields,
		UploadedAt:    time.Now(),
		UploadedBy:    identity,
	}

	// Add to index
	if err := rp.IndexMgr.AddPackageVersion(suite, component, debInfo.Package, ver); err != nil {
		http.Error(w, "Failed to register package", http.StatusInternalServerError)
		return
	}

	// Regenerate Release and Packages files for this suite
	dist, ok := rp.IndexMgr.GetDistribution(suite)
	if ok {
		s.renderAndStoreDistribution(suite, dist, rp)
	}

	// Persist updated snapshot to MinIO (async by default) - only if minioClient is configured
	if rp.MinioClient != nil {
		go func() {
			store := rp.SnapshotStore
			snap := rp.IndexMgr.GetIndex().ToSnapshot(version.String())
			// Background context: the request context is canceled when the
			// handler returns, which would abort the persist.
			if _, err := store.PutSnapshot(context.Background(), snap); err != nil {
				slog.Warn("failed to persist snapshot after upload", "error", err)
			}
		}()

		// Write checksum sidecar (fire-and-forget, use background context)
		go func() {
			stat, err := rp.MinioClient.StatObject(context.Background(), objectKey)
			if err != nil {
				slog.Warn("failed to stat object for sidecar", "object", objectKey, "error", err)
				return
			}

			sidecar := &model.ChecksumSidecarV1{
				FormatVersion: 1,
				Package:       debInfo.Package,
				Version:       debInfo.Version,
				Architecture:  debInfo.Architecture,
				ControlFields: debInfo.ControlFields,
				MD5:           debInfo.MD5,
				SHA1:          debInfo.SHA1,
				SHA256:        debInfo.SHA256,
				Size:          size,
				ETag:          stat.ETag,
				WrittenAt:     time.Now().Format(time.RFC3339),
			}

			store := minio.NewChecksumSidecarStore(rp.MinioClient)
			if err := store.PutChecksumSidecar(context.Background(), objectKey, sidecar); err != nil {
				slog.Warn("failed to write checksum sidecar", "object", objectKey, "error", err)
			}
		}()
	}

	// Return registered metadata
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "registered",
		"package":      debInfo.Package,
		"version":      debInfo.Version,
		"architecture": debInfo.Architecture,
		"filename":     objectKey,
		"checksums": map[string]interface{}{
			"md5":    debInfo.MD5,
			"sha1":   debInfo.SHA1,
			"sha256": debInfo.SHA256,
			"size":   size,
		},
	})
}

// handlePresign returns a presigned URL for direct MinIO upload (presigned mode only)
func (s *Server) handlePresign(w http.ResponseWriter, r *http.Request) {
	if s.config.UploadMode != "presigned" {
		http.Error(w, "Presigned upload disabled", http.StatusMethodNotAllowed)
		return
	}

	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Parse request
	var req struct {
		Suite     string `json:"suite"`
		Component string `json:"component"`
		Filename  string `json:"filename"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Suite == "" {
		req.Suite = rp.DefaultSuite
	}
	if req.Component == "" {
		req.Component = rp.DefaultComponent
	}

	if !isValidFilename(req.Filename) {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	// Validate suite/component
	if !isValidPathComponent(req.Suite) || !isValidPathComponent(req.Component) {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Check authorization
	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}
	if s.denyIfNotAuthorized(w, identity, acl.OpUpload, req.Suite, req.Component, r) {
		return
	}

	// Generate staging key under the configured staging prefix
	stagingPrefix := s.config.Storage.StagingPrefix
	if stagingPrefix == "" {
		stagingPrefix = "_staging/"
	}
	stagingKey := fmt.Sprintf("%s%s-%d", stagingPrefix, req.Filename, time.Now().UnixNano())

	// Generate presigned URL from MinIO
	if rp.MinioClient == nil {
		http.Error(w, "Storage not configured", http.StatusInternalServerError)
		return
	}

	presignedInfo, err := rp.MinioClient.GeneratePresignedUploadURL(r.Context(), stagingKey, s.config.PresignExpiry)
	if err != nil {
		http.Error(w, "Failed to generate upload URL", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(presignedInfo)
}

// handleRegister registers a presigned upload
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.config.UploadMode != "presigned" {
		http.Error(w, "Presigned upload disabled", http.StatusMethodNotAllowed)
		return
	}

	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Parse request
	var req struct {
		Suite      string `json:"suite"`
		Component  string `json:"component"`
		StagingKey string `json:"staging_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Suite == "" {
		req.Suite = rp.DefaultSuite
	}
	if req.Component == "" {
		req.Component = rp.DefaultComponent
	}

	if !isValidStagingKey(req.StagingKey, s.config.Storage.StagingPrefix) {
		http.Error(w, "Invalid staging_key", http.StatusBadRequest)
		return
	}

	// Validate suite/component
	if !isValidPathComponent(req.Suite) || !isValidPathComponent(req.Component) {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}

	// Check authorization
	if s.denyIfNotAuthorized(w, identity, acl.OpUpload, req.Suite, req.Component, r) {
		return
	}

	// Fetch staged object from MinIO
	if rp.MinioClient == nil {
		http.Error(w, "Storage not configured", http.StatusInternalServerError)
		return
	}

	stagedStat, err := rp.MinioClient.StatObject(r.Context(), req.StagingKey)
	if err != nil {
		http.Error(w, "Staged file not found", http.StatusNotFound)
		return
	}

	stagedFile, err := rp.MinioClient.FetchStagedFile(r.Context(), req.StagingKey)
	if err != nil {
		http.Error(w, "Staged file not found", http.StatusNotFound)
		return
	}
	defer func() {
		_ = stagedFile.Close()
	}()

	// Validate .deb while streaming.
	// NOTE: validate.Deb reports Size 0; the authoritative size is the
	// staged object's size from StatObject above.
	debInfo, err := validate.Deb(stagedFile)
	if err != nil {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Validate extracted metadata
	if !isValidPathComponent(debInfo.Package) || !isValidPathComponent(debInfo.Version) || !isValidPathComponent(debInfo.Architecture) {
		http.Error(w, "Invalid package format", http.StatusBadRequest)
		return
	}

	// Object key for final pool location
	objectKey := fmt.Sprintf("pool/%s/%s_%s_%s.deb", req.Component, debInfo.Package, debInfo.Version, debInfo.Architecture)

	// Move from staging to pool (atomic from client perspective)
	err = rp.MinioClient.MoveObject(r.Context(), req.StagingKey, objectKey)
	if err != nil {
		http.Error(w, "Failed to register package", http.StatusInternalServerError)
		return
	}

	// Create PackageVersion
	ver := &model.PackageVersion{
		Version:      debInfo.Version,
		Architecture: debInfo.Architecture,
		Filename:     objectKey,
		Checksums: model.Checksums{
			MD5:    debInfo.MD5,
			SHA1:   debInfo.SHA1,
			SHA256: debInfo.SHA256,
			Size:   stagedStat.Size,
		},
		ControlFields: debInfo.ControlFields,
		UploadedAt:    time.Now(),
		UploadedBy:    identity,
	}

	// Add to index
	if err := rp.IndexMgr.AddPackageVersion(req.Suite, req.Component, debInfo.Package, ver); err != nil {
		http.Error(w, "Failed to register package", http.StatusInternalServerError)
		return
	}

	// Regenerate Release and Packages files
	dist, ok := rp.IndexMgr.GetDistribution(req.Suite)
	if ok {
		s.renderAndStoreDistribution(req.Suite, dist, rp)
	}

	// Persist updated snapshot to MinIO (async by default)
	if rp.MinioClient != nil {
		go func() {
			store := rp.SnapshotStore
			snap := rp.IndexMgr.GetIndex().ToSnapshot(version.String())
			// Background context: the request context is canceled when the
			// handler returns, which would abort the persist.
			if _, err := store.PutSnapshot(context.Background(), snap); err != nil {
				slog.Warn("failed to persist snapshot after register", "error", err)
			}
		}()

		// Write checksum sidecar (fire-and-forget, use background context)
		go func() {
			stat, err := rp.MinioClient.StatObject(context.Background(), objectKey)
			if err != nil {
				slog.Warn("failed to stat object for sidecar", "object", objectKey, "error", err)
				return
			}

			sidecar := &model.ChecksumSidecarV1{
				FormatVersion: 1,
				Package:       debInfo.Package,
				Version:       debInfo.Version,
				Architecture:  debInfo.Architecture,
				ControlFields: debInfo.ControlFields,
				MD5:           debInfo.MD5,
				SHA1:          debInfo.SHA1,
				SHA256:        debInfo.SHA256,
				Size:          stat.Size,
				ETag:          stat.ETag,
				WrittenAt:     time.Now().Format(time.RFC3339),
			}

			store := minio.NewChecksumSidecarStore(rp.MinioClient)
			if err := store.PutChecksumSidecar(context.Background(), objectKey, sidecar); err != nil {
				slog.Warn("failed to write checksum sidecar", "object", objectKey, "error", err)
			}
		}()
	}

	// Return registered metadata
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "registered",
		"package":      debInfo.Package,
		"version":      debInfo.Version,
		"architecture": debInfo.Architecture,
		"filename":     objectKey,
		"checksums": map[string]interface{}{
			"md5":    debInfo.MD5,
			"sha1":   debInfo.SHA1,
			"sha256": debInfo.SHA256,
			"size":   stagedStat.Size,
		},
	})
}

// handleListDists returns available distributions/components/architectures
func (s *Server) handleListDists(w http.ResponseWriter, r *http.Request) {
	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}

	// Check authorization (suite-less operation)
	if s.denyIfNotAuthorized(w, identity, acl.OpListDists, "", "", r) {
		return
	}

	response := map[string]interface{}{
		"distributions": rp.IndexMgr.ListDistributions(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleRemovePackage removes a package version from a distribution
func (s *Server) handleRemovePackage(w http.ResponseWriter, r *http.Request) {
	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Parse path: /api/v1/dists/{suite}/{component}/remove/{package}[/{version}[/{arch}]]
	// (repo prefix already stripped for prefix-routed repos)
	path := strings.TrimPrefix(stripRepoPrefix(r.URL.Path, rp), "/api/v1/")
	parts := strings.Split(path, "/")

	if len(parts) < 5 || parts[0] != "dists" || parts[3] != "remove" {
		http.Error(w, "Invalid path format, expected /api/v1/dists/{suite}/{component}/remove/{package}[/{version}[/{arch}]]", http.StatusBadRequest)
		return
	}

	// Validate every user-controlled path segment
	for _, seg := range parts[1:] {
		if !isValidPathComponent(seg) {
			http.Error(w, "Invalid path segment", http.StatusBadRequest)
			return
		}
	}

	suite := parts[1]
	if suite == "" {
		suite = rp.DefaultSuite
	}

	component := parts[2]
	if component == "" {
		component = rp.DefaultComponent
	}

	pkgName := parts[4]

	// Check authorization
	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}
	if s.denyIfNotAuthorized(w, identity, acl.OpRemove, suite, component, r) {
		return
	}

	// Optional: specify version and architecture
	ver := ""
	arch := ""
	if len(parts) >= 6 {
		ver = parts[5]
	}
	if len(parts) >= 7 {
		arch = parts[6]
	}

	// Check if protected suite would be fully drained. Protection is an
	// anti-drain guard only: uploads into a protected suite are legitimate
	// day-to-day operations and are not restricted here.
	if isProtectedSuite(rp, suite) {
		if dist, ok := rp.IndexMgr.GetDistribution(suite); ok {
			if allMatches, _ := collectMoveMatches(dist, "", nil); len(allMatches) <= 1 {
				if r.URL.Query().Get("force") != "true" {
					http.Error(w, "suite '"+suite+"' is protected and cannot be fully emptied; retry with ?force=true", http.StatusConflict)
					return
				}
				// Draining a protected suite requires the distinct
				// unprotect grant, not just remove.
				if s.denyIfNotAuthorized(w, identity, acl.OpUnprotect, suite, component, r) {
					return
				}
			}
		}
	}

	// Call index manager to remove
	if err := rp.IndexMgr.RemovePackageVersion(suite, component, pkgName, ver, arch); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-render the distribution
	dist, ok := rp.IndexMgr.GetDistribution(suite)
	if ok {
		s.renderAndStoreDistribution(suite, dist, rp)
	}

	// Persist updated snapshot to MinIO (async by default)
	if rp.MinioClient != nil {
		go func() {
			store := rp.SnapshotStore
			snap := rp.IndexMgr.GetIndex().ToSnapshot("api-remove")
			// Background context: the request context is canceled when the
			// handler returns, which would abort the persist.
			if _, err := store.PutSnapshot(context.Background(), snap); err != nil {
				slog.Warn("failed to persist snapshot after remove", "error", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "removed",
		"package": pkgName,
		"suite":   suite,
	})
}

// moveRequest is the JSON body for POST /api/v1/admin/move.
type moveRequest struct {
	FromSuite string   `json:"from_suite"`
	ToSuite   string   `json:"to_suite"`
	Component string   `json:"component"` // optional, empty = all components
	Packages  []string `json:"packages"`  // optional glob list, empty = all packages
	DryRun    bool     `json:"dry_run"`
	Force     bool     `json:"force"` // optional, bypass protected suite check
}

// movedPackage describes one moved package version in the move response.
type movedPackage struct {
	Package      string `json:"package"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Component    string `json:"component"`
	Filename     string `json:"filename"`
}

// moveCandidate is one package version selected for a suite move.
type moveCandidate struct {
	component string
	pkgName   string
	ver       *model.PackageVersion
}

// isProtectedSuite reports whether suite is rp's protected default suite.
func isProtectedSuite(rp *repo.Repo, suite string) bool {
	return rp.ProtectDefaultSuite && suite == rp.DefaultSuite
}

// collectMoveMatches returns the package versions in dist matching the
// component filter and package globs (empty means all).
func collectMoveMatches(dist *model.Distribution, component string, globs []string) ([]moveCandidate, error) {
	for _, g := range globs {
		if _, err := path.Match(g, ""); err != nil {
			return nil, fmt.Errorf("invalid package pattern %q: %w", g, err)
		}
	}

	matchPkg := func(name string) bool {
		if len(globs) == 0 {
			return true
		}
		for _, g := range globs {
			if ok, _ := path.Match(g, name); ok {
				return true
			}
		}
		return false
	}

	var matches []moveCandidate
	dist.Mu.RLock()
	defer dist.Mu.RUnlock()
	for compName, comp := range dist.Components {
		if component != "" && compName != component {
			continue
		}
		for pkgName, pkg := range comp.Packages {
			if !matchPkg(pkgName) {
				continue
			}
			for _, ver := range pkg.Versions {
				matches = append(matches, moveCandidate{component: compName, pkgName: pkgName, ver: ver})
			}
		}
	}
	return matches, nil
}

// handleMovePackages moves package versions between suites within the index.
// Pool object paths contain no suite segment, so this is an index-only
// operation: entries are added to the destination suite (creating it if
// needed) and removed from the source suite, then both suites are
// re-rendered. Set dry_run to preview the move list without mutating.
func (s *Server) handleMovePackages(w http.ResponseWriter, r *http.Request) {
	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.FromSuite == "" || req.ToSuite == "" {
		http.Error(w, "from_suite and to_suite are required", http.StatusBadRequest)
		return
	}
	if req.FromSuite == req.ToSuite {
		http.Error(w, "from_suite and to_suite must differ", http.StatusBadRequest)
		return
	}
	if !isValidPathComponent(req.FromSuite) || !isValidPathComponent(req.ToSuite) {
		http.Error(w, "Invalid suite name", http.StatusBadRequest)
		return
	}
	if req.Component != "" && !isValidPathComponent(req.Component) {
		http.Error(w, "Invalid component name", http.StatusBadRequest)
		return
	}

	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}

	dist, ok := rp.IndexMgr.GetDistribution(req.FromSuite)
	if !ok {
		http.Error(w, "Source distribution not found", http.StatusNotFound)
		return
	}

	matches, err := collectMoveMatches(dist, req.Component, req.Packages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	toMoved := func() []movedPackage {
		moved := make([]movedPackage, 0, len(matches))
		for _, m := range matches {
			moved = append(moved, movedPackage{
				Package:      m.pkgName,
				Version:      m.ver.Version,
				Architecture: m.ver.Architecture,
				Component:    m.component,
				Filename:     m.ver.Filename,
			})
		}
		return moved
	}

	if len(matches) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "moved",
			"from_suite": req.FromSuite,
			"to_suite":   req.ToSuite,
			"moved":      []movedPackage{},
			"count":      0,
		})
		return
	}

	// Components affected by this move (computed before auth so the
	// protected-suite check can authorize per component below).
	components := make(map[string]bool)
	for _, m := range matches {
		components[m.component] = true
	}

	// Check if protected suite would be fully drained
	if isProtectedSuite(rp, req.FromSuite) {
		if allMatches, _ := collectMoveMatches(dist, "", nil); len(matches) >= len(allMatches) {
			if !req.Force {
				http.Error(w, "suite '"+req.FromSuite+"' is protected and cannot be fully emptied; retry with force: true", http.StatusConflict)
				return
			}
			// Draining a protected suite requires the distinct unprotect
			// grant on every affected component, not just remove.
			for comp := range components {
				if s.denyIfNotAuthorized(w, identity, acl.OpUnprotect, req.FromSuite, comp, r) {
					return
				}
			}
		}
	}

	// Authorize every affected component: remove on the source suite,
	// upload on the destination suite.
	for comp := range components {
		if s.denyIfNotAuthorized(w, identity, acl.OpRemove, req.FromSuite, comp, r) {
			return
		}
		if s.denyIfNotAuthorized(w, identity, acl.OpUpload, req.ToSuite, comp, r) {
			return
		}
	}

	if req.DryRun {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "dry_run",
			"from_suite": req.FromSuite,
			"to_suite":   req.ToSuite,
			"moved":      toMoved(),
			"count":      len(matches),
		})
		return
	}

	moved := make([]movedPackage, 0, len(matches))
	for _, m := range matches {
		if err := rp.IndexMgr.AddPackageVersion(req.ToSuite, m.component, m.pkgName, m.ver); err != nil {
			http.Error(w, fmt.Sprintf("Failed to add %s to %s: %v", m.pkgName, req.ToSuite, err), http.StatusInternalServerError)
			return
		}
		if err := rp.IndexMgr.RemovePackageVersion(req.FromSuite, m.component, m.pkgName, m.ver.Version, m.ver.Architecture); err != nil {
			http.Error(w, fmt.Sprintf("Failed to remove %s from %s: %v", m.pkgName, req.FromSuite, err), http.StatusInternalServerError)
			return
		}
		moved = append(moved, movedPackage{
			Package:      m.pkgName,
			Version:      m.ver.Version,
			Architecture: m.ver.Architecture,
			Component:    m.component,
			Filename:     m.ver.Filename,
		})
	}

	// Re-render both suites (destination was created by the first add).
	if dest, ok := rp.IndexMgr.GetDistribution(req.ToSuite); ok {
		s.renderAndStoreDistribution(req.ToSuite, dest, rp)
	}
	if src, ok := rp.IndexMgr.GetDistribution(req.FromSuite); ok {
		s.renderAndStoreDistribution(req.FromSuite, src, rp)
	}

	// Persist updated snapshot to MinIO (async by default)
	if rp.MinioClient != nil {
		go func() {
			store := rp.SnapshotStore
			snap := rp.IndexMgr.GetIndex().ToSnapshot("api-move")
			if _, err := store.PutSnapshot(context.Background(), snap); err != nil {
				slog.Warn("failed to persist snapshot after move", "error", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "moved",
		"from_suite": req.FromSuite,
		"to_suite":   req.ToSuite,
		"moved":      moved,
		"count":      len(moved),
	})
}

// handleReconcile starts an async index reconciliation job from pool contents
func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Check authorization (suite-less operation)
	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}
	if s.denyIfNotAuthorized(w, identity, acl.OpReconcile, "", "", r) {
		return
	}

	// Try to start a new reconciliation job (single-flight control)
	jobID, started := rp.JobManager.TryStart()
	if !started {
		// A reconciliation job is already running
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "conflict",
			"message":  "A reconciliation job is already running",
			"job_id":   jobID,
			"poll_url": fmt.Sprintf("/api/v1/admin/reconcile/%s", jobID),
		})
		return
	}

	// Start reconciliation in background
	go s.runReconcileJob(jobID, rp)

	// Return 202 Accepted immediately
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "running",
		"job_id":   jobID,
		"poll_url": fmt.Sprintf("/api/v1/admin/reconcile/%s", jobID),
	})
}

// runReconcileJob performs the actual reconciliation work in the background
func (s *Server) runReconcileJob(jobID string, rp *repo.Repo) {
	ctx := context.Background()
	reconciler := reconcile.NewReconciler(rp.MinioClient, rp.IndexMgr, rp.DefaultSuite)

	// Progress callback to update job state
	onProgress := func(processed, total int) {
		rp.JobManager.UpdateProgress(jobID, processed, total)
	}

	// Perform reconciliation
	newIndex, discrepancies, err := reconciler.Reconcile(ctx, onProgress)
	if err != nil {
		rp.JobManager.Fail(jobID, err)
		slog.Error("reconciliation failed", "job_id", jobID, "error", err)
		return
	}

	// Apply the reconciled index
	rp.IndexMgr.Load(newIndex)

	// Render all distributions (Release/Packages) after reconciliation
	for _, suite := range rp.IndexMgr.ListDistributions() {
		dist, ok := rp.IndexMgr.GetDistribution(suite)
		if !ok {
			continue
		}
		s.renderAndStoreDistribution(suite, dist, rp)
	}

	// Persist snapshot to MinIO (sync) so it survives service restart
	if rp.MinioClient != nil {
		store := rp.SnapshotStore
		snap := rp.IndexMgr.GetIndex().ToSnapshot(version.String())
		if _, err := store.PutSnapshot(ctx, snap); err != nil {
			slog.Warn("failed to persist reconciled snapshot", "error", err)
		}
	}

	// Mark job as complete
	rp.JobManager.Complete(jobID, discrepancies, int64(rp.IndexMgr.GetSnapshotGen()))
	slog.Info("reconciliation complete", "job_id", jobID, "discrepancies", len(discrepancies))
}

// handleReconcileStatus returns the status of a reconciliation job
func (s *Server) handleReconcileStatus(w http.ResponseWriter, r *http.Request) {
	// Get the repo from request context
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	// Check authorization (suite-less operation)
	identity := r.Header.Get("X-CI-Identity")
	if identity == "" {
		identity = "unknown"
	}
	if s.denyIfNotAuthorized(w, identity, acl.OpReconcile, "", "", r) {
		return
	}

	jobID := r.PathValue("jobID")
	job, ok := rp.JobManager.Get(jobID)
	if !ok {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Calculate progress percentage
	var percent float64
	if job.Total > 0 {
		percent = float64(job.Processed) / float64(job.Total) * 100
	}

	// Build response
	resp := map[string]interface{}{
		"job_id":     job.ID,
		"status":     string(job.Status),
		"started_at": job.StartedAt,
		"processed":  job.Processed,
		"total":      job.Total,
		"percent":    fmt.Sprintf("%.1f", percent),
	}

	if job.CompletedAt != nil {
		resp["completed_at"] = job.CompletedAt
	}

	switch job.Status {
	case reconcile.JobStatusComplete:
		resp["discrepancies"] = len(job.Discrepancies)
		resp["snapshot_gen"] = job.SnapshotGen
		resp["issues"] = job.Discrepancies
	case reconcile.JobStatusFailed:
		resp["error"] = job.Error
	}

	_ = json.NewEncoder(w).Encode(resp)
}
