package apiserver

import (
	"log/slog"
	"net/http"

	"git.golder.lan/rossgolderltd/debian-repo/internal/feed"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// handleSuiteFeed serves the Atom feed for a suite's recent package uploads.
func (s *Server) handleSuiteFeed(w http.ResponseWriter, r *http.Request) {
	rp, ok := r.Context().Value(repoContextKey{}).(*repo.Repo)
	if !ok || rp == nil {
		http.NotFound(w, r)
		return
	}
	suite := r.PathValue("suite")

	// Check if feeds are enabled for this repo
	if !rp.FeedEnabled {
		http.NotFound(w, r)
		return
	}

	// Verify the suite exists
	if _, ok := rp.IndexMgr.GetDistribution(suite); !ok {
		http.NotFound(w, r)
		return
	}

	// Get recent package versions
	entries := rp.IndexMgr.RecentVersions(suite, rp.FeedMaxItems)

	// Render Atom feed
	body, err := feed.RenderAtomFeed(rp.RepoURL, suite, entries)
	if err != nil {
		slog.Error("failed to render atom feed", "repo_id", rp.ID, "suite", suite, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write atom feed response", "repo_id", rp.ID, "suite", suite, "error", err)
	}
}
