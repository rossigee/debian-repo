package apiserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/acl"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptauth"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptmeta"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
	"git.golder.lan/rossgolderltd/debian-repo/internal/webauth"
)

// Deps holds shared dependencies for the API server
type Deps struct {
	Config     *config.Config
	Authorizer acl.Authorizer
	CITokens   map[string]string // token -> identity
	AptAuth    *aptauth.Store
	WebAuth    *webauth.Provider
}

// Server holds the HTTP server state with a repo registry
type Server struct {
	registry       *repo.Registry
	deps           Deps
	config         *config.Config
	ciTokens       map[string]string // token -> identity
	authorizer     acl.Authorizer
	aptAuth        *aptauth.Store
	webAuth        *webauth.Provider
	templateLoader *templates.Loader
}

// NewServer creates a new API server with a repo registry
// For Phase 1, populates single-repo fields from the (single) repo
func NewServer(registry *repo.Registry, cfg *config.Config, aptAuth *aptauth.Store, webAuth *webauth.Provider) (*Server, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if aptAuth == nil {
		return nil, fmt.Errorf("aptAuth is required")
	}

	ciTokens := make(map[string]string)
	for _, tokenCfg := range cfg.Auth.CITokens {
		ciTokens[tokenCfg.Token] = tokenCfg.Identity
	}

	authorizer := acl.FromConfig(cfg.Auth.CITokens)
	deps := Deps{
		Config:     cfg,
		Authorizer: authorizer,
		CITokens:   ciTokens,
		AptAuth:    aptAuth,
		WebAuth:    webAuth,
	}

	return &Server{
		registry:       registry,
		deps:           deps,
		config:         cfg,
		ciTokens:       ciTokens,
		authorizer:     authorizer,
		aptAuth:        aptAuth,
		webAuth:        webAuth,
		templateLoader: templates.NewLoader(),
	}, nil
}

// RegisterWithMetrics sets up all HTTP routes including metrics handler
func (s *Server) RegisterWithMetrics(mux *http.ServeMux, metricsHandler http.Handler) {
	// Register metrics BEFORE calling Register() so "/" doesn't shadow it
	if metricsHandler != nil {
		mux.HandleFunc("/metrics", metricsHandler.ServeHTTP)
	}
	s.Register(mux)
}

// Register sets up all HTTP routes, including global routes and per-repo routes
func (s *Server) Register(mux *http.ServeMux) {
	// Register global routes once
	s.registerGlobalRoutes(mux)

	// Register per-repo routes
	for _, rp := range s.registry.All() {
		s.registerRepoRoutes(mux, rp)
	}
}

// registerGlobalRoutes registers routes that are not repo-specific
func (s *Server) registerGlobalRoutes(mux *http.ServeMux) {
	// Health checks (always public)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/readiness", s.handleReadyz)

	// Static assets (always public)
	mux.HandleFunc("/static/", s.handleStatic)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/favicon.svg", s.handleStatic) // Serve via static handler
	mux.HandleFunc("/robots.txt", s.handleRobots)

	// OIDC auth routes (always unauthenticated - required for auth flow)
	if s.deps.WebAuth != nil {
		mux.HandleFunc("/login", s.handleLogin)
		mux.HandleFunc("/auth/login", s.deps.WebAuth.HandleLogin)
		mux.HandleFunc("/auth/callback", s.deps.WebAuth.HandleCallback)
		mux.HandleFunc("/auth/logout", s.deps.WebAuth.HandleLogout)
	}

	// User pages (OIDC auth protected, repo-independent - registered once
	// globally so additional repos don't cause duplicate registrations)
	if s.deps.WebAuth != nil {
		mux.HandleFunc("/profile", s.deps.WebAuth.Middleware(s.wrapMethod(s.handleProfile, "GET")))
		mux.HandleFunc("/logout", s.handleLogout)
	}
}

