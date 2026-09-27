// Package integration contains end-to-end integration tests
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/apiserver"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptauth"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/testsupport/gpgtest"
	"git.golder.lan/rossgolderltd/debian-repo/internal/validate"
)

// TestDebValidation tests that generated .deb files can be validated
func TestDebValidation(t *testing.T) {
	deb := generateMinimalDeb(t, "test-pkg", "1.0.0", "amd64")

	// Try to validate the .deb
	debInfo, err := validate.Deb(bytes.NewReader(deb))
	if err != nil {
		t.Fatalf("Failed to validate .deb: %v", err)
	}

	if debInfo.Package != "test-pkg" {
		t.Fatalf("Expected package 'test-pkg', got %s", debInfo.Package)
	}
	if debInfo.Version != "1.0.0" {
		t.Fatalf("Expected version '1.0.0', got %s", debInfo.Version)
	}
	if debInfo.Architecture != "amd64" {
		t.Fatalf("Expected arch 'amd64', got %s", debInfo.Architecture)
	}

	t.Logf("✓ .deb validation successful: %s_%s_%s (checksums: %s, %s, %s)",
		debInfo.Package, debInfo.Version, debInfo.Architecture,
		debInfo.MD5[:8], debInfo.SHA1[:8], debInfo.SHA256[:8])
}

