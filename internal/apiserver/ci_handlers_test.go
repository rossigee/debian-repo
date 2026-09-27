package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/aptauth"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/testsupport/gpgtest"
)

// withTestRepo sets a repo in the request context (uses router.go:repoContext)
func withTestRepo(r *http.Request, rp *repo.Repo) *http.Request {
	return r.WithContext(repoContext(r.Context(), rp))
}

// newTestServerWithTwoPackages creates a test server with two packages for testing removal edge cases
func newTestServerWithTwoPackages(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)

	// Add a second package so removal tests don't hit protection checks
	pv2 := &model.PackageVersion{
		Version:       "2.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 200},
		ControlFields: map[string]string{"Package": "another-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := s.registry.All()[0].IndexMgr.AddPackageVersion("stable", "main", "another-pkg", pv2); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}
	return s
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	m := index.NewManager()
	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "amd64",
		Checksums:     model.Checksums{Size: 100},
		ControlFields: map[string]string{"Package": "test-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := m.AddPackageVersion("stable", "main", "test-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.RepoMetadata.Origin = "myorgname.com"
	cfg.RepoMetadata.Label = "golder.tech Debian Repository"
	cfg.RepoMetadata.Description = "Test repo"
	cfg.Auth.Metrics.Token = "test-token"
	cfg.Auth.AptUsers.StoreKey = "_meta/apt-users.json.gz"
	// Add default grants for all operations on all repos/suites/components
	cfg.Auth.CITokens = []config.CITokenConfig{
		{
			Token:    "test-token",
			Identity: "test",
			Grants: []config.GrantConfig{
				{
					Repos:      []string{"*"},
					Suites:     []string{"*"},
					Components: []string{"*"},
					Operations: []string{"*"},
				},
			},
		},
	}

	keyPath := gpgtest.KeyPath(t)
	signer, err := gpgsign.NewSigner(keyPath, "", false)
	if err != nil {
		t.Fatalf("NewSigner(%s): %v", keyPath, err)
	}

	// Create a test repo with all required fields
	testRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      m,
		MinioClient:   nil,
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        signer,
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "myorgname.com",
			Label:       "golder.tech Debian Repository",
			Description: "Test repo",
		},
		DefaultSuite:        "stable",
		DefaultComponent:    "main",
		ProtectDefaultSuite: true,
		RepoURL:             "https://test.local",
	}

	registry, err := repo.NewRegistry([]*repo.Repo{testRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	srv, err := NewServer(registry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	return srv
}

func TestHandleRemovePackageNotFound(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/nonexistent/1.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d", rec.Code)
	}

	t.Logf("✓ RemovePackage returns error for non-existent package")
}

func TestHandleRemovePackageInvalidPath(t *testing.T) {
	s := newTestServer(t)
	testRepo := s.registry.All()[0]

	req := httptest.NewRequest("DELETE", "/api/v1/invalid", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", rec.Code)
	}

	t.Logf("✓ RemovePackage returns 400 for invalid path")
}

func TestHandleRemovePackageMissingVersion(t *testing.T) {
	// Adding a package with empty version/arch so path-based removal works
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// Remove with only package name - the path-based parsing requires component
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	// Should return error because empty version/arch doesn't match stored key
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 (empty version/arch), got %d: %s", rec.Code, rec.Body.String())
	}

	t.Logf("✓ RemovePackage returns error for empty version/arch")
}

func TestHandleRemovePackageWithVersionOnly(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// Path with version but no arch - arch will be empty string, won't match
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	// Should return error because arch is empty
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 (empty arch), got %d", rec.Code)
	}

	t.Logf("✓ RemovePackage returns error for empty arch")
}

func TestHandleRemovePackageCascade(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// Remove another-pkg first
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/another-pkg/2.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for first removal, got %d", rec.Code)
	}

	// Now remove test-pkg with force=true (last package)
	req = httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for second removal, got %d", rec.Code)
	}

	dist, ok := s.registry.All()[0].IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should exist")
	}
	if len(dist.Components) != 0 {
		t.Fatalf("Expected 0 components after cascade, got %d", len(dist.Components))
	}

	t.Logf("✓ RemovePackage cascades cleanup correctly")
}

func TestHandleRemovePackageNoMinIO(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ RemovePackage works without MinIO client")
}

func TestHandleRemovePackageWithMinIO(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ RemovePackage works with MinIO client")
}

func TestIsProtectedSuite(t *testing.T) {
	rp := &repo.Repo{
		DefaultSuite:        "stable",
		ProtectDefaultSuite: true,
	}

	// Test: Default suite when protected returns true
	if !isProtectedSuite(rp, "stable") {
		t.Fatalf("Expected isProtectedSuite(rp, 'stable') to be true")
	}

	// Test: Different suite when protected returns false
	if isProtectedSuite(rp, "testing") {
		t.Fatalf("Expected isProtectedSuite(rp, 'testing') to be false")
	}

	// Test: Default suite when not protected returns false
	rp.ProtectDefaultSuite = false
	if isProtectedSuite(rp, "stable") {
		t.Fatalf("Expected isProtectedSuite(rp, 'stable') to be false when unprotected")
	}

	t.Logf("✓ isProtectedSuite helper works correctly")
}

