// Package repo groups per-repository objects and provides routing utilities.
package repo

import (
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/reconcile"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
)

// Metadata holds repository metadata (Origin, Label, Description).
type Metadata struct {
	Origin      string
	Label       string
	Description string
}

// Repo holds all resources for a single repository instance.
type Repo struct {
	ID, VHost, PathPrefix string

	IndexMgr      *index.Manager
	MinioClient   *minio.Client
	SnapshotStore *minio.SnapshotStore
	ChecksumStore *minio.ChecksumSidecarStore
	Signer        *gpgsign.Signer
	JobManager    *reconcile.JobManager

	Metadata            Metadata
	DefaultSuite        string
	DefaultComponent    string
	ProtectDefaultSuite bool

	FeedEnabled  bool
	FeedMaxItems int

	RepoURL string

	// hydrated reports whether the snapshot has been loaded and the metadata
	// rendered. It is false from process start until hydration completes, so
	// handlers can serve a retryable response instead of a half-rendered index
	// while MinIO is slow or unreachable.
	hydrated atomic.Bool
}

// MarkHydrated records that the repository's metadata is loaded and rendered.
func (r *Repo) MarkHydrated() { r.hydrated.Store(true) }

// Hydrated reports whether the repository is ready to serve apt metadata.
func (r *Repo) Hydrated() bool { return r.hydrated.Load() }

// BuildRepo constructs a Repo from a resolved config, given base clients and a default signer.
// If the resolved config uses the default signer, defaultSigner is used; otherwise a new
// signer is constructed (and will fail fast if the key is invalid).
func BuildRepo(rc config.ResolvedRepo, base *minio.Client, defaultSigner *gpgsign.Signer, publicScheme, defaultHost string) (*Repo, error) {
	// Scope the MinIO client to this repo's bucket and prefix.
	//
	// The bucket is not checked here. Doing so made startup depend on MinIO:
	// if storage was slow or unreachable the process would refuse to start and
	// the orchestrator would crash-loop it. The bucket is verified during
	// background hydration instead, which retries until storage is reachable.
	minioClient := base.WithBucketAndPrefix(rc.Bucket, rc.KeyPrefix)

	// Select signer
	var signer *gpgsign.Signer
	if rc.UsesDefaultSigner {
		signer = defaultSigner
	} else {
		var err error
		signer, err = gpgsign.NewSigner(rc.SigningKeyPath, rc.SigningPassphrase, true)
		if err != nil {
			return nil, fmt.Errorf("failed to load signing key for repo %s: %w", rc.ID, err)
		}
	}

	// Build repo URL from vhost/path/scheme
	repoURL := buildRepoURL(publicScheme, rc.VHost, rc.PathPrefix, defaultHost)

	rp := &Repo{
		ID:                  rc.ID,
		VHost:               rc.VHost,
		PathPrefix:          rc.PathPrefix,
		IndexMgr:            index.NewManager(),
		MinioClient:         minioClient,
		SnapshotStore:       minio.NewSnapshotStore(minioClient, rc.SnapshotKey, minio.DefaultHistoryPrefix(rc.SnapshotKey), rc.SnapshotHistoryKeep),
		ChecksumStore:       minio.NewChecksumSidecarStore(minioClient),
		Signer:              signer,
		JobManager:          reconcile.NewJobManager(),
		Metadata:            Metadata{Origin: rc.Origin, Label: rc.Label, Description: rc.Description},
		DefaultSuite:        rc.DefaultSuite,
		DefaultComponent:    rc.DefaultComponent,
		ProtectDefaultSuite: rc.ProtectDefaultSuite,
		FeedEnabled:         rc.FeedEnabled,
		FeedMaxItems:        rc.FeedMaxItems,
		RepoURL:             repoURL,
	}

	slog.Info("built repo", "id", rp.ID, "vhost", rp.VHost, "path_prefix", rp.PathPrefix, "repo_url", rp.RepoURL, "signer", "default_signer="+fmt.Sprintf("%v", rc.UsesDefaultSigner))

	return rp, nil
}

// buildRepoURL constructs the public base URL for a repo from vhost, path, and defaults.
func buildRepoURL(scheme, vhost, pathPrefix, defaultHost string) string {
	if scheme == "" {
		scheme = "https"
	}

	host := vhost
	if host == "" {
		host = defaultHost
	}

	path := strings.TrimSuffix(pathPrefix, "/")

	return scheme + "://" + host + path
}

// RoutePattern returns the net/http.ServeMux pattern for a repo and a path.
// For example: "debs.example.com/dists/" or "/dists/" (if no vhost) or "/myorg/dists/" (if prefix only).
func RoutePattern(rp *Repo, path string) string {
	prefix := strings.TrimSuffix(rp.PathPrefix, "/")
	return rp.VHost + prefix + path
}

// Registry holds a set of repositories and provides lookup by ID.
type Registry struct {
	repos map[string]*Repo
	list  []*Repo // preserve order
}

// NewRegistry creates a registry from a list of repos, validating uniqueness and no duplicates.
func NewRegistry(repos []*Repo) (*Registry, error) {
	if len(repos) == 0 {
		return nil, fmt.Errorf("registry requires at least one repo")
	}

	// Check for duplicate IDs
	idMap := make(map[string]bool)
	for _, r := range repos {
		if idMap[r.ID] {
			return nil, fmt.Errorf("duplicate repo ID: %s", r.ID)
		}
		idMap[r.ID] = true
	}

	// Check for duplicate (vhost, path_prefix) pairs
	vhostPathMap := make(map[string]bool)
	for _, r := range repos {
		key := r.VHost + "|" + r.PathPrefix
		if vhostPathMap[key] {
			return nil, fmt.Errorf("duplicate (vhost, path_prefix): %s + %s", r.VHost, r.PathPrefix)
		}
		vhostPathMap[key] = true
	}

	reposByID := make(map[string]*Repo)
	for _, r := range repos {
		reposByID[r.ID] = r
	}

	return &Registry{
		repos: reposByID,
		list:  repos,
	}, nil
}

// All returns all repos in order.
func (r *Registry) All() []*Repo {
	return r.list
}

// ByID returns the repo with the given ID, or (nil, false) if not found.
func (r *Registry) ByID(id string) (*Repo, bool) {
	rp, ok := r.repos[id]
	return rp, ok
}