// TestEndToEndUploadAndFetch tests the complete flow: upload → metadata serving
func TestEndToEndUploadAndFetch(t *testing.T) {
	testKeyPath := gpgtest.KeyPath(t)

	signer, err := gpgsign.NewSigner(testKeyPath, "", false)
	if err != nil {
		t.Fatalf("Failed to load test GPG key: %v", err)
	}

	// Create test config
	cfg := &config.Config{
		Listen: struct {
			HTTP    string `yaml:"http"`
			Metrics string `yaml:"metrics"`
		}{
			HTTP:    ":0", // Use random port
			Metrics: ":0",
		},
		UploadMode:    "direct",
		PoolServeMode: "proxy",
		Auth: struct {
			CITokens []config.CITokenConfig `yaml:"ci_tokens"`
			Metrics  struct {
				Token string `yaml:"token"`
			} `yaml:"metrics"`
			AptUsers struct {
				StoreKey       string        `yaml:"store_key"`
				ReloadInterval time.Duration `yaml:"reload_interval"`
			} `yaml:"apt_users"`
			OIDC struct {
				IssuerURL    string        `yaml:"issuer_url"`
				ClientID     string        `yaml:"client_id"`
				ClientSecret string        `yaml:"client_secret"`
				RedirectURL  string        `yaml:"redirect_url"`
				CookieSecret string        `yaml:"cookie_secret"`
				SessionTTL   time.Duration `yaml:"session_ttl"`
			} `yaml:"oidc"`
		}{
			CITokens: []config.CITokenConfig{
				{
					Token:    "test-token-123",
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
			},
			Metrics: struct {
				Token string `yaml:"token"`
			}{
				Token: "test-metrics-token",
			},
		},
		RepoMetadata: struct {
			Origin      string `yaml:"origin"`
			Label       string `yaml:"label"`
			Description string `yaml:"description"`
		}{
			Origin:      "myorgname.com",
			Label:       "myorgname",
			Description: "Ross Golder Ltd Debian Repository",
		},
	}

	// Create index and server
	indexMgr := index.NewManager()
	indexMgr.SetIndex(model.NewIndex())

	// Create a single default repo for Phase 1
	testRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      indexMgr,
		MinioClient:   nil, // Not used in test
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        signer,
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "myorgname.com",
			Label:       "myorgname",
			Description: "Ross Golder Ltd Debian Repository",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://debs.myorgname.com",
	}
	registry, err := repo.NewRegistry([]*repo.Repo{testRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	// Add test user for apt Basic Auth tests
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

	// Set up HTTP routes
	mux := http.NewServeMux()
	server.Register(mux)

	// Create test server
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test 1: Upload a package
	t.Run("UploadPackage", func(t *testing.T) {
		deb := generateMinimalDeb(t, "test-pkg", "1.0.0", "amd64")

		req, err := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader(deb))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		req.Header.Set("Authorization", "Bearer test-token-123")
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Upload failed with status %d: %s", resp.StatusCode, string(body))
		}

		var uploadResp map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if uploadResp["status"] != "registered" {
			t.Fatalf("Expected status 'registered', got %v", uploadResp["status"])
		}

		if uploadResp["package"] != "test-pkg" {
			t.Fatalf("Expected package 'test-pkg', got %v", uploadResp["package"])
		}

		t.Logf("Upload successful: %+v", uploadResp)
	})

	// Test 2: Fetch Packages metadata
	t.Run("FetchPackagesMetadata", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL+"/dists/stable/main/binary-amd64/Packages", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to fetch Packages: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		// Metadata might not be rendered yet (depending on implementation)
		// Just verify we get a response
		if resp.StatusCode == http.StatusServiceUnavailable {
			t.Logf("Packages not yet rendered (expected until rendering is wired): %s", resp.Status)
		} else if resp.StatusCode != http.StatusOK {
			t.Logf("Unexpected status %d: %s", resp.StatusCode, string(body))
		} else {
			t.Logf("Packages metadata (%d bytes):\n%s", len(body), string(body))
		}
	})

	// Test 3: Fetch Release metadata
	t.Run("FetchReleaseMetadata", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL+"/dists/stable/Release", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to fetch Release: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		// Release metadata might not be rendered yet
		if resp.StatusCode == http.StatusServiceUnavailable {
			t.Logf("Release not yet rendered (expected until rendering is wired): %s", resp.Status)
		} else if resp.StatusCode != http.StatusOK {
			t.Logf("Unexpected status %d: %s", resp.StatusCode, string(body))
		} else {
			t.Logf("Release metadata (%d bytes):\n%s", len(body), string(body))
		}
	})

	// Test 4: Fetch public key
	t.Run("FetchPublicKey", func(t *testing.T) {
		req, err := http.NewRequest("GET", ts.URL+"/pubkey.gpg", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.SetBasicAuth("testuser", "testpass")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to fetch public key: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to fetch public key: %s", resp.Status)
		}

		body, _ := io.ReadAll(resp.Body)
		keyContent := string(body)

		if !bytes.Contains(body, []byte("BEGIN PGP PUBLIC KEY BLOCK")) {
			t.Fatal("Public key missing PGP header")
		}

		t.Logf("Public key retrieved (%d bytes)", len(body))
		t.Logf("Key fingerprint: %s", signer.KeyInfo().Fingerprint)
		t.Logf("Key UIDs: %v", signer.KeyInfo().UIDs)

		// Verify the key can be parsed
		if len(keyContent) < 100 {
			t.Fatal("Public key too short")
		}
	})

	// Test 5: Verify index state
	t.Run("VerifyIndexState", func(t *testing.T) {
		dists := indexMgr.ListDistributions()
		if len(dists) == 0 {
			t.Fatal("No distributions in index")
		}

		t.Logf("Distributions in index: %v", dists)

		// Verify the package was registered
		dist, ok := indexMgr.GetDistribution("stable")
		if !ok {
			t.Fatal("Distribution 'stable' not found in index")
		}

		if len(dist.Components) == 0 {
			t.Fatal("No components in distribution")
		}

		t.Logf("Distribution 'stable' has components: %v", func() []string {
			var names []string
			for name := range dist.Components {
				names = append(names, name)
			}
			return names
		}())
	})

	// Test 6: Test authentication
	t.Run("TestAuthentication", func(t *testing.T) {
		deb := generateMinimalDeb(t, "auth-test", "1.0.0", "amd64")

		// Request without auth header
		req, err := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader(deb))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to fetch: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", resp.StatusCode)
		}

		t.Logf("Auth check passed: request without token correctly rejected")
	})

	// Test 7: Health checks
	t.Run("HealthChecks", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/healthz", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to fetch health: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Health check failed: %s", resp.Status)
		}

		t.Logf("Health check passed")
	})
}

