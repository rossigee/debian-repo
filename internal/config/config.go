// Package config provides configuration loading and validation for the debian-repo service.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// CITokenConfig describes one CI/CD bearer token and its ACL grants.
type CITokenConfig struct {
	Token    string        `yaml:"token"`
	Identity string        `yaml:"identity"`
	Grants   []GrantConfig `yaml:"grants"`
}

// GrantConfig is the YAML shape of one acl.Grant.
type GrantConfig struct {
	Repos      []string `yaml:"repos"`
	Suites     []string `yaml:"suites"`
	Components []string `yaml:"components"`
	Operations []string `yaml:"operations"`
}

// RepoConfig describes one repository with optional vhost/prefix and storage/signing overrides.
type RepoConfig struct {
	ID         string `yaml:"id"`          // required, unique
	VHost      string `yaml:"vhost"`       // optional, e.g. "debs.example.com"
	PathPrefix string `yaml:"path_prefix"` // optional, e.g. "/myorg"

	Storage struct {
		Endpoint  string `yaml:"endpoint"`   // falls back to global MinIO.Endpoint
		Bucket    string `yaml:"bucket"`     // falls back to global MinIO.Bucket
		AccessKey string `yaml:"access_key"` // falls back to global MinIO.AccessKey
		SecretKey string `yaml:"secret_key"` // falls back to global MinIO.SecretKey
		UseTLS    *bool  `yaml:"use_tls"`    // nil = inherit global
		CACert    string `yaml:"ca_cert"`    // falls back to global MinIO.CACert
		KeyPrefix string `yaml:"key_prefix"` // optional object storage prefix
	} `yaml:"storage"`

	Signing struct {
		KeyPath    string `yaml:"key_path"`   // falls back to global Signing.KeyPath
		Passphrase string `yaml:"passphrase"` // falls back to global or env var
	} `yaml:"signing"`

	RepoMetadata struct {
		Origin      string `yaml:"origin"`      // falls back to global
		Label       string `yaml:"label"`       // falls back to global
		Description string `yaml:"description"` // falls back to global
	} `yaml:"repo_metadata"`

	DefaultSuite        string `yaml:"default_suite"`         // default "stable"
	DefaultComponent    string `yaml:"default_component"`     // default "main"
	ProtectDefaultSuite *bool  `yaml:"protect_default_suite"` // nil = true (protected)

	Feed struct {
		Enabled  *bool `yaml:"enabled"`   // nil = true
		MaxItems int   `yaml:"max_items"` // 0 = default 20
	} `yaml:"feed"`
}