// registerRepoRoutes registers routes for a single repository
// For Phase 1, we store the repo in the request context so handlers can access it
func (s *Server) registerRepoRoutes(mux *http.ServeMux, rp *repo.Repo) {
	pattern := func(path string) string { return repo.RoutePattern(rp, path) }

	// HTML pages: always public (users need access to install instructions and GPG key)
	mux.HandleFunc(pattern("/"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleIndexCards(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/install.html"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleInstall(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/gpg-key.html"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleGPGKeyPage(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/packages/"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handlePackageDetail(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))

	// JSON API endpoints (always public)
	mux.HandleFunc(pattern("/index.json"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleIndexJSON(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))

	// Repository metadata endpoints (public)
	mux.HandleFunc(pattern("/pubkey.gpg"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handlePublicKey(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/dists/{suite}/feed.atom"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleSuiteFeed(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/dists/"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		s.handleDists(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET", "HEAD"))

	if s.deps.Config.PoolServeMode == "proxy" {
		mux.HandleFunc(pattern("/pool/"), s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
			s.handlePoolProxy(w, r.WithContext(repoContext(r.Context(), rp)))
		}, "GET", "HEAD"))
	}

	// CI-facing endpoints (bearer token auth)
	mux.HandleFunc(pattern("/api/v1/dists"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleListDists(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/api/v1/upload"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleUpload(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST"))
	mux.HandleFunc(pattern("/api/v1/presign"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handlePresign(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST"))
	mux.HandleFunc(pattern("/api/v1/register"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleRegister(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST"))
	mux.HandleFunc(pattern("/api/v1/admin/reconcile"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleReconcile(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST"))
	mux.HandleFunc(pattern("/api/v1/admin/reconcile/{jobID}"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleReconcileStatus(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "GET"))
	mux.HandleFunc(pattern("/api/v1/admin/move"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleMovePackages(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST"))
	mux.HandleFunc(pattern("/api/v1/dists/"), s.wrapAuth(func(w http.ResponseWriter, r *http.Request) {
		s.handleRemovePackage(w, r.WithContext(repoContext(r.Context(), rp)))
	}, "POST", "DELETE"))
}

// wrapMethod enforces allowed HTTP methods
func (s *Server) wrapMethod(handler http.HandlerFunc, methods ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		allowed := false
		for _, method := range methods {
			if r.Method == method {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

// wrapAuth checks bearer token authentication using constant-time comparison
func (s *Server) wrapAuth(handler http.HandlerFunc, methods ...string) http.HandlerFunc {
	return s.wrapMethod(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		identity, ok := s.constantTimeTokenLookup(token)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		r.Header.Set("X-CI-Identity", identity)
		handler(w, r)
	}, methods...)
}

// Helper functions for repo context

type repoContextKey struct{}

// stripRepoPrefix removes the repo's path prefix from a request path so
// handlers can parse repo-relative paths regardless of prefix routing.
// Repos without a path prefix are unaffected.
func stripRepoPrefix(path string, rp *repo.Repo) string {
	prefix := strings.TrimSuffix(rp.PathPrefix, "/")
	if prefix == "" {
		return path
	}
	return strings.TrimPrefix(path, prefix)
}

// repoContext returns a context with the repo attached
func repoContext(ctx context.Context, rp *repo.Repo) context.Context {
	return context.WithValue(ctx, repoContextKey{}, rp)
}

// currentRepo retrieves the repo from the request context
// For Phase 1, always returns the repo (should only be one)
// In Phase 2, will use this to route to the correct repo
func (s *Server) currentRepo(r *http.Request) *repo.Repo {
	rp, ok := r.Context().Value(repoContextKey{}).(*repo.Repo)
	if ok && rp != nil {
		return rp
	}
	// Fallback: return the first repo (should only be one in Phase 1)
	repos := s.registry.All()
	if len(repos) > 0 {
		return repos[0]
	}
	return nil
}

// constantTimeTokenLookup performs a constant-time comparison against all configured tokens
func (s *Server) constantTimeTokenLookup(token string) (string, bool) {
	tokenBytes := []byte(token)
	for t, identity := range s.deps.CITokens {
		if subtle.ConstantTimeCompare([]byte(t), tokenBytes) == 1 {
			return identity, true
		}
	}
	return "", false
}

// handleHealthz returns 200 if the service is alive
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "OK\n")
}

// handleReadyz returns 200 if ready to serve
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Check if any repo's index is loaded
	rp := s.currentRepo(r)
	if rp == nil || len(rp.IndexMgr.ListDistributions()) == 0 {
		http.Error(w, "Service not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "Ready\n")
}

// handleLogin initiates OIDC authentication flow with optional return_to redirect
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/"
	}
	http.Redirect(w, r, fmt.Sprintf("/auth/login?return_to=%s", url.QueryEscape(returnTo)), http.StatusFound)
}

// MetricsAuthMiddleware wraps an http.Handler with shared-secret bearer token authentication
// Usable by the separate metrics server in main.go
func MetricsAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		want := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(auth), []byte(want)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// renderAndStoreDistribution regenerates Release and Packages files for a distribution
func (s *Server) renderAndStoreDistribution(suite string, dist *model.Distribution, rp *repo.Repo) {
	// Generate Release file with signatures
	releaseData, err := aptmeta.RenderRelease(dist, rp.Metadata.Origin, rp.Metadata.Label, rp.Metadata.Description)
	if err != nil {
		slog.Warn("failed to render release", "suite", suite, "error", err)
		return
	}

	// Create detached signature (Release.gpg)
	releaseGPG, err := rp.Signer.DetachSign(releaseData)
	if err != nil {
		slog.Warn("failed to create detached signature", "suite", suite, "error", err)
		return
	}

	// Create clearsigned version (InRelease)
	inRelease, err := rp.Signer.ClearSign(releaseData)
	if err != nil {
		slog.Warn("failed to create clearsigned release", "suite", suite, "error", err)
		return
	}

	// Render Packages for all architectures (use first component only)
	packagesByArch := make(map[string][]byte)
	packagesGzByArch := make(map[string][]byte)

	// Snapshot component/architecture lists under the dist read lock.
	// The Render* helpers lock internally, but enumerating the maps and
	// slice here without the lock races with concurrent mutations.
	dist.Mu.RLock()
	components := make([]string, 0, len(dist.Components))
	for compName := range dist.Components {
		components = append(components, compName)
	}
	sort.Strings(components)
	archs := make([]string, len(dist.Architectures))
	copy(archs, dist.Architectures)
	dist.Mu.RUnlock()

	for _, arch := range archs {
		if len(components) == 0 {
			continue
		}
		compName := components[0] // Always use first component (sorted)
		uncompressed, compressed, err := aptmeta.RenderPackages(dist, compName, arch)
		if err != nil {
			slog.Warn("failed to render packages", "suite", suite, "component", compName, "arch", arch, "error", err)
			continue
		}
		packagesByArch[arch] = uncompressed
		packagesGzByArch[arch] = compressed
	}

	// Store rendered distribution
	rendered := &model.RenderedDist{
		Suite:            suite,
		ReleasePlain:     releaseData,
		ReleaseGPG:       releaseGPG,
		InRelease:        inRelease,
		PackagesByArch:   packagesByArch,
		PackagesGzByArch: packagesGzByArch,
		RenderedAt:       time.Now(),
	}

	if err := rp.IndexMgr.SetRenderedDist(suite, rendered); err != nil {
		slog.Warn("failed to set rendered distribution", "suite", suite, "error", err)
	}
}
