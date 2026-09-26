// Package main is the server entrypoint for debian-repo
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/apiserver"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptauth"
	"git.golder.lan/rossgolderltd/debian-repo/internal/aptmeta"
	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/logging"
	"git.golder.lan/rossgolderltd/debian-repo/internal/metrics"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"git.golder.lan/rossgolderltd/debian-repo/internal/tracing"
	"git.golder.lan/rossgolderltd/debian-repo/internal/version"
	"git.golder.lan/rossgolderltd/debian-repo/internal/webauth"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	var (
		configPath  = flag.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
		showVersion = flag.Bool("version", false, "Show version and exit")
		healthcheck = flag.Bool("healthcheck", false, "Run healthcheck and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("debian-repo", version.String())
		os.Exit(0)
	}

	if *healthcheck {
		resp, err := http.Get("http://localhost:5080/readyz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Initialize structured logging
	logging.Init("info")

	slog.Info("debian-repo starting", "version", version.String())
	slog.Info("loading config", "path", *configPath)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Resolve repos through the config store abstraction (synthesizes a default repo if none are configured)
	repoStore := repo.NewStaticConfigFile(cfg)
	repoRecords, err := repoStore.ListRepos(context.Background())
	if err != nil {
		slog.Error("failed to resolve repos", "error", err)
		os.Exit(1)
	}
	resolvedRepos := make([]config.ResolvedRepo, len(repoRecords))
	for i, rec := range repoRecords {
		resolvedRepos[i] = rec.ToResolvedRepo()
	}
	slog.Info("resolved repos", "count", len(resolvedRepos))

	// Create a base MinIO client (shared SDK connection, deduped by endpoint)
	// For now, use the first repo's settings; TODO: dedupe by (endpoint, accessKey) if multiple repos have different endpoints
	if len(resolvedRepos) == 0 {
		slog.Error("no repos configured")
		os.Exit(1)
	}
	firstRepo := resolvedRepos[0]
	baseMinioClient, err := minio.NewBaseClient(minio.BaseClientConfig{
		Endpoint:   firstRepo.Endpoint,
		AccessKey:  firstRepo.AccessKey,
		SecretKey:  firstRepo.SecretKey,
		UseTLS:     firstRepo.UseTLS,
		CACertPath: firstRepo.CACert,
	})
	if err != nil {
		slog.Error("failed to create base MinIO client", "error", err)
		os.Exit(1)
	}
	slog.Info("created base MinIO client", "endpoint", firstRepo.Endpoint)

	// Create default signer (used by all repos unless overridden)
	passphrase := os.Getenv("GPG_PASSPHRASE")
	defaultSigner, err := gpgsign.NewSigner(cfg.Signing.KeyPath, passphrase, true)
	if err != nil {
		slog.Error("failed to load default GPG signer", "error", err)
		os.Exit(1)
	}
	slog.Info("loaded default GPG key", "key_id", defaultSigner.KeyInfo().KeyID, "uid", defaultSigner.KeyInfo().UIDs[0])

	// Build per-repo objects
	repos := make([]*repo.Repo, 0, len(resolvedRepos))
	for _, rc := range resolvedRepos {
		rp, err := repo.BuildRepo(rc, baseMinioClient, defaultSigner, "https", firstRepo.Endpoint)
		if err != nil {
			slog.Error("failed to build repo", "repo_id", rc.ID, "error", err)
			os.Exit(1)
		}

		// Load snapshot from MinIO (or start with empty index if not found)
		snap, err := rp.SnapshotStore.GetLatestSnapshot(context.Background())
		if err != nil {
			slog.Warn("failed to load snapshot for repo, starting with empty index", "repo_id", rp.ID, "error", err)
			rp.IndexMgr.SetIndex(model.NewIndex())
		} else if snap != nil {
			slog.Info("loaded snapshot for repo", "repo_id", rp.ID, "gen", snap.SnapshotGen, "distributions", len(snap.Distributions))
			loadedIndex := model.FromSnapshot(snap, "_meta/index-snapshot.json.gz")
			rp.IndexMgr.SetIndex(loadedIndex)
		} else {
			slog.Info("no snapshot found for repo, starting with empty index", "repo_id", rp.ID)
			rp.IndexMgr.SetIndex(model.NewIndex())
		}

		// Render all distributions (generate Packages files and signatures)
		for _, suite := range rp.IndexMgr.ListDistributions() {
			dist, ok := rp.IndexMgr.GetDistribution(suite)
			if !ok {
				continue
			}

			// Generate Release file with signatures
			releaseData, err := aptmeta.RenderRelease(dist, rp.Metadata.Origin, rp.Metadata.Label, rp.Metadata.Description)
			if err != nil {
				slog.Warn("failed to render release", "repo_id", rp.ID, "suite", suite, "error", err)
				continue
			}

			// Create detached signature (Release.gpg)
			releaseGPG, err := rp.Signer.DetachSign(releaseData)
			if err != nil {
				slog.Warn("failed to create detached signature", "repo_id", rp.ID, "suite", suite, "error", err)
				continue
			}

			// Create clearsigned version (InRelease)
			inRelease, err := rp.Signer.ClearSign(releaseData)
			if err != nil {
				slog.Warn("failed to create clearsigned release", "repo_id", rp.ID, "suite", suite, "error", err)
				continue
			}

			// Render Packages for all architectures (use first component only)
			packagesByArch := make(map[string][]byte)
			packagesGzByArch := make(map[string][]byte)

			// Get components in sorted order for deterministic rendering
			components := make([]string, 0, len(dist.Components))
			for compName := range dist.Components {
				components = append(components, compName)
			}
			sort.Strings(components)

			for _, arch := range dist.Architectures {
				if len(components) == 0 {
					continue
				}
				compName := components[0] // Always use first component (sorted)
				uncompressed, compressed, err := aptmeta.RenderPackages(dist, compName, arch)
				if err != nil {
					slog.Warn("failed to render packages", "repo_id", rp.ID, "suite", suite, "component", compName, "arch", arch, "error", err)
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
				slog.Warn("failed to set rendered distribution", "repo_id", rp.ID, "suite", suite, "error", err)
			}
		}

		repos = append(repos, rp)
	}

	// Create registry
	registry, err := repo.NewRegistry(repos)
	if err != nil {
		slog.Error("failed to create repo registry", "error", err)
		os.Exit(1)
	}

	// Initialize metrics with all repos' index managers
	// (For now, use the first repo; TODO: aggregate metrics across all repos)
	_ = metrics.Init(repos[0].IndexMgr)
	if len(repos) > 0 && repos[0].IndexMgr.GetIndex() != nil {
		_ = repos[0].IndexMgr.GetIndex().SnapshotGen
	}

	// Load apt users from MinIO (shared across all repos, scoped to first repo's bucket/prefix)
	// NOTE: must use a bucket-scoped client (repos[0].MinioClient), not the unscoped baseMinioClient,
	// otherwise every Get/Put fails with "Bucket name cannot be empty".
	aptUserMinioStore := minio.NewAptUserStore(repos[0].MinioClient, cfg.Auth.AptUsers.StoreKey)
	aptStore := aptauth.NewStore()
	if err := aptStore.ReloadFrom(context.Background(), aptUserMinioStore); err != nil {
		slog.Warn("failed to load apt users, starting with no apt users", "error", err)
	}

	// Start apt-user reload ticker
	reloadCtx, cancelReload := context.WithCancel(context.Background())
	go aptStore.StartReloadLoop(reloadCtx, aptUserMinioStore, cfg.Auth.AptUsers.ReloadInterval)

	// Initialize OIDC provider if configured (shared across all repos)
	var webAuthProvider *webauth.Provider
	if cfg.Auth.OIDC.IssuerURL != "" {
		webAuthProvider, err = webauth.NewProvider(context.Background(), webauth.Config{
			IssuerURL:    cfg.Auth.OIDC.IssuerURL,
			ClientID:     cfg.Auth.OIDC.ClientID,
			ClientSecret: cfg.Auth.OIDC.ClientSecret,
			RedirectURL:  cfg.Auth.OIDC.RedirectURL,
			CookieSecret: cfg.Auth.OIDC.CookieSecret,
			SessionTTL:   cfg.Auth.OIDC.SessionTTL,
		})
		if err != nil {
			slog.Warn("failed to initialize OIDC provider, browser routes will be served unauthenticated", "error", err, "issuer", cfg.Auth.OIDC.IssuerURL)
			webAuthProvider = nil
		} else {
			slog.Info("initialized OIDC provider", "issuer", cfg.Auth.OIDC.IssuerURL)
		}
	} else {
		slog.Warn("OIDC not configured, browser routes will be served unauthenticated")
	}

	// Initialize OTLP tracing if configured
	var otelCleanup func(context.Context) error
	if cfg.Tracing.Enabled {
		otelCleanup, err = tracing.InitTracer(context.Background(), cfg.Tracing.ServiceName, cfg.Tracing.OTLPHost, cfg.Tracing.OTLPPort, cfg.Tracing.Sampler, cfg.Tracing.SampleRate)
		if err != nil {
			slog.Error("failed to initialize OTLP tracing", "error", err)
			os.Exit(1)
		}
		defer func() {
			if otelCleanup != nil {
				if err := otelCleanup(context.Background()); err != nil {
					slog.Error("failed to shutdown OTLP tracer", "error", err)
				}
			}
		}()
	}

	// Create API server with the repo registry
	server, err := apiserver.NewServer(registry, cfg, aptStore, webAuthProvider)
	if err != nil {
		slog.Error("failed to create API server", "error", err)
		os.Exit(1)
	}

	// Set up HTTP routes (including authenticated /metrics)
	mux := http.NewServeMux()
	server.RegisterWithMetrics(mux, apiserver.MetricsAuthMiddleware(cfg.Auth.Metrics.Token, promhttp.Handler()))

	// Wrap with OTLP tracing middleware if enabled
	var tracedMux http.Handler = mux
	if cfg.Tracing.Enabled {
		tracedMux = tracing.HTTPMiddleware(mux)
	}

	// Wrap with request tracing middleware
	tracedMux = logging.NewRequestTracer(tracedMux)

	// Start HTTP server
	// NOTE: WriteTimeout is disabled (0) to avoid truncating large .deb downloads and long-running
	// reconcile operations. Go's WriteTimeout is a hard deadline covering handler processing + response
	// writing; for large transfers via io.Copy, it can fire mid-stream, flushing Content-Length but
	// closing the connection before all bytes are sent. HAProxy's timeouts (50s) provide the outer bound.
	httpServer := &http.Server{
		Addr:         cfg.Listen.HTTP,
		Handler:      tracedMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // disabled; HAProxy has its own 50s timeout
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	go func() {
		slog.Info("listening for requests", "addr", cfg.Listen.HTTP)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("initiating graceful shutdown")

	// Stop reload ticker
	cancelReload()

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil && err != http.ErrServerClosed {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}
