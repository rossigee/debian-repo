package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/acl"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
)

// limitedAuthorizer grants remove+upload but NOT unprotect, while keeping
// the full-grant test identity for the control case.
func useLimitedAuthorizer(s *Server) {
	s.authorizer = acl.FromConfig([]config.CITokenConfig{
		{
			Token:    "limited-token",
			Identity: "limited",
			Grants: []config.GrantConfig{
				{
					Repos:      []string{"*"},
					Suites:     []string{"*"},
					Components: []string{"*"},
					Operations: []string{"upload", "remove"},
				},
			},
		},
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
	})
	s.ciTokens = map[string]string{"limited-token": "limited", "test-token": "test"}
}

// Draining a protected suite with force requires the distinct unprotect
// grant: remove alone must 403.
func TestRemoveProtectedSuiteForceRequiresUnprotect(t *testing.T) {
	s := newTestServer(t) // stable holds exactly test-pkg
	useLimitedAuthorizer(s)

	req := httptest.NewRequest("DELETE",
		"/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req.Header.Set("X-CI-Identity", "limited")
	rec := httptest.NewRecorder()
	s.handleRemovePackage(rec, withTestRepo(req, s.registry.All()[0]))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without unprotect grant, got %d: %s", rec.Code, rec.Body.String())
	}

	// Full-grant identity still succeeds.
	req = httptest.NewRequest("DELETE",
		"/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64?force=true", nil)
	req.Header.Set("X-CI-Identity", "test")
	rec = httptest.NewRecorder()
	s.handleRemovePackage(rec, withTestRepo(req, s.registry.All()[0]))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with *-grant identity, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Same rule through the move endpoint: full-drain with force needs unprotect.
func TestMoveProtectedSuiteForceRequiresUnprotect(t *testing.T) {
	s := newTestServer(t) // stable holds exactly test-pkg
	useLimitedAuthorizer(s)

	body := `{"from_suite":"stable","to_suite":"libvirtd","packages":["test-pkg"],"force":true}`
	req := httptest.NewRequest("POST", "/api/v1/admin/move", strings.NewReader(body))
	req.Header.Set("X-CI-Identity", "limited")
	rec := httptest.NewRecorder()
	s.handleMovePackages(rec, withTestRepo(req, s.registry.All()[0]))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without unprotect grant, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Without force, draining a protected suite is 409 regardless of grants.
func TestRemoveProtectedSuiteConflictWithoutForce(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("DELETE",
		"/api/v1/dists/stable/main/remove/test-pkg/1.0.0/amd64", nil)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleRemovePackage(rec, withTestRepo(req, s.registry.All()[0]))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}