// ResolvedRepo is a fully resolved RepoConfig with every field defaulted from the global Config.
type ResolvedRepo struct {
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

// Config is the root configuration structure
type Config struct {
	Repos          []RepoConfig `yaml:"repos"`            // optional; if empty, a default repo is synthesized
	RequiredRepoID string       `yaml:"required_repo_id"` // optional; if set, must exist in Repos when explicit repos are configured

	Listen struct {
		HTTP    string `yaml:"http"`
		Metrics string `yaml:"metrics"`
	} `yaml:"listen"`

	Storage struct {
		MinIO struct {
			Endpoint  string `yaml:"endpoint"`
			Bucket    string `yaml:"bucket"`
			UseTLS    bool   `yaml:"use_tls"`
			AccessKey string `yaml:"access_key"`
			SecretKey string `yaml:"secret_key"`
			CACert    string `yaml:"ca_cert"`
		} `yaml:"minio"`
		SnapshotKey         string `yaml:"snapshot_key"`
		SnapshotHistoryKeep int    `yaml:"snapshot_history_keep"`
		StagingPrefix       string `yaml:"staging_prefix"`
	} `yaml:"storage"`

	Index struct {
		PersistMode     string        `yaml:"persist_mode"` // sync or debounced
		PersistDebounce time.Duration `yaml:"persist_debounce"`
	} `yaml:"index"`

	PoolServeMode        string `yaml:"pool_serve_mode"`        // proxy or passthrough
	PoolPassthroughStyle string `yaml:"pool_passthrough_style"` // haproxy_route or redirect

	UploadMode    string        `yaml:"upload_mode"` // direct or presigned
	PresignExpiry time.Duration `yaml:"presign_expiry"`

	Signing struct {
		KeyPath       string `yaml:"key_path"`
		Passphrase    string `yaml:"passphrase"`
		KeepDecrypted bool   `yaml:"keep_decrypted"`
	} `yaml:"signing"`

	Validation struct {
		MaxDebSizeBytes      int64    `yaml:"max_deb_size_bytes"`
		AllowedArchitectures []string `yaml:"allowed_architectures"`
		AllowedDistributions []string `yaml:"allowed_distributions"`
		AllowedComponents    []string `yaml:"allowed_components"`
	} `yaml:"validation"`

	Auth struct {
		CITokens []CITokenConfig `yaml:"ci_tokens"`

		Metrics struct {
			Token string `yaml:"token"`
		} `yaml:"metrics"`

		AptUsers struct {
			StoreKey       string        `yaml:"store_key"`
			ReloadInterval time.Duration `yaml:"reload_interval"`
		} `yaml:"apt_users"`

		OIDC struct {
			IssuerURL    string        `yaml:"issuer_url"`
			ClientID     string        `yaml:"client_id"`
			ClientSecret string        `yaml:"client_secret"`
			RedirectURL  string        `yaml:"redirect_url"`
			CookieSecret string        `yaml:"cookie_secret"`
			SessionTTL   time.Duration `yaml:"session_ttl"`
		} `yaml:"oidc"`
	} `yaml:"auth"`

	RepoMetadata struct {
		Origin      string `yaml:"origin"`
		Label       string `yaml:"label"`
		Description string `yaml:"description"`
	} `yaml:"repo_metadata"`

	Tracing struct {
		Enabled     bool    `yaml:"enabled"`
		OTLPHost    string  `yaml:"otlp_host"`
		OTLPPort    string  `yaml:"otlp_port"`
		ServiceName string  `yaml:"service_name"`
		Sampler     string  `yaml:"sampler"` // always, never, or probabilistic
		SampleRate  float64 `yaml:"sample_rate"`
	} `yaml:"tracing"`

	LogLevel string `yaml:"log_level"`
}

// Load reads config from a YAML file, with env var substitution
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Defaults
	if cfg.Listen.HTTP == "" {
		cfg.Listen.HTTP = ":8080"
	}
	if cfg.Listen.Metrics == "" {
		cfg.Listen.Metrics = ":9090"
	}
	if cfg.Storage.SnapshotKey == "" {
		cfg.Storage.SnapshotKey = "_meta/index-snapshot.json.gz"
	}
	if cfg.Storage.SnapshotHistoryKeep == 0 {
		cfg.Storage.SnapshotHistoryKeep = 20
	}
	if cfg.Storage.StagingPrefix == "" {
		cfg.Storage.StagingPrefix = "_staging/"
	}
	if cfg.Index.PersistMode == "" {
		cfg.Index.PersistMode = "sync"
	}
	if cfg.Index.PersistDebounce == 0 {
		cfg.Index.PersistDebounce = 30 * time.Second
	}
	if cfg.PoolServeMode == "" {
		cfg.PoolServeMode = "proxy"
	}
	if cfg.PoolPassthroughStyle == "" {
		cfg.PoolPassthroughStyle = "haproxy_route"
	}
	if cfg.UploadMode == "" {
		cfg.UploadMode = "direct"
	}
	if cfg.PresignExpiry == 0 {
		cfg.PresignExpiry = 15 * time.Minute
	}
	if !cfg.Signing.KeepDecrypted {
		cfg.Signing.KeepDecrypted = true
	}
	if cfg.Validation.MaxDebSizeBytes == 0 {
		cfg.Validation.MaxDebSizeBytes = 536870912 // 512MB
	}
	if cfg.RepoMetadata.Origin == "" {
		cfg.RepoMetadata.Origin = "myorgname.com"
	}
	if cfg.RepoMetadata.Label == "" {
		cfg.RepoMetadata.Label = "golder.tech Debian Repository"
	}
	if cfg.RepoMetadata.Description == "" {
		cfg.RepoMetadata.Description = "Debian package repository for golder.tech infrastructure"
	}

	if cfg.Auth.AptUsers.StoreKey == "" {
		cfg.Auth.AptUsers.StoreKey = "_meta/apt-users.json.gz"
	}
	if cfg.Auth.AptUsers.ReloadInterval == 0 {
		cfg.Auth.AptUsers.ReloadInterval = 60 * time.Second
	}
	if cfg.Auth.OIDC.SessionTTL == 0 {
		cfg.Auth.OIDC.SessionTTL = 12 * time.Hour
	}
	if cfg.Auth.Metrics.Token == "" {
		return nil, fmt.Errorf("auth.metrics.token is required")
	}

	for i, token := range cfg.Auth.CITokens {
		if len(token.Grants) == 0 {
			slog.Warn("ci token has no grants", "identity", token.Identity, "index", i)
		}
	}

	// Tracing defaults
	if cfg.Tracing.Sampler == "" {
		cfg.Tracing.Sampler = "probabilistic"
	}
	if cfg.Tracing.SampleRate == 0 {
		cfg.Tracing.SampleRate = 0.1
	}
	if cfg.Tracing.OTLPHost == "" {
		cfg.Tracing.OTLPHost = "localhost"
	}
	if cfg.Tracing.OTLPPort == "" {
		cfg.Tracing.OTLPPort = "4317"
	}
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = "debian-repo"
	}

	// Repos: if empty, auto-inject default repos into grants that have empty Repos
	if len(cfg.Repos) == 0 {
		for i := range cfg.Auth.CITokens {
			for j := range cfg.Auth.CITokens[i].Grants {
				if len(cfg.Auth.CITokens[i].Grants[j].Repos) == 0 {
					cfg.Auth.CITokens[i].Grants[j].Repos = []string{"default"}
				}
			}
		}
	}

	return cfg, nil
}

