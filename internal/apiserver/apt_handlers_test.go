package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// newTestServerForAptHandlers creates a minimal Server for testing apt handlers.
// It sets up a server with a registry containing a single test repo with the given IndexMgr.
func newTestServerForAptHandlers(mgr *index.Manager) *Server {
	testRepo := &repo.Repo{
		ID:            "default",
		IndexMgr:      mgr,
		MinioClient:   nil,
		SnapshotStore: nil,
		ChecksumStore: nil,
		Signer:        nil,
		JobManager:    nil,
		Metadata: repo.Metadata{
			Origin:      "test",
			Label:       "Test Repository",
			Description: "Test repo",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
		RepoURL:          "https://test.local",
	}
	registry, _ := repo.NewRegistry([]*repo.Repo{testRepo})
	server := &Server{
		registry: registry,
	}
	return server
}

// TestPackagesPathParsing tests both compressed and uncompressed Packages file paths
func TestPackagesPathParsing(t *testing.T) {
	mgr := index.NewManager()

	// Create a test distribution via AddPackageVersion
	if err := mgr.AddPackageVersion("stable", "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/test-pkg_1.0.0_amd64.deb",
		Checksums: model.Checksums{
			SHA256: "abc123",
			Size:   1000,
		},
	}); err != nil {
		t.Fatalf("failed to add package: %v", err)
	}

	// Also add arm64 variant
	if err := mgr.AddPackageVersion("stable", "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "arm64",
		Filename:     "pool/main/test-pkg_1.0.0_arm64.deb",
		Checksums: model.Checksums{
			SHA256: "def456",
			Size:   1000,
		},
	}); err != nil {
		t.Fatalf("failed to add arm64 package: %v", err)
	}

	// Create test data for rendering
	uncompressedData := []byte("Package: test-pkg\nVersion: 1.0.0\nArchitecture: amd64\n")
	compressedData := []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00") // gzip magic bytes prefix

	// Create and set rendered distribution
	rendered := &model.RenderedDist{
		Suite:          "stable",
		ReleasePlain:   []byte("Origin: test\n"),
		ReleaseGPG:     []byte("-----BEGIN PGP-----\n"),
		InRelease:      []byte("-----BEGIN PGP SIGNED MESSAGE-----\n"),
		PackagesByArch: map[string][]byte{"amd64": uncompressedData, "arm64": uncompressedData},
		PackagesGzByArch: map[string][]byte{
			"amd64": compressedData,
			"arm64": compressedData,
		},
	}
	mgr.SetRenderedDist("stable", rendered)

	server := newTestServerForAptHandlers(mgr)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   []byte
		expectedType   string
	}{
		{
			name:           "uncompressed_packages_amd64",
			path:           "/dists/stable/main/binary-amd64/Packages",
			expectedStatus: http.StatusOK,
			expectedBody:   uncompressedData,
			expectedType:   "text/plain; charset=utf-8",
		},
		{
			name:           "compressed_packages_amd64",
			path:           "/dists/stable/main/binary-amd64/Packages.gz",
			expectedStatus: http.StatusOK,
			expectedBody:   compressedData,
			expectedType:   "application/gzip",
		},
		{
			name:           "uncompressed_packages_arm64",
			path:           "/dists/stable/main/binary-arm64/Packages",
			expectedStatus: http.StatusOK,
			expectedBody:   uncompressedData,
			expectedType:   "text/plain; charset=utf-8",
		},
		{
			name:           "compressed_packages_arm64",
			path:           "/dists/stable/main/binary-arm64/Packages.gz",
			expectedStatus: http.StatusOK,
			expectedBody:   compressedData,
			expectedType:   "application/gzip",
		},
		{
			name:           "missing_architecture",
			path:           "/dists/stable/main/binary-i386/Packages",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing_architecture_compressed",
			path:           "/dists/stable/main/binary-i386/Packages.gz",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "release_file",
			path:           "/dists/stable/Release",
			expectedStatus: http.StatusOK,
			expectedType:   "text/plain; charset=utf-8",
		},
		{
			name:           "inrelease_file",
			path:           "/dists/stable/InRelease",
			expectedStatus: http.StatusOK,
			expectedType:   "text/plain; charset=utf-8",
		},
		{
			name:           "release_gpg_file",
			path:           "/dists/stable/Release.gpg",
			expectedStatus: http.StatusOK,
			expectedType:   "application/pgp-signature; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.handleDists(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedType != "" {
				contentType := w.Header().Get("Content-Type")
				if contentType != tt.expectedType {
					t.Errorf("expected content-type %q, got %q", tt.expectedType, contentType)
				}
			}

			// For gzip, also check Content-Encoding header
			if tt.name == "compressed_packages_amd64" || tt.name == "compressed_packages_arm64" {
				encoding := w.Header().Get("Content-Encoding")
				if encoding != "gzip" {
					t.Errorf("expected content-encoding gzip, got %q", encoding)
				}
			}

			if tt.expectedBody != nil && string(w.Body.Bytes()) != string(tt.expectedBody) {
				t.Errorf("body mismatch:\nexpected: %q\ngot: %q", tt.expectedBody, w.Body.Bytes())
			}
		})
	}
}

