package apiserver

import (
	"log/slog"
	"net/http"

	"git.golder.lan/rossgolderltd/debian-repo/internal/acl"
)

func (s *Server) denyIfNotAuthorized(w http.ResponseWriter, identity string, op acl.Operation, suite, component string, r *http.Request) bool {
	// Get the repo from the request context (set during route registration)
	rp := s.currentRepo(r)
	repoID := "unknown"
	if rp != nil {
		repoID = rp.ID
	}

	decision := s.authorizer.Authorize(acl.Request{
		Identity:  identity,
		Operation: op,
		Repo:      repoID,
		Suite:     suite,
		Component: component,
	})

	if decision.Allowed {
		return false
	}

	slog.Warn("acl denied", "identity", identity, "operation", op, "repo", repoID, "suite", suite, "component", component)
	http.Error(w, "Forbidden", http.StatusForbidden)
	return true
}
