// Package apiserver handles HTTP endpoint serving for apt and CI clients
package apiserver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
	"git.golder.lan/rossgolderltd/debian-repo/internal/version"
)

// handleDists serves distribution metadata (Packages, Release, InRelease)
func (s *Server) handleDists(w http.ResponseWriter, r *http.Request) {
	// Parse path: /dists/{suite}/{component}/binary-{arch}/Packages(.gz)
	// or /dists/{suite}/Release, /dists/{suite}/InRelease, /dists/{suite}/Release.gpg

	rp := s.currentRepo(r)
	if rp == nil {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(stripRepoPrefix(r.URL.Path, rp), "/dists/")
	parts := strings.Split(path, "/")

	if len(parts) < 1 {
		http.NotFound(w, r)
		return
	}

	suite := parts[0]

	// Validate suite exists
	if _, ok := rp.IndexMgr.GetDistribution(suite); !ok {
		http.NotFound(w, r)
		return
	}

	// Handle Release files
	if len(parts) == 2 {
		switch parts[1] {
		case "Release":
			// Serve cached Release (always in sync with cached Packages)
			rendered := rp.IndexMgr.GetRenderedDist(suite)
			if rendered == nil {
				http.Error(w, "Release not yet rendered", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(rendered.ReleasePlain)
			return

		case "Release.gpg":
			// Return the pre-rendered detached signature
			rendered := rp.IndexMgr.GetRenderedDist(suite)
			if rendered == nil {
				http.Error(w, "Release not yet rendered", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/pgp-signature; charset=utf-8")
			_, _ = w.Write(rendered.ReleaseGPG)
			return

		case "InRelease":
			// Return the pre-rendered clearsigned file
			rendered := rp.IndexMgr.GetRenderedDist(suite)
			if rendered == nil {
				http.Error(w, "InRelease not yet rendered", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(rendered.InRelease)
			return
		}
	}

	// Handle Packages files: /dists/{suite}/{component}/binary-{arch}/Packages(.gz)
	if len(parts) >= 4 && strings.HasPrefix(parts[2], "binary-") {
		arch := strings.TrimPrefix(parts[2], "binary-")

		// Serve cached Packages (always in sync with Release)
		rendered := rp.IndexMgr.GetRenderedDist(suite)
		if rendered == nil {
			http.Error(w, "Packages not yet rendered", http.StatusServiceUnavailable)
			return
		}

		if len(parts) >= 4 && (parts[3] == "Packages" || parts[3] == "Packages.gz") {
			if parts[3] == "Packages.gz" {
				// Packages.gz
				w.Header().Set("Content-Type", "application/gzip")
				w.Header().Set("Content-Encoding", "gzip")
				if data, ok := rendered.PackagesGzByArch[arch]; ok {
					_, _ = w.Write(data)
				} else {
					http.NotFound(w, r)
				}
			} else {
				// Uncompressed Packages
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				if data, ok := rendered.PackagesByArch[arch]; ok {
					_, _ = w.Write(data)
				} else {
					http.NotFound(w, r)
				}
			}
			return
		}
	}

	http.NotFound(w, r)
}

// handlePoolProxy serves .deb files from MinIO (proxy mode only)
func (s *Server) handlePoolProxy(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.NotFound(w, r)
		return
	}

	if s.config.PoolServeMode != "proxy" {
		http.Error(w, "Pool serving disabled", http.StatusMethodNotAllowed)
		return
	}

	// Parse path: /pool/{component}/{filename} (repo prefix already stripped)
	path := "pool/" + strings.TrimPrefix(stripRepoPrefix(r.URL.Path, rp), "/pool/")

	// Stat the object first to verify it exists and get size
	info, err := rp.MinioClient.StatObject(r.Context(), path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Stream .deb file from MinIO
	obj, err := rp.MinioClient.GetObject(r.Context(), path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() {
		_ = obj.Close()
	}()

	// Set response headers for caching and verification
	w.Header().Set("Content-Type", "application/x-deb")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size))
	w.Header().Set("Cache-Control", "public, immutable, max-age=31536000")
	if info.ETag != "" {
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, info.ETag))
	}

	// Verify checksum in background (don't block response)
	slog.Info("checksum: starting background verification", "path", path)
	go s.verifyFileChecksum(context.Background(), path, rp)

	// Stream the object
	n, err := io.Copy(w, obj)
	if err != nil {
		slog.Warn("pool proxy: error streaming object", "key", path, "bytes_copied", n, "error", err)
	}
}

// verifyFileChecksum checks if the actual file checksum matches what's in Packages.
// Logs warnings if mismatches are detected (indicates stale metadata).
func (s *Server) verifyFileChecksum(ctx context.Context, poolPath string, rp *repo.Repo) {
	// Extract filename from path: pool/component/filename.deb
	parts := strings.Split(poolPath, "/")
	if len(parts) < 3 {
		return
	}
	filename := parts[len(parts)-1]

	// Try to find this file's expected checksum in the current index
	currentIndex := rp.IndexMgr.GetIndex()
	var expectedSHA256 string

	// Search all distributions/components for this filename
	currentIndex.Mu.RLock()
	for _, dist := range currentIndex.Distributions {
		dist.Mu.RLock()
		for _, component := range dist.Components {
			for _, pkg := range component.Packages {
				for _, ver := range pkg.Versions {
					if strings.HasSuffix(ver.Filename, filename) {
						expectedSHA256 = ver.Checksums.SHA256
						break
					}
				}
				if expectedSHA256 != "" {
					break
				}
			}
			if expectedSHA256 != "" {
				break
			}
		}
		dist.Mu.RUnlock()
		if expectedSHA256 != "" {
			break
		}
	}
	currentIndex.Mu.RUnlock()

	if expectedSHA256 == "" {
		// File not in index (orphan or not yet indexed)
		slog.Info("checksum: file not in index", "filename", filename)
		return
	}

	slog.Info("checksum: verifying file", "filename", filename, "expected", expectedSHA256[:16])

	// Calculate actual checksum from file in MinIO
	obj, err := rp.MinioClient.GetObject(ctx, poolPath)
	if err != nil {
		slog.Warn("checksum: failed to get object", "path", poolPath, "error", err)
		return
	}
	defer func() {
		_ = obj.Close()
	}()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, obj); err != nil {
		slog.Warn("checksum: failed to read object", "path", poolPath, "error", err)
		return
	}

	actualSHA256 := fmt.Sprintf("%x", hasher.Sum(nil))

	// Check for mismatch
	if actualSHA256 != expectedSHA256 {
		slog.Warn("checksum mismatch: package metadata is stale",
			"path", poolPath,
			"expected_sha256", expectedSHA256,
			"actual_sha256", actualSHA256,
			"action", "triggering reconciliation")

		// Trigger reconciliation to rebuild Packages from current pool state
		jobID, started := rp.JobManager.TryStart()
		if started {
			go s.runReconcileJob(jobID, rp)
			slog.Info("reconciliation auto-triggered by checksum mismatch", "job_id", jobID)
		}
	} else {
		slog.Debug("checksum verified", "path", poolPath, "sha256", actualSHA256)
	}
}

// handleInstall serves installation instructions
func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	keyInfo := rp.Signer.KeyInfo()

	// Optional ?suite= parameter selects the suite shown in the apt
	// sources examples (defaults to "stable").
	suite := r.URL.Query().Get("suite")
	if suite == "" {
		suite = "stable"
	}
	if !isValidPathComponent(suite) {
		http.Error(w, "Invalid suite", http.StatusBadRequest)
		return
	}

	data := &templates.InstallPageData{
		PageData:       s.basePageData(r),
		RepositoryURL:  rp.RepoURL,
		KeyID:          keyInfo.KeyID,
		KeyFingerprint: keyInfo.Fingerprint,
		Suite:          suite,
	}
	data.Title = "Installation Instructions"
	data.CurrentPage = "install"
	data.PageHeader = "Repository GPG Key"
	data.NavItems = map[string]string{
		"index":   "/",
		"install": "/install.html",
		"gpg":     "/gpg-key.html",
	}

	html, err := s.templateLoader.RenderInstall(data)
	if err != nil {
		slog.Error("failed to render install page", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, html)
}

// handleGPGKeyPage serves GPG key information page
func (s *Server) handleGPGKeyPage(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	keyInfo := rp.Signer.KeyInfo()

	data := &templates.GPGKeyPageData{
		PageData:       s.basePageData(r),
		KeyID:          keyInfo.KeyID,
		KeyFingerprint: keyInfo.Fingerprint,
		KeyUIDs:        keyInfo.UIDs,
		PublicKeyBlock: keyInfo.ArmoredPublicKey,
	}
	data.Title = "GPG Key Information"
	data.CurrentPage = "gpg"
	data.PageHeader = "Repository GPG Key"
	data.NavItems = map[string]string{
		"index":   "/",
		"install": "/install.html",
		"gpg":     "/gpg-key.html",
	}

	html, err := s.templateLoader.RenderGPGKey(data)
	if err != nil {
		slog.Error("failed to render GPG key page", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, html)
}

// handlePublicKey serves the armored public key
func (s *Server) handlePublicKey(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pgp-keys")
	_, _ = fmt.Fprint(w, rp.Signer.KeyInfo().ArmoredPublicKey)
}

// handleStatic serves static CSS and JavaScript files
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract filename from path
	filename := strings.TrimPrefix(r.URL.Path, "/static/")
	if filename == "" {
		http.NotFound(w, r)
		return
	}

	// Determine content type
	var contentType string
	switch {
	case strings.HasSuffix(filename, ".css"):
		contentType = "text/css; charset=utf-8"
	case strings.HasSuffix(filename, ".js"):
		contentType = "application/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}

	// Read embedded file
	data, err := templates.GetStaticFile(filename)
	if err != nil {
		slog.Debug("static file not found", "file", filename)
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleIndexCards serves the card-based repository index page
// Optional ?suite= parameter filters cards to a single suite; without it
// all suites are shown and each card lists the suites holding it.
func (s *Server) handleIndexCards(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.Error(w, "Repository not found", http.StatusInternalServerError)
		return
	}

	suiteFilter := r.URL.Query().Get("suite")
	if suiteFilter != "" && !isValidPathComponent(suiteFilter) {
		http.Error(w, "Invalid suite", http.StatusBadRequest)
		return
	}

	// Build card index data
	data := &templates.CardIndexPageData{
		PageData:    s.basePageData(r),
		ActiveSuite: suiteFilter,
	}
	data.Title = "Debian Repository"
	data.CurrentPage = "index"
	data.PageHeader = "📦 Debian Package Repository"
	data.VersionString = version.String()
	data.NavItems = map[string]string{
		"index":   "/",
		"install": "/install.html",
		"gpg":     "/gpg-key.html",
	}

	// cardSource tracks one package across suites (a package may live in
	// several suites; the card shows the union of that info).
	type cardSource struct {
		pkg    *model.Package
		suites map[string]bool
	}

	// Get last updated time and collect all packages
	lastUpdated := time.Time{}
	packageMap := make(map[string]*cardSource)
	suiteCounts := make(map[string]int)

	suites := rp.IndexMgr.ListDistributions()
	sort.Strings(suites)
	for _, suite := range suites {
		dist, _ := rp.IndexMgr.GetDistribution(suite)
		if dist == nil {
			continue
		}

		dist.Mu.RLock()
		for _, comp := range dist.Components {
			for pkgName, pkg := range comp.Packages {
				// Track latest uploaded time
				for _, ver := range pkg.Versions {
					if ver.UploadedAt.After(lastUpdated) {
						lastUpdated = ver.UploadedAt
					}
				}
				// Store package (will use latest version info)
				src, exists := packageMap[pkgName]
				if !exists {
					src = &cardSource{pkg: pkg, suites: make(map[string]bool)}
					packageMap[pkgName] = src
				}
				src.suites[suite] = true
				suiteCounts[suite]++
			}
		}
		dist.Mu.RUnlock()
	}

	// Suite filter tabs (All + one per suite with package counts)
	data.Suites = make([]templates.SuiteTab, 0, len(suites)+1)
	data.Suites = append(data.Suites, templates.SuiteTab{Name: "", Count: len(packageMap), Active: suiteFilter == ""})
	for _, suite := range suites {
		data.Suites = append(data.Suites, templates.SuiteTab{Name: suite, Count: suiteCounts[suite], Active: suiteFilter == suite})
	}

	// Convert to cards
	for pkgName, src := range packageMap {
		pkg := src.pkg
		// Apply suite filter
		if suiteFilter != "" && !src.suites[suiteFilter] {
			continue
		}
		// Get latest version info
		var latestVer *model.PackageVersion
		var latestTime time.Time
		archSet := make(map[string]bool)

		for _, ver := range pkg.Versions {
			if ver.UploadedAt.After(latestTime) {
				latestTime = ver.UploadedAt
				latestVer = ver
			}
			archSet[ver.Architecture] = true
		}

		// Count distinct versions
		versionSet := make(map[string]bool)
		for _, ver := range pkg.Versions {
			versionSet[ver.Version] = true
		}

		if latestVer == nil {
			continue
		}

		// Convert architecture set to sorted slice
		archs := make([]string, 0, len(archSet))
		for arch := range archSet {
			archs = append(archs, arch)
		}
		sort.Strings(archs)

		cardSuites := make([]string, 0, len(src.suites))
		for suite := range src.suites {
			cardSuites = append(cardSuites, suite)
		}
		sort.Strings(cardSuites)

		data.Packages = append(data.Packages, templates.PackageCard{
			Name:          pkgName,
			LatestVersion: latestVer.Version,
			Description:   latestVer.ControlFields["Description"],
			Section:       latestVer.ControlFields["Section"],
			Maintainer:    latestVer.ControlFields["Maintainer"],
			VersionCount:  len(versionSet),
			ArchCount:     len(archSet),
			Architectures: archs,
			Suites:        cardSuites,
		})
	}

	if lastUpdated.IsZero() {
		lastUpdated = time.Now().UTC()
	}

	data.TotalPackages = len(data.Packages)
	data.LastUpdated = lastUpdated

	// Sort packages alphabetically
	sort.Slice(data.Packages, func(i, j int) bool {
		return data.Packages[i].Name < data.Packages[j].Name
	})

	// Render
	html, err := s.templateLoader.RenderCardIndex(data)
	if err != nil {
		slog.Error("failed to render card index", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, html)
}

// handleProfile serves the user profile page
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if s.webAuth == nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	sess, ok := s.webAuth.CurrentSession(r)
	if !ok {
		http.Redirect(w, r, "/auth/login?return_to=/profile", http.StatusFound)
		return
	}

	data := &templates.UserProfilePageData{
		PageData: s.basePageData(r),
		Email:    sess.Email,
		Name:     sess.Email,
	}
	data.Title = "User Profile"
	data.CurrentPage = "profile"
	data.PageHeader = "👤 Profile"
	data.NavItems = map[string]string{
		"index":   "/",
		"install": "/install.html",
		"gpg":     "/gpg-key.html",
	}

	html, err := s.templateLoader.RenderProfile(data)
	if err != nil {
		slog.Error("failed to render profile", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, html)
}

// handleLogout clears the user session and redirects to home
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.webAuth != nil {
		s.webAuth.HandleLogout(w, r)
	} else {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

// handleFavicon serves favicon.svg as favicon.ico for legacy browser compatibility
func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	data, err := templates.GetStaticFile("favicon.svg")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

// handleRobots serves robots.txt for search engine crawlers
func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	data, err := templates.GetStaticFile("robots.txt")
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

// handlePackageDetail serves a package detail page
func (s *Server) handlePackageDetail(w http.ResponseWriter, r *http.Request) {
	rp := s.currentRepo(r)
	if rp == nil {
		http.NotFound(w, r)
		return
	}

	// Extract package name from path /packages/{name}
	path := strings.TrimPrefix(r.URL.Path, "/packages/")
	if path == r.URL.Path || path == "" {
		http.NotFound(w, r)
		return
	}

	pkgName := strings.TrimSuffix(path, "/")

	// Get index and search for package
	idx := rp.IndexMgr.GetIndex()
	if idx == nil || idx.Distributions == nil {
		http.NotFound(w, r)
		return
	}

	// Search for package in all distributions
	var pkgMetadata *model.Package
	versionSet := make(map[string]*model.PackageVersion)

	for _, dist := range idx.Distributions {
		for _, comp := range dist.Components {
			if pkg, exists := comp.Packages[pkgName]; exists {
				pkgMetadata = pkg
				for versionKey, pv := range pkg.Versions {
					if _, hasKey := versionSet[versionKey]; !hasKey {
						versionSet[versionKey] = pv
					}
				}
			}
		}
	}

	if pkgMetadata == nil || len(versionSet) == 0 {
		http.NotFound(w, r)
		return
	}

	// Build version list with distribution and checksum info
	versionDetails := make([]templates.PackageVersionDetail, 0)
	for _, pv := range pkgMetadata.Versions {
		// Extract checksums from control fields if available
		size := pv.Checksums.Size
		sha256 := pv.Checksums.SHA256
		sha1 := pv.Checksums.SHA1
		md5 := pv.Checksums.MD5

		// Find which distributions contain this version
		distNames := make([]string, 0)
		for _, dist := range idx.Distributions {
			for _, comp := range dist.Components {
				if pkg, exists := comp.Packages[pkgName]; exists {
					if _, hasVersion := pkg.Versions[fmt.Sprintf("%s_%s", pv.Version, pv.Architecture)]; hasVersion {
						distNames = append(distNames, dist.Suite)
						break
					}
				}
			}
		}

		versionDetails = append(versionDetails, templates.PackageVersionDetail{
			Version:       pv.Version,
			Architecture:  pv.Architecture,
			Size:          size,
			SHA256:        sha256,
			MD5:           md5,
			SHA1:          sha1,
			Distributions: distNames,
		})
	}

	// Sort versions by version descending
	sort.Slice(versionDetails, func(i, j int) bool {
		return versionDetails[i].Version > versionDetails[j].Version
	})

	// Extract metadata from control fields
	description := ""
	section := ""
	priority := ""
	maintainer := ""
	depends := ""
	if len(pkgMetadata.Versions) > 0 {
		for _, pv := range pkgMetadata.Versions {
			if pv.ControlFields != nil {
				if desc, ok := pv.ControlFields["Description"]; ok {
					description = desc
				}
				if sec, ok := pv.ControlFields["Section"]; ok {
					section = sec
				}
				if pri, ok := pv.ControlFields["Priority"]; ok {
					priority = pri
				}
				if maint, ok := pv.ControlFields["Maintainer"]; ok {
					maintainer = maint
				}
				if dep, ok := pv.ControlFields["Depends"]; ok {
					depends = dep
				}
			}
			break // Just use first version's metadata
		}
	}

	data := &templates.PackageDetailPageData{
		PageData:    s.basePageData(r),
		PackageName: pkgMetadata.Name,
		Description: description,
		Section:     section,
		Priority:    priority,
		Maintainer:  maintainer,
		Depends:     depends,
		Versions:    versionDetails,
	}

	data.Title = fmt.Sprintf("%s - Package Details", pkgName)
	data.CurrentPage = "index"
	data.PageHeader = fmt.Sprintf("📦 %s", pkgName)
	data.PageSubtitle = template.HTML(description)
	data.NavItems = map[string]string{
		"index":   "/",
		"install": "/install.html",
		"gpg":     "/gpg-key.html",
	}

	html, err := s.templateLoader.RenderPackageDetail(data)
	if err != nil {
		slog.Error("failed to render package detail", "package", pkgName, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(html))
}
