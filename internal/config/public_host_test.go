package config

import "testing"

// A repository with no vhost takes its public host from configuration, and the
// storage endpoint is never used as the public host.
func TestResolveRepoPublicHost(t *testing.T) {
	cfg := &Config{
		PublicHost: "debs.example.com",
		Repos: []RepoConfig{
			{ID: "testing", PathPrefix: "/testing"},
			{ID: "override", PathPrefix: "/override", PublicHost: "other.example.com"},
		},
	}
	cfg.Storage.MinIO.Endpoint = "minio.internal:9000"

	repos, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos: %v", err)
	}
	byID := make(map[string]ResolvedRepo, len(repos))
	for _, rr := range repos {
		byID[rr.ID] = rr
	}

	if got := byID["testing"].PublicHost; got != "debs.example.com" {
		t.Errorf("vhost-less repo PublicHost = %q, want global %q", got, "debs.example.com")
	}
	if got := byID["override"].PublicHost; got != "other.example.com" {
		t.Errorf("per-repo PublicHost = %q, want %q", got, "other.example.com")
	}
	for id, rr := range byID {
		if rr.PublicHost == cfg.Storage.MinIO.Endpoint {
			t.Errorf("repo %s: PublicHost is the storage endpoint", id)
		}
	}
}

// The synthesized default repository also takes the global public host.
func TestSynthesizedRepoPublicHost(t *testing.T) {
	cfg := &Config{PublicHost: "debs.example.com"}
	cfg.Storage.MinIO.Endpoint = "minio.internal:9000"

	repos, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one synthesized repo, got %d", len(repos))
	}
	if got := repos[0].PublicHost; got != "debs.example.com" {
		t.Errorf("synthesized PublicHost = %q, want %q", got, "debs.example.com")
	}
}
