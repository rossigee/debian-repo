package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

func TestStripRepoPrefix(t *testing.T) {
	plain := &repo.Repo{ID: "default", PathPrefix: ""}
	if got := stripRepoPrefix("/dists/stable/Release", plain); got != "/dists/stable/Release" {
		t.Fatalf("plain repo must be unaffected, got %q", got)
	}
	prefixed := &repo.Repo{ID: "testing", PathPrefix: "/testing"}
	if got := stripRepoPrefix("/testing/dists/stable/Release", prefixed); got != "/dists/stable/Release" {
		t.Fatalf("prefix not stripped, got %q", got)
	}
	trailing := &repo.Repo{ID: "testing", PathPrefix: "/testing/"}
	if got := stripRepoPrefix("/testing/pool/main/x.deb", trailing); got != "/pool/main/x.deb" {
		t.Fatalf("trailing-slash prefix not stripped, got %q", got)
	}
	// Non-matching path is returned unchanged (mux would not route it here).
	if got := stripRepoPrefix("/other/path", prefixed); got != "/other/path" {
		t.Fatalf("unexpected rewrite, got %q", got)
	}
}

// A prefixed repo must parse the repo-relative path: before the fix,
// handleRemovePackage always 400'd for prefixed repos because it trimmed
// "/api/v1/" from "/testing/api/v1/...".
func TestHandleRemovePackagePrefixedRepo(t *testing.T) {
	s := newTestServerWithTwoPackages(t)
	rp := s.registry.All()[0]
	rp.PathPrefix = "/testing"

	req := httptest.NewRequest("DELETE",
		"/testing/api/v1/dists/stable/main/remove/another-pkg/2.0.0/amd64", nil)
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleRemovePackage(rec, withTestRepo(req, rp))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for prefixed remove, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Registering routes for two repos must not panic (profile/logout are
// global; per-repo patterns must not collide).
func TestRegisterTwoReposNoPanic(t *testing.T) {
	s := newTestServer(t)
	// A Repo carries an atomic.Bool, so it must not be copied by value. Build a
	// second Repo with its own identity rather than dereferencing the first.
	second := &repo.Repo{
		ID:         "testing",
		PathPrefix: "/testing",
		IndexMgr:   s.registry.All()[0].IndexMgr,
		JobManager: s.registry.All()[0].JobManager,
	}

	reg, err := repo.NewRegistry([]*repo.Repo{s.registry.All()[0], second})
	if err != nil {
		t.Fatalf("registry failed: %v", err)
	}
	s.registry = reg

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked with two repos: %v", r)
		}
	}()
	mux := http.NewServeMux()
	s.Register(mux)

	// Prefixed API route resolves with auth.
	req := httptest.NewRequest("GET", "/testing/api/v1/dists", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on prefixed route, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "distributions") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
