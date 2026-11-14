package apiserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// Package detail must resolve the package name from the repo-relative path,
// so it works for a prefix-routed repository as well as a plain one.
func TestPackageDetailResolvesPrefixedAndPlainRepos(t *testing.T) {
	s := newTestServer(t)
	plain := s.registry.All()[0]
	prefixed := &repo.Repo{
		ID:         "testing",
		PathPrefix: "/testing",
		IndexMgr:   plain.IndexMgr,
		JobManager: plain.JobManager,
	}

	cases := []struct {
		name string
		rp   *repo.Repo
		path string
		want int
	}{
		{"plain repo", plain, "/packages/test-pkg", http.StatusOK},
		{"prefixed repo", prefixed, "/testing/packages/test-pkg", http.StatusOK},
		{"prefixed repo, unknown package", prefixed, "/testing/packages/no-such-pkg", http.StatusNotFound},
		{"prefixed repo, no name", prefixed, "/testing/packages/", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tc.path, nil)
			s.handlePackageDetail(rec, withTestRepo(req, tc.rp))
			if rec.Code != tc.want {
				t.Errorf("GET %s: status = %d, want %d: %s", tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