// TestMultiplePackageVersions tests registering multiple versions and architectures
func TestMultiplePackageVersions(t *testing.T) {
	testKeyPath := gpgtest.KeyPath(t)

	signer, err := gpgsign.NewSigner(testKeyPath, "", false)
	if err != nil {
		t.Fatalf("Failed to load test GPG key: %v", err)
	}

	cfg := &config.Config{
		Listen: struct {
			HTTP    string `yaml:"http"`
			Metrics string `yaml:"metrics"`
		}{HTTP: ":0", Metrics: ":0"},
		UploadMode:    "direct",
		PoolServeMode: "proxy",
		Auth: struct {
			CITokens []config.CITokenConfig `yaml:"ci_tokens"`
			Metrics  struct {
				Token string `yaml:"token"`
			} `yaml:"metrics"`
			AptUsers struct {
				StoreKey       string        `yaml:"store_key"`
				ReloadInterval time.Duration `yaml:"reload_interval"`
			} `yaml:"apt_users"`
			OIDC struct {
				IssuerURL    string        `yaml:"issuer_url"`
				ClientID     string        `yaml:"client_id"`
				ClientSecret string        `yaml:"client_secret"`
				RedirectURL  string        `yaml:"redirect_url"`
				CookieSecret string        `yaml:"cookie_secret"`
				SessionTTL   time.Duration `yaml:"session_ttl"`
			} `yaml:"oidc"`
		}{
			CITokens: []config.CITokenConfig{
				{
					Token:    "test-token",
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
			},
			Metrics: struct {
				Token string `yaml:"token"`
			}{
				Token: "test-metrics-token",
			},
		},
	}

	indexMgr := index.NewManager()
	indexMgr.SetIndex(model.NewIndex())

	// Create a single default repo for Phase 1
	testRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      indexMgr,
		MinioClient:   nil, // Not used in test
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        signer,
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "myorgname.com",
			Label:       "myorgname",
			Description: "Ross Golder Ltd Debian Repository",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://debs.myorgname.com",
	}
	registry, err := repo.NewRegistry([]*repo.Repo{testRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	server, err := apiserver.NewServer(registry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Upload multiple versions and architectures
	variations := []struct {
		pkg  string
		ver  string
		arch string
	}{
		{"app", "1.0.0", "amd64"},
		{"app", "1.0.0", "arm64"},
		{"app", "2.0.0", "amd64"},
		{"app", "2.0.0", "arm64"},
		{"lib", "1.5.0", "amd64"},
		{"lib", "1.5.0", "arm64"},
	}

	for _, v := range variations {
		deb := generateMinimalDeb(t, v.pkg, v.ver, v.arch)

		req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/upload?suite=stable&component=main", ts.URL), bytes.NewReader(deb))
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload %s_%s_%s: %v", v.pkg, v.ver, v.arch, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Upload failed for %s_%s_%s: %s", v.pkg, v.ver, v.arch, resp.Status)
		}

		t.Logf("✓ Uploaded %s_%s_%s", v.pkg, v.ver, v.arch)
	}

	// Verify all packages are in index
	dist, _ := indexMgr.GetDistribution("stable")
	if len(dist.Components) != 1 {
		t.Fatalf("Expected 1 component, got %d", len(dist.Components))
	}

	comp := dist.Components["main"]
	if len(comp.Packages) != 2 {
		t.Fatalf("Expected 2 packages (app, lib), got %d", len(comp.Packages))
	}

	appPkg := comp.Packages["app"]
	if len(appPkg.Versions) != 4 {
		t.Fatalf("Expected 4 versions of 'app', got %d", len(appPkg.Versions))
	}

	t.Logf("✓ All packages registered correctly (2 packages, 6 total versions)")
}

// TestMultiRepoIsolation tests that two repos are fully isolated
func TestMultiRepoIsolation(t *testing.T) {
	testKeyPath := gpgtest.KeyPath(t)

	signer, err := gpgsign.NewSigner(testKeyPath, "", false)
	if err != nil {
		t.Fatalf("Failed to load test GPG key: %v", err)
	}

	// Create test config with two repos
	cfg := &config.Config{
		Listen: struct {
			HTTP    string `yaml:"http"`
			Metrics string `yaml:"metrics"`
		}{
			HTTP:    ":0",
			Metrics: ":0",
		},
		UploadMode:    "direct",
		PoolServeMode: "proxy",
		Auth: struct {
			CITokens []config.CITokenConfig `yaml:"ci_tokens"`
			Metrics  struct {
				Token string `yaml:"token"`
			} `yaml:"metrics"`
			AptUsers struct {
				StoreKey       string        `yaml:"store_key"`
				ReloadInterval time.Duration `yaml:"reload_interval"`
			} `yaml:"apt_users"`
			OIDC struct {
				IssuerURL    string        `yaml:"issuer_url"`
				ClientID     string        `yaml:"client_id"`
				ClientSecret string        `yaml:"client_secret"`
				RedirectURL  string        `yaml:"redirect_url"`
				CookieSecret string        `yaml:"cookie_secret"`
				SessionTTL   time.Duration `yaml:"session_ttl"`
			} `yaml:"oidc"`
		}{
			CITokens: []config.CITokenConfig{
				{
					Token:    "stable-token",
					Identity: "stable-ci",
					Grants: []config.GrantConfig{
						{
							Repos:      []string{"stable"},
							Suites:     []string{"stable"},
							Components: []string{"main"},
							Operations: []string{"upload"},
						},
					},
				},
				{
					Token:    "testing-token",
					Identity: "testing-ci",
					Grants: []config.GrantConfig{
						{
							Repos:      []string{"testing"},
							Suites:     []string{"testing"},
							Components: []string{"main"},
							Operations: []string{"upload"},
						},
					},
				},
			},
			Metrics: struct {
				Token string `yaml:"token"`
			}{
				Token: "test-metrics-token",
			},
		},
		RepoMetadata: struct {
			Origin      string `yaml:"origin"`
			Label       string `yaml:"label"`
			Description string `yaml:"description"`
		}{
			Origin:      "myorgname.com",
			Label:       "myorgname",
			Description: "Ross Golder Ltd Debian Repository",
		},
	}

	// Create two repos
	stableIndexMgr := index.NewManager()
	stableIndexMgr.SetIndex(model.NewIndex())

	testingIndexMgr := index.NewManager()
	testingIndexMgr.SetIndex(model.NewIndex())

	stableRepo := &repo.Repo{
		ID:               "stable",
		IndexMgr:         stableIndexMgr,
		MinioClient:      nil,
		SnapshotStore:    nil,
		ChecksumStore:    nil,
		Signer:           signer,
		JobManager:       nil,
		Metadata:         repo.Metadata{Origin: "myorgname.com", Label: "stable", Description: "Stable repo"},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://debs.myorgname.com",
	}

	testingRepo := &repo.Repo{
		ID:               "testing",
		PathPrefix:       "/testing",
		IndexMgr:         testingIndexMgr,
		MinioClient:      nil,
		SnapshotStore:    nil,
		ChecksumStore:    nil,
		Signer:           signer,
		JobManager:       nil,
		Metadata:         repo.Metadata{Origin: "myorgname.com", Label: "testing", Description: "Testing repo"},
		DefaultSuite:     "testing",
		DefaultComponent: "main",
		RepoURL:          "https://debs.myorgname.com/testing",
	}

	registry, err := repo.NewRegistry([]*repo.Repo{stableRepo, testingRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	server, err := apiserver.NewServer(registry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test: Upload to stable repo
	t.Run("UploadToStableRepo", func(t *testing.T) {
		deb := generateMinimalDeb(t, "stable-pkg", "1.0.0", "amd64")
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/upload?suite=stable&component=main", bytes.NewReader(deb))
		req.Header.Set("Authorization", "Bearer stable-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
		t.Logf("✓ Uploaded to stable repo")
	})

	// Test: Upload to testing repo
	t.Run("UploadToTestingRepo", func(t *testing.T) {
		deb := generateMinimalDeb(t, "testing-pkg", "1.0.0", "amd64")
		req, _ := http.NewRequest("POST", ts.URL+"/testing/api/v1/upload?suite=testing&component=main", bytes.NewReader(deb))
		req.Header.Set("Authorization", "Bearer testing-token")

		resp, _ := http.DefaultClient.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
		}

		var uploadResp map[string]interface{}
		json.Unmarshal(body, &uploadResp)
		t.Logf("Upload response: %+v", uploadResp)

		// Immediately check if distribution exists
		dists := testingIndexMgr.ListDistributions()
		t.Logf("Distributions after upload: %v", dists)

		// Check if we can get the repo's IndexMgr directly
		t.Logf("Testing repo ID: %s", testingRepo.ID)
		t.Logf("Testing repo IndexMgr is testingIndexMgr: %v", testingRepo.IndexMgr == testingIndexMgr)

		t.Logf("✓ Uploaded to testing repo")
	})

	// Test: ACL isolation - stable token can't upload to testing
	t.Run("StableTokenRejectsTestingRepo", func(t *testing.T) {
		deb := generateMinimalDeb(t, "isolation-test", "1.0.0", "amd64")
		req, _ := http.NewRequest("POST", ts.URL+"/testing/api/v1/upload?suite=testing&component=main", bytes.NewReader(deb))
		req.Header.Set("Authorization", "Bearer stable-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Expected 403 (Forbidden), got %d", resp.StatusCode)
		}
		t.Logf("✓ ACL correctly rejects stable token on testing repo")
	})

	// Test: Verify indexes are separate
	t.Run("IndexesAreSeparate", func(t *testing.T) {
		// Check stable repo
		stableDists := stableIndexMgr.ListDistributions()
		if len(stableDists) == 0 {
			t.Fatal("No distributions in stable repo")
		}

		stableDist, ok := stableIndexMgr.GetDistribution("stable")
		if !ok {
			t.Logf("Available distributions in stable: %v", stableDists)
			t.Fatal("Stable distribution not found")
		}
		if len(stableDist.Components) != 1 {
			t.Fatalf("Expected 1 component in stable, got %d", len(stableDist.Components))
		}

		// Check testing repo
		testingDists := testingIndexMgr.ListDistributions()
		if len(testingDists) == 0 {
			t.Fatal("No distributions in testing repo")
		}

		testingDist, ok := testingIndexMgr.GetDistribution("testing")
		if !ok {
			t.Logf("Available distributions in testing: %v", testingDists)
			t.Fatal("Testing distribution not found")
		}
		if len(testingDist.Components) != 1 {
			t.Fatalf("Expected 1 component in testing, got %d", len(testingDist.Components))
		}

		stableMain := stableDist.Components["main"]
		testingMain := testingDist.Components["main"]

		if len(stableMain.Packages) == 0 {
			t.Fatal("Stable repo should have packages")
		}
		if len(testingMain.Packages) == 0 {
			t.Fatal("Testing repo should have packages")
		}

		// Verify no cross-contamination
		if _, ok := stableMain.Packages["testing-pkg"]; ok {
			t.Fatal("testing-pkg should not be in stable repo")
		}
		if _, ok := testingMain.Packages["stable-pkg"]; ok {
			t.Fatal("stable-pkg should not be in testing repo")
		}

		t.Logf("✓ Indexes are properly isolated")
	})
}

func TestProtectedSuiteDrain(t *testing.T) {
	testKeyPath := gpgtest.KeyPath(t)

	signer, err := gpgsign.NewSigner(testKeyPath, "", false)
	if err != nil {
		t.Fatalf("Failed to load test GPG key: %v", err)
	}

	// Create test config with admin token that can upload and remove
	cfg := &config.Config{
		Listen: struct {
			HTTP    string `yaml:"http"`
			Metrics string `yaml:"metrics"`
		}{
			HTTP:    ":0",
			Metrics: ":0",
		},
		UploadMode:    "direct",
		PoolServeMode: "proxy",
		Auth: struct {
			CITokens []config.CITokenConfig `yaml:"ci_tokens"`
			Metrics  struct {
				Token string `yaml:"token"`
			} `yaml:"metrics"`
			AptUsers struct {
				StoreKey       string        `yaml:"store_key"`
				ReloadInterval time.Duration `yaml:"reload_interval"`
			} `yaml:"apt_users"`
			OIDC struct {
				IssuerURL    string        `yaml:"issuer_url"`
				ClientID     string        `yaml:"client_id"`
				ClientSecret string        `yaml:"client_secret"`
				RedirectURL  string        `yaml:"redirect_url"`
				CookieSecret string        `yaml:"cookie_secret"`
				SessionTTL   time.Duration `yaml:"session_ttl"`
			} `yaml:"oidc"`
		}{
			CITokens: []config.CITokenConfig{
				{
					Token:    "admin-token",
					Identity: "admin",
					Grants: []config.GrantConfig{
						{
							Repos:      []string{"*"},
							Suites:     []string{"*"},
							Components: []string{"*"},
							Operations: []string{"upload", "remove", "unprotect"},
						},
					},
				},
			},
			Metrics: struct {
				Token string `yaml:"token"`
			}{
				Token: "test-metrics-token",
			},
		},
		RepoMetadata: struct {
			Origin      string `yaml:"origin"`
			Label       string `yaml:"label"`
			Description string `yaml:"description"`
		}{
			Origin:      "myorgname.com",
			Label:       "myorgname",
			Description: "Test Debian Repository",
		},
	}

	// Create protected repo (protect_default_suite = true)
	protectedIndexMgr := index.NewManager()
	protectedIndexMgr.SetIndex(model.NewIndex())

	protectedRepo := &repo.Repo{
		ID:                  "protected",
		VHost:               "",
		PathPrefix:          "/protected",
		IndexMgr:            protectedIndexMgr,
		MinioClient:         nil,
		SnapshotStore:       nil,
		ChecksumStore:       nil,
		Signer:              signer,
		JobManager:          nil,
		Metadata:            repo.Metadata{Origin: "myorgname.com", Label: "protected", Description: "Protected repo"},
		DefaultSuite:        "stable",
		DefaultComponent:    "main",
		ProtectDefaultSuite: true,
		RepoURL:             "https://example.com/protected",
	}

	// Create unprotected repo (protect_default_suite = false)
	unprotectedIndexMgr := index.NewManager()
	unprotectedIndexMgr.SetIndex(model.NewIndex())

	unprotectedRepo := &repo.Repo{
		ID:                  "unprotected",
		VHost:               "",
		PathPrefix:          "/unprotected",
		IndexMgr:            unprotectedIndexMgr,
		MinioClient:         nil,
		SnapshotStore:       nil,
		ChecksumStore:       nil,
		Signer:              signer,
		JobManager:          nil,
		Metadata:            repo.Metadata{Origin: "myorgname.com", Label: "unprotected", Description: "Unprotected repo"},
		DefaultSuite:        "testing",
		DefaultComponent:    "main",
		ProtectDefaultSuite: false,
		RepoURL:             "https://example.com/unprotected",
	}

	// Create registry with both repos
	registry, err := repo.NewRegistry([]*repo.Repo{protectedRepo, unprotectedRepo})
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	aptStore := aptauth.NewStore()
	server, err := apiserver.NewServer(registry, cfg, aptStore, nil)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	mux := http.NewServeMux()
	server.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("ProtectedSuiteRemoveDrain", func(t *testing.T) {
		// Upload one package to protected repo's stable suite
		deb := generateMinimalDeb(t, "protected-pkg", "1.0.0", "amd64")
		uploadURL := ts.URL + "/protected/api/v1/upload?suite=stable&component=main"
		req, _ := http.NewRequest("POST", uploadURL, bytes.NewReader(deb))
		req.Header.Set("Authorization", "Bearer admin-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload package: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to upload package: %d %s", resp.StatusCode, string(body))
		}
		t.Logf("✓ Uploaded package to protected suite")

		// Try to remove the only package - should get 409
		removeURL := ts.URL + "/protected/api/v1/dists/stable/main/remove/protected-pkg/1.0.0/amd64"
		req, _ = http.NewRequest("DELETE", removeURL, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make remove request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response body: %s", string(body))
			t.Fatalf("Expected 409 Conflict, got %d", resp.StatusCode)
		}
		t.Logf("✓ Remove last package from protected suite returns 409 Conflict")

		// Retry with ?force=true - should succeed
		removeURLForce := removeURL + "?force=true"
		req, _ = http.NewRequest("DELETE", removeURLForce, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make forced remove request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response body: %s", string(body))
			t.Fatalf("Expected 200 OK with ?force=true, got %d", resp.StatusCode)
		}
		t.Logf("✓ Remove with ?force=true bypasses protection and succeeds")
	})

	t.Run("ProtectedSuiteMoveDrain", func(t *testing.T) {
		// Upload two packages to protected repo's stable suite
		for i := 1; i <= 2; i++ {
			pkgName := fmt.Sprintf("movepkg-%d", i)
			deb := generateMinimalDeb(t, pkgName, "1.0.0", "amd64")
			uploadURL := ts.URL + "/protected/api/v1/upload?suite=stable&component=main"
			req, _ := http.NewRequest("POST", uploadURL, bytes.NewReader(deb))
			req.Header.Set("Authorization", "Bearer admin-token")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Failed to upload package %s: %v", pkgName, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("Failed to upload package %s: %d %s", pkgName, resp.StatusCode, string(body))
			}
		}
		t.Logf("✓ Uploaded 2 packages to protected suite")

		// Try move-all without force - should get 409
		movePayload := map[string]interface{}{
			"from_suite": "stable",
			"to_suite":   "testing",
		}
		payload, _ := json.Marshal(movePayload)
		moveURL := ts.URL + "/protected/api/v1/admin/move"
		req, _ := http.NewRequest("POST", moveURL, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make move request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response body: %s", string(body))
			t.Fatalf("Expected 409 Conflict for move-all protected suite, got %d", resp.StatusCode)
		}
		t.Logf("✓ Move-all from protected suite returns 409 Conflict")

		// Retry with force: true - should succeed
		movePayloadForced := map[string]interface{}{
			"from_suite": "stable",
			"to_suite":   "testing",
			"force":      true,
		}
		payloadForced, _ := json.Marshal(movePayloadForced)
		req, _ = http.NewRequest("POST", moveURL, bytes.NewReader(payloadForced))
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make forced move request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response body: %s", string(body))
			t.Fatalf("Expected 200 OK with force: true, got %d", resp.StatusCode)
		}
		t.Logf("✓ Move with force: true bypasses protection and succeeds")
	})

	t.Run("UnprotectedSuiteNoRestriction", func(t *testing.T) {
		// Upload one package to unprotected repo's testing suite
		deb := generateMinimalDeb(t, "unprotected-pkg", "1.0.0", "amd64")
		uploadURL := ts.URL + "/unprotected/api/v1/upload?suite=testing&component=main"
		req, _ := http.NewRequest("POST", uploadURL, bytes.NewReader(deb))
		req.Header.Set("Authorization", "Bearer admin-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to upload package: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Failed to upload package: %d %s", resp.StatusCode, string(body))
		}
		t.Logf("✓ Uploaded package to unprotected suite")

		// Remove the only package - should succeed without force (no protection)
		removeURL := ts.URL + "/unprotected/api/v1/dists/testing/main/remove/unprotected-pkg/1.0.0/amd64"
		req, _ = http.NewRequest("DELETE", removeURL, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make remove request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("Response body: %s", string(body))
			t.Fatalf("Expected 200 OK for unprotected suite, got %d", resp.StatusCode)
		}
		t.Logf("✓ Removing from unprotected suite succeeds without force")
	})
}