// ResolveRepos returns a list of fully resolved repos, synthesizing a default repo if none are configured.
func ResolveRepos(cfg *Config) ([]ResolvedRepo, error) {
	if len(cfg.Repos) == 0 {
		return []ResolvedRepo{synthesizeDefaultRepo(cfg)}, nil
	}

	resolved := make([]ResolvedRepo, 0, len(cfg.Repos))
	seenKeys := make(map[string]bool)

	for _, rc := range cfg.Repos {
		if rc.ID == "" {
			return nil, fmt.Errorf("repo missing required 'id' field")
		}
		if seenKeys[rc.ID] {
			return nil, fmt.Errorf("duplicate repo id: %s", rc.ID)
		}
		seenKeys[rc.ID] = true

		rr := resolveRepoConfig(cfg, rc)
		resolved = append(resolved, rr)
	}

	// Validate no two repos have the same (vhost, path_prefix) pair
	seenVhostPrefix := make(map[string]bool)
	for _, rr := range resolved {
		key := rr.VHost + "|" + rr.PathPrefix
		if seenVhostPrefix[key] {
			return nil, fmt.Errorf("duplicate (vhost, path_prefix): %s + %s", rr.VHost, rr.PathPrefix)
		}
		seenVhostPrefix[key] = true
	}

	// Validate RequiredRepoID if specified: must exist in the explicit repos list
	if cfg.RequiredRepoID != "" {
		found := false
		for _, rr := range resolved {
			if rr.ID == cfg.RequiredRepoID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("required_repo_id %q not found in repos list", cfg.RequiredRepoID)
		}
	}

	return resolved, nil
}

// ResolveRepo resolves a single repo by ID, or returns an error if not found.
func ResolveRepo(cfg *Config, id string) (*ResolvedRepo, error) {
	resolved, err := ResolveRepos(cfg)
	if err != nil {
		return nil, err
	}

	for _, rr := range resolved {
		if rr.ID == id {
			return &rr, nil
		}
	}

	return nil, fmt.Errorf("repo not found: %s", id)
}

func synthesizeDefaultRepo(cfg *Config) ResolvedRepo {
	snapshotKey := cfg.Storage.SnapshotKey
	if snapshotKey == "" {
		snapshotKey = "_meta/index-snapshot.json.gz"
	}
	historyKeep := cfg.Storage.SnapshotHistoryKeep
	if historyKeep == 0 {
		historyKeep = 20
	}
	stagingPrefix := cfg.Storage.StagingPrefix
	if stagingPrefix == "" {
		stagingPrefix = "_staging/"
	}
	return ResolvedRepo{
		ID:                  "default",
		VHost:               "",
		PathPrefix:          "",
		Endpoint:            cfg.Storage.MinIO.Endpoint,
		Bucket:              cfg.Storage.MinIO.Bucket,
		AccessKey:           cfg.Storage.MinIO.AccessKey,
		SecretKey:           cfg.Storage.MinIO.SecretKey,
		UseTLS:              cfg.Storage.MinIO.UseTLS,
		CACert:              cfg.Storage.MinIO.CACert,
		KeyPrefix:           "",
		SigningKeyPath:      cfg.Signing.KeyPath,
		SigningPassphrase:   cfg.Signing.Passphrase,
		UsesDefaultSigner:   true,
		Origin:              cfg.RepoMetadata.Origin,
		Label:               cfg.RepoMetadata.Label,
		Description:         cfg.RepoMetadata.Description,
		DefaultSuite:        "stable",
		DefaultComponent:    "main",
		ProtectDefaultSuite: true,
		FeedEnabled:         true,
		FeedMaxItems:        20,
		SnapshotKey:         snapshotKey,
		SnapshotHistoryKeep: historyKeep,
		StagingPrefix:       stagingPrefix,
	}
}

