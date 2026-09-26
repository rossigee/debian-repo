package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	return cfgPath
}

const baseConfig = `
listen:
  http: ":8080"
  metrics: ":9090"
storage:
  minio:
    endpoint: "minio.example.com"
    bucket: "my-bucket"
    access_key: "key"
    secret_key: "secret"
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20
  staging_prefix: "_staging/"
auth:
  ci_tokens:
    - token: "token1"
      identity: "ci1"
  metrics:
    token: "metrics-secret"
  apt_users:
    store_key: "_meta/apt-users.json.gz"
    reload_interval: "1m"
`

func TestLoadDefaults(t *testing.T) {
	cfgPath := writeConfig(t, baseConfig)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Listen.HTTP != ":8080" {
		t.Fatalf("Expected :8080, got %s", loaded.Listen.HTTP)
	}
	if loaded.Listen.Metrics != ":9090" {
		t.Fatalf("Expected default metrics :9090, got %s", loaded.Listen.Metrics)
	}
	if loaded.Storage.SnapshotKey != "_meta/index-snapshot.json.gz" {
		t.Fatalf("Expected default snapshot key, got %s", loaded.Storage.SnapshotKey)
	}
	if loaded.Storage.SnapshotHistoryKeep != 20 {
		t.Fatalf("Expected default history keep 20, got %d", loaded.Storage.SnapshotHistoryKeep)
	}
	if loaded.Storage.StagingPrefix != "_staging/" {
		t.Fatalf("Expected default staging prefix, got %s", loaded.Storage.StagingPrefix)
	}
	if loaded.Index.PersistMode != "sync" {
		t.Fatalf("Expected default persist mode sync, got %s", loaded.Index.PersistMode)
	}
	if loaded.Index.PersistDebounce != 30*time.Second {
		t.Fatalf("Expected default debounce 30s, got %v", loaded.Index.PersistDebounce)
	}
	if loaded.PoolServeMode != "proxy" {
		t.Fatalf("Expected default pool serve mode proxy, got %s", loaded.PoolServeMode)
	}
	if loaded.PoolPassthroughStyle != "haproxy_route" {
		t.Fatalf("Expected default passthrough style, got %s", loaded.PoolPassthroughStyle)
	}
	if loaded.UploadMode != "direct" {
		t.Fatalf("Expected default upload mode direct, got %s", loaded.UploadMode)
	}
	if loaded.Tracing.Sampler != "probabilistic" {
		t.Fatalf("Expected default sampler probabilistic, got %s", loaded.Tracing.Sampler)
	}
	if loaded.Tracing.SampleRate != 0.1 {
		t.Fatalf("Expected default sample rate 0.1, got %f", loaded.Tracing.SampleRate)
	}
	if loaded.Tracing.OTLPHost != "localhost" {
		t.Fatalf("Expected default OTLP host localhost, got %s", loaded.Tracing.OTLPHost)
	}
	if loaded.Tracing.OTLPPort != "4317" {
		t.Fatalf("Expected default OTLP port 4317, got %s", loaded.Tracing.OTLPPort)
	}
	if loaded.Tracing.ServiceName != "debian-repo" {
		t.Fatalf("Expected default service name, got %s", loaded.Tracing.ServiceName)
	}
	if loaded.Auth.AptUsers.StoreKey != "_meta/apt-users.json.gz" {
		t.Fatalf("Expected default apt users store key, got %s", loaded.Auth.AptUsers.StoreKey)
	}
	if loaded.Auth.AptUsers.ReloadInterval != 60*time.Second {
		t.Fatalf("Expected default reload interval 1m, got %v", loaded.Auth.AptUsers.ReloadInterval)
	}

	t.Logf("✓ LoadDefaults applies all defaults correctly")
}

func TestLoadWithCustomValues(t *testing.T) {
	cfg := baseConfig + `
index:
  persist_mode: "debounced"
  persist_debounce: "1m"
pool_serve_mode: "passthrough"
upload_mode: "presigned"
signing:
  key_path: "/keys/test.asc"
tracing:
  enabled: true
  otlp_host: "otel.example.com"
  otlp_port: "4318"
  service_name: "my-service"
  sampler: "always"
  sample_rate: 1.0
`
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Listen.HTTP != ":8080" {
		t.Fatalf("Expected :8080, got %s", loaded.Listen.HTTP)
	}
	if loaded.Index.PersistMode != "debounced" {
		t.Fatalf("Expected debounced, got %s", loaded.Index.PersistMode)
	}
	if loaded.Index.PersistDebounce != time.Minute {
		t.Fatalf("Expected 1m, got %v", loaded.Index.PersistDebounce)
	}
	if loaded.PoolServeMode != "passthrough" {
		t.Fatalf("Expected passthrough, got %s", loaded.PoolServeMode)
	}
	if loaded.UploadMode != "presigned" {
		t.Fatalf("Expected presigned, got %s", loaded.UploadMode)
	}
	if loaded.Signing.KeyPath != "/keys/test.asc" {
		t.Fatalf("Expected /keys/test.asc, got %s", loaded.Signing.KeyPath)
	}
	if !loaded.Tracing.Enabled {
		t.Fatal("Expected tracing enabled")
	}
	if loaded.Tracing.OTLPHost != "otel.example.com" {
		t.Fatalf("Expected otel.example.com, got %s", loaded.Tracing.OTLPHost)
	}
	if loaded.Tracing.OTLPPort != "4318" {
		t.Fatalf("Expected 4318, got %s", loaded.Tracing.OTLPPort)
	}
	if loaded.Tracing.ServiceName != "my-service" {
		t.Fatalf("Expected my-service, got %s", loaded.Tracing.ServiceName)
	}
	if loaded.Tracing.Sampler != "always" {
		t.Fatalf("Expected always, got %s", loaded.Tracing.Sampler)
	}
	if loaded.Tracing.SampleRate != 1.0 {
		t.Fatalf("Expected 1.0, got %f", loaded.Tracing.SampleRate)
	}

	t.Logf("✓ LoadWithCustomValues works correctly")
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Expected error for missing file")
	}

	t.Logf("✓ Load returns error for missing file")
}