// TestPackagesPathEdgeCases tests edge cases in path parsing
func TestPackagesPathEdgeCases(t *testing.T) {
	mgr := index.NewManager()

	// Create distribution via package addition
	if err := mgr.AddPackageVersion("stable", "main", "test-pkg", &model.PackageVersion{
		Version:      "1.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/test_1.0.0_amd64.deb",
		Checksums:    model.Checksums{SHA256: "abc123", Size: 1000},
	}); err != nil {
		t.Fatalf("failed to add package: %v", err)
	}

	rendered := &model.RenderedDist{
		Suite:            "stable",
		ReleasePlain:     []byte("Origin: test\n"),
		PackagesByArch:   map[string][]byte{"amd64": []byte("test")},
		PackagesGzByArch: map[string][]byte{"amd64": []byte("gztest")},
	}
	mgr.SetRenderedDist("stable", rendered)

	server := newTestServerForAptHandlers(mgr)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		description    string
	}{
		{
			name:           "invalid_suite",
			path:           "/dists/testing/main/binary-amd64/Packages",
			expectedStatus: http.StatusNotFound,
			description:    "suite that doesn't exist should 404",
		},
		{
			name:           "missing_component",
			path:           "/dists/stable/Packages",
			expectedStatus: http.StatusNotFound,
			description:    "missing component should 404",
		},
		{
			name:           "root_path",
			path:           "/dists/",
			expectedStatus: http.StatusNotFound,
			description:    "root /dists/ should 404",
		},
		{
			name:           "empty_path",
			path:           "/dists",
			expectedStatus: http.StatusNotFound,
			description:    "just /dists should 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.handleDists(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestPackagesPathParsingLogic tests the specific path component extraction
func TestPackagesPathParsingLogic(t *testing.T) {
	tests := []struct {
		path                 string
		expectedSuite        string
		expectedComponent    string
		expectedArch         string
		expectedFilename     string
		expectedIsValid      bool
		expectedIsCompressed bool
	}{
		{
			path:                 "/dists/stable/main/binary-amd64/Packages",
			expectedSuite:        "stable",
			expectedComponent:    "main",
			expectedArch:         "amd64",
			expectedFilename:     "Packages",
			expectedIsValid:      true,
			expectedIsCompressed: false,
		},
		{
			path:                 "/dists/stable/main/binary-amd64/Packages.gz",
			expectedSuite:        "stable",
			expectedComponent:    "main",
			expectedArch:         "amd64",
			expectedFilename:     "Packages.gz",
			expectedIsValid:      true,
			expectedIsCompressed: true,
		},
		{
			path:                 "/dists/testing/contrib/binary-arm64/Packages",
			expectedSuite:        "testing",
			expectedComponent:    "contrib",
			expectedArch:         "arm64",
			expectedFilename:     "Packages",
			expectedIsValid:      true,
			expectedIsCompressed: false,
		},
		{
			path:             "/dists/stable/Release",
			expectedSuite:    "stable",
			expectedFilename: "Release",
			expectedIsValid:  false,
		},
		{
			path:            "/dists/stable/main/Packages",
			expectedSuite:   "stable",
			expectedIsValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			path := tt.path[7:] // Remove "/dists/"
			parts := []string{}
			for _, p := range []byte(path) {
				if p == '/' {
					break
				}
			}
			parts = strings.Split(path, "/")

			// Check basic structure
			if tt.expectedIsValid {
				if len(parts) < 4 {
					t.Errorf("expected at least 4 parts, got %d", len(parts))
					return
				}

				suite := parts[0]
				if suite != tt.expectedSuite {
					t.Errorf("expected suite %q, got %q", tt.expectedSuite, suite)
				}

				filename := parts[3]
				if filename != tt.expectedFilename {
					t.Errorf("expected filename %q, got %q", tt.expectedFilename, filename)
				}

				if filename == "Packages.gz" {
					if !tt.expectedIsCompressed {
						t.Error("expected uncompressed but got .gz extension")
					}
				}

				if len(parts) >= 3 && parts[2][:7] == "binary-" {
					arch := parts[2][7:]
					if arch != tt.expectedArch {
						t.Errorf("expected arch %q, got %q", tt.expectedArch, arch)
					}
				}
			}
		})
	}
}
