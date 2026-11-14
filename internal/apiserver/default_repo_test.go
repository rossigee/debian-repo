package apiserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// A repo-scoped handler reached without a repository in its context must 404.
// It must not fall back to the first repository's content.
func TestRepoScopedHandlerWithoutRepoReturns404(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handleDists(rec, httptest.NewRequest("GET", "/dists/stable/Release", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("handleDists without repo: status = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleIndexJSON(rec, httptest.NewRequest("GET", "/index.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("handleIndexJSON without repo: status = %d, want 404", rec.Code)
	}
}

// /readyz is global, so it must wait for every repository, not just the first.
func TestReadyzWaitsForEveryRepository(t *testing.T) {
	first := &repo.Repo{ID: "first", PathPrefix: "/first", IndexMgr: index.NewManager()}
	second := &repo.Repo{ID: "second", PathPrefix: "/second", IndexMgr: index.NewManager()}
	reg, err := repo.NewRegistry([]*repo.Repo{first, second})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	s := &Server{registry: reg}

	first.MarkHydrated()
	rec := httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("first hydrated, second not: status = %d, want 503", rec.Code)
	}

	second.MarkHydrated()
	rec = httptest.NewRecorder()
	s.handleReadyz(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("all hydrated: status = %d, want 200", rec.Code)
	}
}
