package apiserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func uploadDeb(t *testing.T, s *Server, query, arch string) *httptest.ResponseRecorder {
	t.Helper()
	s.config.UploadMode = "direct"
	deb := buildTestDeb(t, "allow-pkg", "1.0.0", arch)
	req := httptest.NewRequest("POST", "/api/v1/upload?"+query, bytes.NewReader(deb))
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleUpload(rec, withTestRepo(req, s.registry.All()[0]))
	return rec
}

func expectStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, mustContain string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
	if mustContain != "" && !strings.Contains(rec.Body.String(), mustContain) {
		t.Errorf("body %q does not name %q", rec.Body.String(), mustContain)
	}
}

func TestAllowedDistributions(t *testing.T) {
	s := newTestServer(t)
	s.config.Validation.AllowedDistributions = []string{"testing"}

	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "amd64"),
		http.StatusBadRequest, `distribution "stable"`)
	expectStatus(t, uploadDeb(t, s, "suite=testing&component=main", "amd64"),
		http.StatusOK, "")
}

func TestAllowedComponents(t *testing.T) {
	s := newTestServer(t)
	s.config.Validation.AllowedComponents = []string{"contrib"}

	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "amd64"),
		http.StatusBadRequest, `component "main"`)
	expectStatus(t, uploadDeb(t, s, "suite=stable&component=contrib", "amd64"),
		http.StatusOK, "")
}

func TestAllowedArchitectures(t *testing.T) {
	s := newTestServer(t)
	s.config.Validation.AllowedArchitectures = []string{"arm64"}

	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "amd64"),
		http.StatusBadRequest, `architecture "amd64"`)
	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "arm64"),
		http.StatusOK, "")
}

func TestMaxDebSizeBytes(t *testing.T) {
	s := newTestServer(t)
	deb := buildTestDeb(t, "allow-pkg", "1.0.0", "amd64")
	s.config.Validation.MaxDebSizeBytes = int64(len(deb) - 1)

	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "amd64"),
		http.StatusRequestEntityTooLarge, "maximum size")

	s.config.Validation.MaxDebSizeBytes = int64(len(deb))
	expectStatus(t, uploadDeb(t, s, "suite=stable&component=main", "amd64"),
		http.StatusOK, "")
}

// Move checks the destination distribution against the allowlist.
func TestMoveRejectsDisallowedDistribution(t *testing.T) {
	s := newTestServer(t)
	s.config.Validation.AllowedDistributions = []string{"stable"}

	rec := doMove(t, s, `{"from_suite":"stable","to_suite":"testing"}`)
	expectStatus(t, rec, http.StatusBadRequest, `distribution "testing"`)
}

// Remove checks the suite in the path against the allowlist.
func TestRemoveRejectsDisallowedDistribution(t *testing.T) {
	s := newTestServer(t)
	s.config.Validation.AllowedDistributions = []string{"testing"}

	req := httptest.NewRequest("DELETE",
		"/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64", nil)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleRemovePackage(rec, withTestRepo(req, s.registry.All()[0]))
	expectStatus(t, rec, http.StatusBadRequest, `distribution "stable"`)
}
