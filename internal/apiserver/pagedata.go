package apiserver

import (
	"net/http"
	"net/url"
	"strings"

	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
)

// basePageData builds the common auth-aware PageData fields for any page handler
// that renders the navbar. Safe to call even when OIDC is not configured
// (s.deps.WebAuth == nil): returns OIDCEnabled=false and IsAuthenticated=false
// in that case.
//
// BasePath is the request's repository path prefix, so every repository-scoped
// link on the page stays under that repository.
func (s *Server) basePageData(r *http.Request) templates.PageData {
	pd := templates.PageData{
		OIDCEnabled: s.deps.WebAuth != nil,
		BasePath:    repoBasePath(s.currentRepo(r)),
	}
	if s.deps.WebAuth == nil {
		return pd
	}

	if sess, ok := s.deps.WebAuth.CurrentSession(r); ok {
		pd.IsAuthenticated = true
		pd.UserEmail = sess.Email
		pd.UserName = sess.Email // no separate name claim today; Email doubles as display name
	}

	return pd
}

// repoBasePath returns a repository's path prefix without a trailing slash.
// A root repository, or no repository, has an empty base path.
func repoBasePath(rp *repo.Repo) string {
	if rp == nil {
		return ""
	}
	return strings.TrimSuffix(rp.PathPrefix, "/")
}

// repositoryHost returns the host part of a repository URL. It is empty when the
// URL is path-only, which is the case when the repository has no public host.
func repositoryHost(repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// displayRepositoryURL returns the repository URL to show in instructions. With
// no public host the URL is path-only and cannot be used by a client, so the
// placeholder REPOSITORY_URL is shown instead; the page then says why.
func displayRepositoryURL(repoURL string) string {
	if repositoryHost(repoURL) == "" {
		return "REPOSITORY_URL"
	}
	return repoURL
}
