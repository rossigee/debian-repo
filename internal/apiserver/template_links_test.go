package apiserver

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

var hrefPattern = regexp.MustCompile(`href="([^"]*)"`)

// Links a repository page may point at without the repository's prefix: static
// assets and the global account and login routes are served at the root.
var globalHrefPrefixes = []string{"/static/", "/profile", "/logout", "/login", "/auth/"}

func prefixedRepo(s *Server, prefix, repoURL string) *repo.Repo {
	plain := s.registry.All()[0]
	return &repo.Repo{
		ID:         "prefixed",
		PathPrefix: prefix,
		RepoURL:    repoURL,
		IndexMgr:   plain.IndexMgr,
		JobManager: plain.JobManager,
		Signer:     plain.Signer,
	}
}

// Every repository-scoped page, rendered for a prefix-routed repository, must
// keep its links under that prefix. A link to "/" or "/install.html" would send
// the user to the default repository.
func TestRepositoryPagesStayInsidePathPrefix(t *testing.T) {
	s := newTestServer(t)
	rp := prefixedRepo(s, "/testing", "https://debs.example.com/testing")

	pages := []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"card index", s.handleIndexCards, "/testing/?suite=stable"},
		{"install", s.handleInstall, "/testing/install.html?suite=stable"},
		{"gpg key", s.handleGPGKeyPage, "/testing/gpg-key.html"},
		{"package detail", s.handlePackageDetail, "/testing/packages/test-pkg"},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", page.path, nil)
			page.handler(rec, withTestRepo(req, rp))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d: %s", page.path, rec.Code, rec.Body.String())
			}

			for _, m := range hrefPattern.FindAllStringSubmatch(rec.Body.String(), -1) {
				href := m[1]
				if !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") {
					continue // relative, absolute URL or fragment
				}
				if strings.HasPrefix(href, "/testing") || hasAnyPrefix(href, globalHrefPrefixes) {
					continue
				}
				t.Errorf("%s: link %q escapes the repository's path prefix", page.name, href)
			}
		})
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// The install and GPG pages publish the repository's own address. Nothing is
// hard-coded, so a second repository never shows the first one's host.
func TestInstallAndGPGPagesShowRepositoryHost(t *testing.T) {
	s := newTestServer(t)
	rp := prefixedRepo(s, "/testing", "https://debs.example.com/testing")

	for _, page := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{"install", s.handleInstall, "/testing/install.html?suite=stable"},
		{"gpg key", s.handleGPGKeyPage, "/testing/gpg-key.html"},
	} {
		rec := httptest.NewRecorder()
		page.handler(rec, withTestRepo(httptest.NewRequest("GET", page.path, nil), rp))
		body := rec.Body.String()

		if !strings.Contains(body, "https://debs.example.com/testing/pubkey.gpg") {
			t.Errorf("%s: missing the repository's own key URL", page.name)
		}
		if strings.Contains(body, "myorgname") {
			t.Errorf("%s: contains a hard-coded organisation host", page.name)
		}
	}

	rec := httptest.NewRecorder()
	s.handleInstall(rec, withTestRepo(httptest.NewRequest("GET", "/testing/install.html?suite=stable", nil), rp))
	if !strings.Contains(rec.Body.String(), "machine debs.example.com") {
		t.Error("install: auth.conf.d machine line does not name the repository host")
	}
}

// With no public host the URL is path-only. The pages must not publish that
// path as if it were a usable address.
func TestPagesWithoutPublicHostUseAPlaceholder(t *testing.T) {
	s := newTestServer(t)
	rp := prefixedRepo(s, "/testing", "/testing")

	rec := httptest.NewRecorder()
	s.handleInstall(rec, withTestRepo(httptest.NewRequest("GET", "/testing/install.html?suite=stable", nil), rp))
	body := rec.Body.String()
	if strings.Contains(body, "/testing/pubkey.gpg") {
		t.Error("install: publishes a path-only key URL")
	}
	if !strings.Contains(body, "REPOSITORY_URL/pubkey.gpg") || !strings.Contains(body, "machine REPOSITORY_HOST") {
		t.Error("install: missing the placeholder for the unknown host")
	}

	rec = httptest.NewRecorder()
	s.handleGPGKeyPage(rec, withTestRepo(httptest.NewRequest("GET", "/testing/gpg-key.html", nil), rp))
	if !strings.Contains(rec.Body.String(), "REPOSITORY_URL/pubkey.gpg") {
		t.Error("gpg key: missing the placeholder for the unknown host")
	}
}