func TestLoadEmptyFile(t *testing.T) {
	cfgPath := writeConfig(t, "")

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("Expected error for empty config (missing auth.metrics.token)")
	}

	t.Logf("✓ Load returns error for empty config")
}

func TestLoadInvalidYAML(t *testing.T) {
	cfgPath := writeConfig(t, "not: valid: yaml: [")
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("Expected error for invalid YAML")
	}

	t.Logf("✓ Load returns error for invalid YAML")
}

func TestLoadLogLevels(t *testing.T) {
	cfg := baseConfig + `
log_level: "debug"
`
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.LogLevel != "debug" {
		t.Fatalf("Expected debug, got %s", loaded.LogLevel)
	}

	t.Logf("✓ LoadLogLevels works correctly")
}

func TestLoadAuthConfig(t *testing.T) {
	cfg := `
listen:
  http: ":8080"
  metrics: ":9090"
storage:
  minio:
    endpoint: "minio.example.com"
    bucket: "my-bucket"
    access_key: "key"
    secret_key: "secret"
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20
  staging_prefix: "_staging/"
auth:
  ci_tokens:
    - token: "token1"
      identity: "ci1"
    - token: "token2"
      identity: "ci2"
  metrics:
    token: "metrics-secret"
  apt_users:
    store_key: "_meta/custom-users.json.gz"
    reload_interval: "2m"
`
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Auth.CITokens) != 2 {
		t.Fatalf("Expected 2 CI tokens, got %d", len(loaded.Auth.CITokens))
	}
	if loaded.Auth.CITokens[0].Token != "token1" {
		t.Fatalf("Expected token1, got %s", loaded.Auth.CITokens[0].Token)
	}
	if loaded.Auth.AptUsers.StoreKey != "_meta/custom-users.json.gz" {
		t.Fatalf("Expected custom store key, got %s", loaded.Auth.AptUsers.StoreKey)
	}
	if loaded.Auth.AptUsers.ReloadInterval != 2*time.Minute {
		t.Fatalf("Expected 2m, got %v", loaded.Auth.AptUsers.ReloadInterval)
	}

	t.Logf("✓ LoadAuthConfig works correctly")
}

func TestLoadOIDCConfig(t *testing.T) {
	cfg := `
listen:
  http: ":8080"
  metrics: ":9090"
storage:
  minio:
    endpoint: "minio.example.com"
    bucket: "my-bucket"
    access_key: "key"
    secret_key: "secret"
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20
  staging_prefix: "_staging/"
auth:
  ci_tokens:
    - token: "token1"
      identity: "ci1"
  metrics:
    token: "metrics-secret"
  apt_users:
    store_key: "_meta/apt-users.json.gz"
    reload_interval: "1m"
  oidc:
    issuer_url: "https://keycloak.example.com/realms/test"
    client_id: "my-client"
    client_secret: "secret"
    redirect_url: "https://example.com/callback"
    cookie_secret: "cookie"
`
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Auth.OIDC.IssuerURL != "https://keycloak.example.com/realms/test" {
		t.Fatalf("Expected issuer URL, got %s", loaded.Auth.OIDC.IssuerURL)
	}
	if loaded.Auth.OIDC.ClientID != "my-client" {
		t.Fatalf("Expected client ID, got %s", loaded.Auth.OIDC.ClientID)
	}
	if loaded.Auth.OIDC.ClientSecret != "secret" {
		t.Fatalf("Expected client secret, got %s", loaded.Auth.OIDC.ClientSecret)
	}

	t.Logf("✓ LoadOIDCConfig works correctly")
}

