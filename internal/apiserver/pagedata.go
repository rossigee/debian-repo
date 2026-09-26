package apiserver

import (
	"net/http"

	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
)

// basePageData builds the common auth-aware PageData fields for any page handler
// that renders the navbar. Safe to call even when OIDC is not configured
// (s.deps.WebAuth == nil): returns OIDCEnabled=false and IsAuthenticated=false
// in that case.
func (s *Server) basePageData(r *http.Request) templates.PageData {
	pd := templates.PageData{
		OIDCEnabled: s.deps.WebAuth != nil,
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
