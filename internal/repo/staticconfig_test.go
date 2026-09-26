package repo

import (
	"context"
	"errors"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
)

func TestStaticConfigFile_ListRepos_MatchesResolveRepos(t *testing.T) {
	// Build a minimal test config (empty repos list will synthesize a default)
	cfg := &config.Config{}

	// Get repos via direct ResolveRepos
	directResolved, err := config.ResolveRepos(cfg)
	if err != nil {
		t.Fatalf("failed to resolve repos directly: %v", err)
	}

	// Get repos via StaticConfigFile store
	store := NewStaticConfigFile(cfg)
	storeRecords, err := store.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("failed to list repos from store: %v", err)
	}

	if len(storeRecords) != len(directResolved) {
		t.Errorf("repo count mismatch: direct=%d, store=%d", len(directResolved), len(storeRecords))
		return
	}

	// Round-trip convert and compare key fields
	for i, rec := range storeRecords {
		roundTrip := rec.ToResolvedRepo()
		direct := directResolved[i]

		// Verify round-trip conversion matches original
		if roundTrip.ID != direct.ID {
			t.Errorf("[%d] ID mismatch: %q vs %q", i, roundTrip.ID, direct.ID)
		}
		if roundTrip.Bucket != direct.Bucket {
			t.Errorf("[%d] Bucket mismatch: %q vs %q", i, roundTrip.Bucket, direct.Bucket)
		}
		if roundTrip.DefaultSuite != direct.DefaultSuite {
			t.Errorf("[%d] DefaultSuite mismatch: %q vs %q", i, roundTrip.DefaultSuite, direct.DefaultSuite)
		}
		if roundTrip.FeedEnabled != direct.FeedEnabled {
			t.Errorf("[%d] FeedEnabled mismatch: %v vs %v", i, roundTrip.FeedEnabled, direct.FeedEnabled)
		}
		if roundTrip.FeedMaxItems != direct.FeedMaxItems {
			t.Errorf("[%d] FeedMaxItems mismatch: %d vs %d", i, roundTrip.FeedMaxItems, direct.FeedMaxItems)
		}
	}
}

func TestStaticConfigFile_WriteMethodsReturnErrReadOnlyStore(t *testing.T) {
	cfg := &config.Config{}
	store := NewStaticConfigFile(cfg)

	tests := []struct {
		name   string
		testFn func() error
	}{
		{
			name:   "CreateRepo",
			testFn: func() error { return store.CreateRepo(context.Background(), RepoRecord{}) },
		},
		{
			name:   "UpdateRepo",
			testFn: func() error { return store.UpdateRepo(context.Background(), RepoRecord{}) },
		},
		{
			name:   "DeleteRepo",
			testFn: func() error { return store.DeleteRepo(context.Background(), "test") },
		},
		{
			name:   "CreateSuite",
			testFn: func() error { return store.CreateSuite(context.Background(), SuiteRecord{}) },
		},
		{
			name:   "UpdateSuite",
			testFn: func() error { return store.UpdateSuite(context.Background(), SuiteRecord{}) },
		},
		{
			name:   "DeleteSuite",
			testFn: func() error { return store.DeleteSuite(context.Background(), "repo", "suite") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFn()
			if !errors.Is(err, ErrReadOnlyStore) {
				t.Errorf("expected ErrReadOnlyStore, got %v", err)
			}
		})
	}
}

func TestStaticConfigFile_ListSuites_EmptyToday(t *testing.T) {
	cfg := &config.Config{}
	store := NewStaticConfigFile(cfg)

	// Today, suites cannot be declared in YAML, so ListSuites always returns empty
	suites, err := store.ListSuites(context.Background(), "any-repo")
	if err != nil {
		t.Errorf("ListSuites should not error, got %v", err)
	}
	if len(suites) != 0 {
		t.Errorf("ListSuites should return empty list, got %d suites", len(suites))
	}

	// GetSuite should return not-found error
	_, err = store.GetSuite(context.Background(), "repo", "suite")
	if err == nil {
		t.Errorf("GetSuite should return error for non-existent suite")
	}
}