func TestLoadMetricsAuthToken(t *testing.T) {
	cfg := `
listen:
  http: ":8080"
  metrics: ":9090"
storage:
  minio:
    endpoint: "minio.example.com"
    bucket: "my-bucket"
    access_key: "key"
    secret_key: "secret"
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20
  staging_prefix: "_staging/"
auth:
  ci_tokens:
    - token: "token1"
      identity: "ci1"
  metrics:
    token: "custom-metrics-token"
  apt_users:
    store_key: "_meta/apt-users.json.gz"
    reload_interval: "1m"
`
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Auth.Metrics.Token != "custom-metrics-token" {
		t.Fatalf("Expected custom-metrics-token, got %s", loaded.Auth.Metrics.Token)
	}

	t.Logf("✓ LoadMetricsAuthToken works correctly")
}

func TestLoadMissingMetricsToken(t *testing.T) {
	cfg := `
listen:
  http: ":8080"
storage:
  minio:
    endpoint: "minio.example.com"
    bucket: "b"
    access_key: "k"
    secret_key: "s"
`
	cfgPath := writeConfig(t, cfg)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("Expected error for missing metrics.token")
	}

	t.Logf("✓ Load returns error for missing metrics token")
}

func TestLoadTracingDefaults(t *testing.T) {
	cfg := baseConfig
	cfgPath := writeConfig(t, cfg)

	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Tracing.Sampler != "probabilistic" {
		t.Fatalf("Expected probabilistic, got %s", loaded.Tracing.Sampler)
	}
	if loaded.Tracing.SampleRate != 0.1 {
		t.Fatalf("Expected 0.1, got %f", loaded.Tracing.SampleRate)
	}
	if loaded.Tracing.OTLPHost != "localhost" {
		t.Fatalf("Expected localhost, got %s", loaded.Tracing.OTLPHost)
	}
	if loaded.Tracing.OTLPPort != "4317" {
		t.Fatalf("Expected 4317, got %s", loaded.Tracing.OTLPPort)
	}
	if loaded.Tracing.ServiceName != "debian-repo" {
		t.Fatalf("Expected debian-repo, got %s", loaded.Tracing.ServiceName)
	}
	if loaded.Tracing.Enabled != false {
		t.Fatalf("Expected enabled=false by default, got %v", loaded.Tracing.Enabled)
	}

	t.Logf("✓ LoadTracingDefaults works correctly")
}

func TestRequiredRepoID(t *testing.T) {
	// Test: RequiredRepoID must exist when set
	cfg := &Config{
		RequiredRepoID: "nonexistent",
		Repos: []RepoConfig{
			{ID: "stable", PathPrefix: ""},
			{ID: "testing", PathPrefix: "/testing"},
		},
	}
	_, err := ResolveRepos(cfg)
	if err == nil || err.Error() != "required_repo_id \"nonexistent\" not found in repos list" {
		t.Fatalf("Expected error about missing required repo, got: %v", err)
	}

	// Test: RequiredRepoID succeeds when repo exists
	cfg.RequiredRepoID = "stable"
	_, err = ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("Expected no error with valid RequiredRepoID, got: %v", err)
	}

	// Test: RequiredRepoID empty means no requirement
	cfg.RequiredRepoID = ""
	_, err = ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("Expected no error with empty RequiredRepoID, got: %v", err)
	}

	// Test: RequiredRepoID not enforced when repos synthesized (empty list)
	cfg.RequiredRepoID = "main"
	cfg.Repos = []RepoConfig{} // Will synthesize default repo
	resolved, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("Expected no error with synthesized repo, got: %v", err)
	}
	if len(resolved) != 1 || resolved[0].ID != "default" {
		t.Fatalf("Expected synthesized default repo, got: %+v", resolved)
	}

	t.Logf("✓ RequiredRepoID validation works correctly")
}

func TestProtectDefaultSuite(t *testing.T) {
	// Test: ProtectDefaultSuite defaults to true when nil
	cfg := &Config{
		Repos: []RepoConfig{
			{ID: "stable", DefaultSuite: "stable"},
		},
	}
	resolved, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if !resolved[0].ProtectDefaultSuite {
		t.Fatalf("Expected ProtectDefaultSuite=true by default, got false")
	}

	// Test: ProtectDefaultSuite=false when explicitly disabled
	falseBool := false
	cfg.Repos[0].ProtectDefaultSuite = &falseBool
	resolved, err = ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if resolved[0].ProtectDefaultSuite {
		t.Fatalf("Expected ProtectDefaultSuite=false when disabled, got true")
	}

	// Test: ProtectDefaultSuite=true when explicitly enabled
	trueBool := true
	cfg.Repos[0].ProtectDefaultSuite = &trueBool
	resolved, err = ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if !resolved[0].ProtectDefaultSuite {
		t.Fatalf("Expected ProtectDefaultSuite=true when enabled, got true")
	}

	// Test: Synthesized repo defaults to protected
	cfg.Repos = []RepoConfig{}
	resolved, err = ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if !resolved[0].ProtectDefaultSuite {
		t.Fatalf("Expected synthesized repo to have ProtectDefaultSuite=true, got false")
	}

	t.Logf("✓ ProtectDefaultSuite resolution works correctly")
}
