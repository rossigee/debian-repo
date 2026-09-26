package repo

import (
	"context"
	"fmt"

	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
)

// StaticConfigFile is a RepoConfigStore backed by the already-parsed YAML
// config.Config. It is read-only: all mutation methods return ErrReadOnlyStore.
// Unlike aptauth.Store, it does not need a mutex or periodic reload — its
// backing data (*config.Config) is already fully loaded into memory once at
// startup and never changes for the process lifetime. The in-memory/reload
// pattern from aptauth.Store matters for a future MinIO-JSON-backed
// RepoConfigStore implementation, not this one.
type StaticConfigFile struct {
	cfg *config.Config
}

// NewStaticConfigFile creates a new StaticConfigFile store backed by the given config.
func NewStaticConfigFile(cfg *config.Config) *StaticConfigFile {
	return &StaticConfigFile{cfg: cfg}
}

// ListRepos returns all configured repositories.
func (s *StaticConfigFile) ListRepos(_ context.Context) ([]RepoRecord, error) {
	resolved, err := config.ResolveRepos(s.cfg)
	if err != nil {
		return nil, err
	}
	records := make([]RepoRecord, len(resolved))
	for i, rr := range resolved {
		records[i] = toRepoRecord(rr)
	}
	return records, nil
}

// GetRepo returns a single repository by ID.
func (s *StaticConfigFile) GetRepo(_ context.Context, id string) (RepoRecord, error) {
	rr, err := config.ResolveRepo(s.cfg, id)
	if err != nil {
		return RepoRecord{}, err
	}
	return toRepoRecord(*rr), nil
}

// CreateRepo is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) CreateRepo(_ context.Context, _ RepoRecord) error {
	return ErrReadOnlyStore
}

// UpdateRepo is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) UpdateRepo(_ context.Context, _ RepoRecord) error {
	return ErrReadOnlyStore
}

// DeleteRepo is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) DeleteRepo(_ context.Context, _ string) error {
	return ErrReadOnlyStore
}

// ListSuites is empty today: suites cannot be declared in YAML config yet,
// so this always returns an empty list.
func (s *StaticConfigFile) ListSuites(_ context.Context, _ string) ([]SuiteRecord, error) {
	return nil, nil
}

// GetSuite is not found today: suites cannot be declared in YAML config yet.
func (s *StaticConfigFile) GetSuite(_ context.Context, repoID, name string) (SuiteRecord, error) {
	return SuiteRecord{}, fmt.Errorf("suite not found: %s/%s", repoID, name)
}

// CreateSuite is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) CreateSuite(_ context.Context, _ SuiteRecord) error {
	return ErrReadOnlyStore
}

// UpdateSuite is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) UpdateSuite(_ context.Context, _ SuiteRecord) error {
	return ErrReadOnlyStore
}

// DeleteSuite is not supported on the read-only StaticConfigFile.
func (s *StaticConfigFile) DeleteSuite(_ context.Context, _, _ string) error {
	return ErrReadOnlyStore
}

// toRepoRecord converts a config.ResolvedRepo to a RepoRecord.
func toRepoRecord(rr config.ResolvedRepo) RepoRecord {
	return RepoRecord{
		ID:                  rr.ID,
		VHost:               rr.VHost,
		PathPrefix:          rr.PathPrefix,
		Endpoint:            rr.Endpoint,
		Bucket:              rr.Bucket,
		AccessKey:           rr.AccessKey,
		SecretKey:           rr.SecretKey,
		UseTLS:              rr.UseTLS,
		CACert:              rr.CACert,
		KeyPrefix:           rr.KeyPrefix,
		SigningKeyPath:      rr.SigningKeyPath,
		SigningPassphrase:   rr.SigningPassphrase,
		UsesDefaultSigner:   rr.UsesDefaultSigner,
		Origin:              rr.Origin,
		Label:               rr.Label,
		Description:         rr.Description,
		DefaultSuite:        rr.DefaultSuite,
		DefaultComponent:    rr.DefaultComponent,
		ProtectDefaultSuite: rr.ProtectDefaultSuite,
		FeedEnabled:         rr.FeedEnabled,
		FeedMaxItems:        rr.FeedMaxItems,
		SnapshotKey:         rr.SnapshotKey,
		SnapshotHistoryKeep: rr.SnapshotHistoryKeep,
		StagingPrefix:       rr.StagingPrefix,
	}
}

// ToResolvedRepo converts a RepoRecord back to a config.ResolvedRepo.
// Exported so main.go doesn't need internal knowledge of the conversion.
func (r RepoRecord) ToResolvedRepo() config.ResolvedRepo {
	return config.ResolvedRepo{
		ID:                  r.ID,
		VHost:               r.VHost,
		PathPrefix:          r.PathPrefix,
		Endpoint:            r.Endpoint,
		Bucket:              r.Bucket,
		AccessKey:           r.AccessKey,
		SecretKey:           r.SecretKey,
		UseTLS:              r.UseTLS,
		CACert:              r.CACert,
		KeyPrefix:           r.KeyPrefix,
		SigningKeyPath:      r.SigningKeyPath,
		SigningPassphrase:   r.SigningPassphrase,
		UsesDefaultSigner:   r.UsesDefaultSigner,
		Origin:              r.Origin,
		Label:               r.Label,
		Description:         r.Description,
		DefaultSuite:        r.DefaultSuite,
		DefaultComponent:    r.DefaultComponent,
		ProtectDefaultSuite: r.ProtectDefaultSuite,
		FeedEnabled:         r.FeedEnabled,
		FeedMaxItems:        r.FeedMaxItems,
		SnapshotKey:         r.SnapshotKey,
		SnapshotHistoryKeep: r.SnapshotHistoryKeep,
		StagingPrefix:       r.StagingPrefix,
	}
}