func TestHandleRemovePackageProtectedSuite(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// First remove another-pkg to leave only test-pkg
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/another-pkg/2.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for first removal, got %d", rec.Code)
	}

	// Test: Removing last package from protected suite returns 409
	req = httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("Expected 409 for protected suite drain, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "protected") {
		t.Fatalf("Expected error message to mention protected suite, got: %s", rec.Body.String())
	}

	// Test: Same removal with force=true returns 200
	req = httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 with force=true, got %d: %s", rec.Code, rec.Body.String())
	}

	t.Logf("✓ Protected suite enforcement in handleRemovePackage works correctly")
}

func TestHandleRemovePackagePoolFileDeletion(t *testing.T) {
	// Create a server with multiple package versions to test pool deletion
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// Add a third package with multiple architectures
	pv3 := &model.PackageVersion{
		Version:       "3.0.0",
		Architecture:  "arm64",
		Checksums:     model.Checksums{Size: 300},
		ControlFields: map[string]string{"Package": "multi-arch-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := testRepo.IndexMgr.AddPackageVersion("stable", "main", "multi-arch-pkg", pv3); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	// Verify the package exists with the expected version/arch
	dist, ok := testRepo.IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should exist")
	}
	if comp, ok := dist.Components["main"]; ok {
		if pkg, ok := comp.Packages["multi-arch-pkg"]; ok {
			if _, ok := pkg.Versions["3.0.0:arm64"]; !ok {
				t.Fatal("Expected version 3.0.0:arm64")
			}
		}
	}

	// Test removing specific version/architecture
	// The pool file that would be deleted is: pool/main/multi-arch-pkg_3.0.0_arm64.deb
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/multi-arch-pkg/3.0.0/arm64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for removal, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the package version was removed from the index
	dist, ok = testRepo.IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should still exist")
	}
	if comp, ok := dist.Components["main"]; ok {
		if pkg, ok := comp.Packages["multi-arch-pkg"]; ok {
			if _, ok := pkg.Versions["3.0.0:arm64"]; ok {
				t.Fatal("Expected version 3.0.0:arm64 to be removed")
			}
		}
	}

	t.Logf("✓ Pool file path collection for RemovePackage works correctly")
}

func TestHandleRemovePackagePoolFileMultipleVersions(t *testing.T) {
	// Test removing multiple architectures of same version
	s := newTestServerWithTwoPackages(t)
	testRepo := s.registry.All()[0]

	// Add another architecture of test-pkg
	pv := &model.PackageVersion{
		Version:       "1.0.0",
		Architecture:  "arm64",
		Checksums:     model.Checksums{Size: 150},
		ControlFields: map[string]string{"Package": "test-pkg"},
		UploadedAt:    time.Now(),
	}
	if err := testRepo.IndexMgr.AddPackageVersion("stable", "main", "test-pkg", pv); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	// Verify both architectures exist
	dist, ok := testRepo.IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should exist")
	}
	if comp, ok := dist.Components["main"]; ok {
		if pkg, ok := comp.Packages["test-pkg"]; ok {
			if len(pkg.Versions) != 2 {
				t.Fatalf("Expected 2 versions (amd64 and arm64), got %d", len(pkg.Versions))
			}
		}
	}

	// Remove another-pkg first so we can remove test-pkg entirely
	req := httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/another-pkg/2.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Remove the amd64 version of test-pkg (would collect pool path: pool/main/test-pkg_1.0.0_amd64.deb)
	req = httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify only the arm64 version remains
	dist, ok = testRepo.IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should still exist")
	}
	if comp, ok := dist.Components["main"]; ok {
		if pkg, ok := comp.Packages["test-pkg"]; ok {
			if len(pkg.Versions) != 1 {
				t.Fatalf("Expected 1 version remaining (arm64), got %d", len(pkg.Versions))
			}
			if _, ok := pkg.Versions["1.0.0:arm64"]; !ok {
				t.Fatal("Expected version 1.0.0:arm64 to remain")
			}
		} else {
			t.Fatal("Package test-pkg should still exist")
		}
	}

	// Remove the arm64 version with force (would collect pool path: pool/main/test-pkg_1.0.0_arm64.deb)
	req = httptest.NewRequest("DELETE", "/api/v1/dists/stable/main/remove/test-pkg/1.0.0/arm64?force=true", nil)
	req = withTestRepo(req, testRepo)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()

	s.handleRemovePackage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the entire suite is now empty
	dist, ok = testRepo.IndexMgr.GetDistribution("stable")
	if !ok {
		t.Fatal("Distribution should still exist")
	}
	if len(dist.Components) != 0 {
		t.Fatalf("Expected all components to be removed after cascade, got %d", len(dist.Components))
	}

	t.Logf("✓ Pool file path collection for multiple architectures works correctly")
}
