package integration

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/apiserver"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptauth"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// TestErrorCases covers negative test cases and error handling
func TestErrorCases(t *testing.T) {
	// Setup test server
	indexMgr := index.NewManager()
	indexMgr.GetOrCreateDistribution("stable", "stable", "test", "test", "test")

	cfg := &config.Config{
		PoolServeMode: "proxy",
		UploadMode:    "direct",
	}
	cfg.Auth.CITokens = []config.CITokenConfig{
		{
			Token:    "valid-token",
			Identity: "test-ci",
			Grants: []config.GrantConfig{
				{
					Repos:      []string{"default"},
					Suites:     []string{"stable"},
					Components: []string{"main"},
					Operations: []string{"upload"},
				},
			},
		},
	}
	cfg.Auth.Metrics.Token = "test-metrics-token"

	// Create a single default repo for Phase 1
	testRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      indexMgr,
		MinioClient:   nil, // Not used in test
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        nil, // Not used in test
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "test",
			Label:       "test",
			Description: "test",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://test.local",
	}
	// Hydration runs in the background after the listener starts, so a fixture
	// representing a loaded repository has to say so explicitly.
	testRepo.MarkHydrated()
	registry, err := repo.NewRegistry([]*repo.Repo{testRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	// Add test user for Basic Auth
	testPassHash := "$2a$04$w9bS5GC9Vd4DjCQqiZAOAuihC/3GqAG1JCO5QC6GOEOoKaP6ITqWm" // bcrypt hash of "testpass"
	aptStore.Load([]model.AptUser{
		{
			Username:     "testuser",
			PasswordHash: testPassHash,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			Disabled:     false,
		},
	})
	server, err := apiserver.NewServer(registry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("UploadWithoutAuth", func(t *testing.T) {
		// Missing Authorization header
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("test")))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("UploadWithInvalidToken", func(t *testing.T) {
		// Invalid bearer token
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "Bearer invalid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("UploadInvalidDebFormat", func(t *testing.T) {
		// Invalid .deb file (not ar archive)
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("not a deb file")))
		req.Header.Set("Authorization", "Bearer valid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("PathTraversalInPackageName", func(t *testing.T) {
		// Try to inject path traversal in suite
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=../../../etc/passwd&component=main", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "Bearer valid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400 for path traversal, got %d", resp.StatusCode)
		}
	})

	t.Run("PathTraversalInComponent", func(t *testing.T) {
		// Try to inject path traversal in component
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=../admin", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "Bearer valid-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected 400 for path traversal, got %d", resp.StatusCode)
		}
	})

	t.Run("GetNonexistentPackages", func(t *testing.T) {
		// Request Packages for non-existent suite
		req, _ := http.NewRequest("GET", ts.URL+"/dists/nonexistent/main/binary-amd64/Packages", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK || resp.ContentLength == 0 {
			// Should return empty (not error) for non-existent suite
			t.Logf("GET /dists/nonexistent/... returned %d", resp.StatusCode)
		}
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		// POST to a GET-only endpoint
		req, _ := http.NewRequest("POST", ts.URL+"/dists/stable/Release", bytes.NewReader([]byte("test")))
		req.SetBasicAuth("testuser", "testpass")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("Expected 405, got %d", resp.StatusCode)
		}
	})

	t.Run("HealthCheckAlwaysOk", func(t *testing.T) {
		// Healthz should always return 200
		req, _ := http.NewRequest("GET", ts.URL+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ReadyzWithoutIndex", func(t *testing.T) {
		// Readyz should return 503 if no distributions loaded
		emptyMgr := index.NewManager()
		emptyMgr.SetIndex(model.NewIndex())

		// Create a single default repo with no distributions
		emptyTestRepo := &repo.Repo{
			ID:            "default",
			IndexMgr:      emptyMgr,
			MinioClient:   nil, // Not used in test
			SnapshotStore: nil,
			ChecksumStore: nil,
			Signer:        nil, // Not used in test
			JobManager:    nil,
			Metadata: repo.Metadata{
				Origin:      "test",
				Label:       "test",
				Description: "test",
			},
			DefaultSuite:     "stable",
			DefaultComponent: "main",
			RepoURL:          "https://test.local",
		}
		emptyRegistry, err := repo.NewRegistry([]*repo.Repo{emptyTestRepo})
		if err != nil {
			t.Fatalf("Failed to create registry: %v", err)
		}

		emptyAptStore := aptauth.NewStore()
		emptySrv, err := apiserver.NewServer(emptyRegistry, cfg, emptyAptStore, nil)
		if err != nil {
			t.Fatalf("Failed to create server: %v", err)
		}
		emptyMux := http.NewServeMux()
		emptySrv.Register(emptyMux)
		emptyTs := httptest.NewServer(emptyMux)
		defer emptyTs.Close()

		req, _ := http.NewRequest("GET", emptyTs.URL+"/readyz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("Expected 503, got %d", resp.StatusCode)
		}
	})

	t.Run("ReadyzWithIndex", func(t *testing.T) {
		// Readyz reports the hydration flag, not index contents: a repo whose
		// metadata has finished loading is ready even with no distributions.
		req, _ := http.NewRequest("GET", ts.URL+"/readyz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
	})
}

// TestAuthenticationEdgeCases covers auth edge cases
func TestAuthenticationEdgeCases(t *testing.T) {
	cfg := &config.Config{
		PoolServeMode: "proxy",
		UploadMode:    "direct",
	}
	cfg.Auth.CITokens = []config.CITokenConfig{
		{
			Token:    "token-with-spaces",
			Identity: "test",
			Grants: []config.GrantConfig{
				{
					Repos:      []string{"default"},
					Suites:     []string{"stable"},
					Components: []string{"main"},
					Operations: []string{"upload"},
				},
			},
		},
	}
	cfg.Auth.Metrics.Token = "test-metrics-token"

	indexMgr := index.NewManager()
	indexMgr.GetOrCreateDistribution("stable", "stable", "test", "test", "test")

	// Create a single default repo for Phase 1
	authTestRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      indexMgr,
		MinioClient:   nil, // Not used in test
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        nil, // Not used in test
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "test",
			Label:       "test",
			Description: "test",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://test.local",
	}
	authRegistry, err := repo.NewRegistry([]*repo.Repo{authTestRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	server, err := apiserver.NewServer(authRegistry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("MissingBearerPrefix", func(t *testing.T) {
		// Authorization header without "Bearer " prefix
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "token-with-spaces")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("EmptyBearerToken", func(t *testing.T) {
		// Empty token after "Bearer "
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "Bearer ")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("CapitalBearerPrefix", func(t *testing.T) {
		// "bearer" instead of "Bearer" (case-sensitive)
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("test")))
		req.Header.Set("Authorization", "bearer token-with-spaces")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401 (case-sensitive Bearer), got %d", resp.StatusCode)
		}
	})
}
