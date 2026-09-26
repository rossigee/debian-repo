package config

import (
	"testing"
)

func TestResolvedRepoSnapshotSettings(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.SnapshotKey = "_custom/snap.json.gz"
	cfg.Storage.SnapshotHistoryKeep = 7
	cfg.Storage.StagingPrefix = "_custom-staging/"
	cfg.Auth.Metrics.Token = "m"

	resolved, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if resolved[0].SnapshotKey != "_custom/snap.json.gz" {
		t.Errorf("SnapshotKey = %q", resolved[0].SnapshotKey)
	}
	if resolved[0].SnapshotHistoryKeep != 7 {
		t.Errorf("SnapshotHistoryKeep = %d", resolved[0].SnapshotHistoryKeep)
	}
	if resolved[0].StagingPrefix != "_custom-staging/" {
		t.Errorf("StagingPrefix = %q", resolved[0].StagingPrefix)
	}
}

func TestResolvedRepoSnapshotDefaults(t *testing.T) {
	// Hand-built config without Load defaults must still resolve safe values.
	cfg := &Config{}
	cfg.Auth.Metrics.Token = "m"

	resolved, err := ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("ResolveRepos failed: %v", err)
	}
	if resolved[0].SnapshotKey != "_meta/index-snapshot.json.gz" {
		t.Errorf("SnapshotKey = %q", resolved[0].SnapshotKey)
	}
	if resolved[0].SnapshotHistoryKeep != 20 {
		t.Errorf("SnapshotHistoryKeep = %d", resolved[0].SnapshotHistoryKeep)
	}
	if resolved[0].StagingPrefix != "_staging/" {
		t.Errorf("StagingPrefix = %q", resolved[0].StagingPrefix)
	}
}