func resolveRepoConfig(cfg *Config, rc RepoConfig) ResolvedRepo {
	rr := ResolvedRepo{
		ID:                rc.ID,
		VHost:             rc.VHost,
		PathPrefix:        rc.PathPrefix,
		DefaultSuite:      rc.DefaultSuite,
		DefaultComponent:  rc.DefaultComponent,
		SigningPassphrase: rc.Signing.Passphrase,
		UsesDefaultSigner: rc.Signing.KeyPath == "",
	}

	// Storage defaults
	if rc.Storage.Endpoint == "" {
		rr.Endpoint = cfg.Storage.MinIO.Endpoint
	} else {
		rr.Endpoint = rc.Storage.Endpoint
	}

	if rc.Storage.Bucket == "" {
		rr.Bucket = cfg.Storage.MinIO.Bucket
	} else {
		rr.Bucket = rc.Storage.Bucket
	}

	if rc.Storage.AccessKey == "" {
		rr.AccessKey = cfg.Storage.MinIO.AccessKey
	} else {
		rr.AccessKey = rc.Storage.AccessKey
	}

	if rc.Storage.SecretKey == "" {
		rr.SecretKey = cfg.Storage.MinIO.SecretKey
	} else {
		rr.SecretKey = rc.Storage.SecretKey
	}

	if rc.Storage.UseTLS == nil {
		rr.UseTLS = cfg.Storage.MinIO.UseTLS
	} else {
		rr.UseTLS = *rc.Storage.UseTLS
	}

	if rc.Storage.CACert == "" {
		rr.CACert = cfg.Storage.MinIO.CACert
	} else {
		rr.CACert = rc.Storage.CACert
	}

	rr.KeyPrefix = rc.Storage.KeyPrefix

	// Signing defaults
	if rc.Signing.KeyPath == "" {
		rr.SigningKeyPath = cfg.Signing.KeyPath
		rr.UsesDefaultSigner = true
	} else {
		rr.SigningKeyPath = rc.Signing.KeyPath
		rr.UsesDefaultSigner = false
		if rr.SigningPassphrase == "" {
			rr.SigningPassphrase = cfg.Signing.Passphrase
		}
	}

	// Repo metadata defaults
	if rc.RepoMetadata.Origin == "" {
		rr.Origin = cfg.RepoMetadata.Origin
	} else {
		rr.Origin = rc.RepoMetadata.Origin
	}

	if rc.RepoMetadata.Label == "" {
		rr.Label = cfg.RepoMetadata.Label
	} else {
		rr.Label = rc.RepoMetadata.Label
	}

	if rc.RepoMetadata.Description == "" {
		rr.Description = cfg.RepoMetadata.Description
	} else {
		rr.Description = rc.RepoMetadata.Description
	}

	// Default suite/component
	if rr.DefaultSuite == "" {
		rr.DefaultSuite = "stable"
	}
	if rr.DefaultComponent == "" {
		rr.DefaultComponent = "main"
	}

	// Snapshot settings are global (no per-repo override): every repo
	// persists its own snapshot key namespace via its bucket/key prefix.
	// Fall back to literals when resolving a Config that skipped Load
	// defaults (e.g. hand-built in tests).
	rr.SnapshotKey = cfg.Storage.SnapshotKey
	if rr.SnapshotKey == "" {
		rr.SnapshotKey = "_meta/index-snapshot.json.gz"
	}
	rr.SnapshotHistoryKeep = cfg.Storage.SnapshotHistoryKeep
	if rr.SnapshotHistoryKeep == 0 {
		rr.SnapshotHistoryKeep = 20
	}
	rr.StagingPrefix = cfg.Storage.StagingPrefix
	if rr.StagingPrefix == "" {
		rr.StagingPrefix = "_staging/"
	}

	// Protected suite defaults to true (protect the default suite by default)
	if rc.ProtectDefaultSuite == nil {
		rr.ProtectDefaultSuite = true
	} else {
		rr.ProtectDefaultSuite = *rc.ProtectDefaultSuite
	}

	// Feed defaults to enabled with max 20 items
	if rc.Feed.Enabled == nil {
		rr.FeedEnabled = true
	} else {
		rr.FeedEnabled = *rc.Feed.Enabled
	}
	if rc.Feed.MaxItems == 0 {
		rr.FeedMaxItems = 20
	} else {
		rr.FeedMaxItems = rc.Feed.MaxItems
	}

	return rr
}
