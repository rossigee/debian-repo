package repo

import (
	"context"
	"errors"
)

// RepoRecord is the CRUD-facing representation of one repo's configuration.
// It mirrors config.ResolvedRepo's fields (the fully-defaulted runtime view)
// rather than config.RepoConfig (the sparse YAML input with inherit-vs-override
// pointer semantics), since callers of RepoConfigStore want a complete,
// unambiguous record — not YAML-authoring ergonomics.
// nolint:revive // name is clear in context; Record alone would be ambiguous
type RepoRecord struct {
	ID, VHost, PathPrefix string

	Endpoint, Bucket, AccessKey, SecretKey, CACert string
	UseTLS                                         bool
	KeyPrefix                                      string

	SigningKeyPath, SigningPassphrase string
	UsesDefaultSigner                 bool

	Origin, Label, Description     string
	DefaultSuite, DefaultComponent string
	ProtectDefaultSuite            bool

	FeedEnabled  bool
	FeedMaxItems int

	SnapshotKey         string
	SnapshotHistoryKeep int
	StagingPrefix       string
}

// SuiteRecord is a first-class, declared suite configuration, distinct from
// the lazily-created runtime model.Distribution. It represents "this suite
// is meant to exist" independent of whether any package has been uploaded
// to it yet.
type SuiteRecord struct {
	RepoID                     string
	Name                       string   // e.g. "stable", "testing"
	AllowedComponents          []string // empty = no restriction (any component allowed)
	Protected                  bool     // mirrors ResolvedRepo.ProtectDefaultSuite semantics but per-suite
	Codename                   string   // optional; falls back to Name if empty (matches today's GetOrCreateDistribution(suite, suite, ...) call pattern)
	Origin, Label, Description string   // optional overrides; empty = inherit repo-level Metadata
}

// ErrReadOnlyStore is returned by write methods on a RepoConfigStore
// implementation that does not support mutation (e.g. StaticConfigFile).
// Callers at a future API layer should map this to HTTP 501 Not Implemented
// (not 403 Forbidden — the operation isn't disallowed by policy, the backend
// simply doesn't support persistence yet).
var ErrReadOnlyStore = errors.New("repo config store is read-only")

// RepoConfigStore is the CRUD abstraction over repo and suite configuration.
// Implementations MUST be safe for concurrent use.
// nolint:revive // name is clear in context; ConfigStore alone would be ambiguous
type RepoConfigStore interface {
	ListRepos(ctx context.Context) ([]RepoRecord, error)
	GetRepo(ctx context.Context, id string) (RepoRecord, error)
	CreateRepo(ctx context.Context, r RepoRecord) error
	UpdateRepo(ctx context.Context, r RepoRecord) error
	DeleteRepo(ctx context.Context, id string) error

	ListSuites(ctx context.Context, repoID string) ([]SuiteRecord, error)
	GetSuite(ctx context.Context, repoID, name string) (SuiteRecord, error)
	CreateSuite(ctx context.Context, s SuiteRecord) error
	UpdateSuite(ctx context.Context, s SuiteRecord) error
	DeleteSuite(ctx context.Context, repoID, name string) error
}
