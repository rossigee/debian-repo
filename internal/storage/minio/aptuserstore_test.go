package minio

import (
	"context"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// Regression test: constructing the apt-user store from the unscoped base
// client (as cmd/debian-repo/main.go once did) produced
// "failed to get apt users object: Bucket name cannot be empty" on every
// reload. The store must fail fast with a clear error instead.
func TestAptUserStoreRejectsUnscopedClient(t *testing.T) {
	base := newTestBaseClient(t)
	store := NewAptUserStore(base, "_meta/apt-users.json.gz")

	if _, err := store.GetUsers(context.Background()); err == nil {
		t.Fatal("expected GetUsers to fail with an unscoped client")
	} else if !strings.Contains(err.Error(), "no bucket set") {
		t.Fatalf("expected bucket error, got: %v", err)
	}

	empty := &model.AptUserStoreV1{FormatVersion: 1, Users: []model.AptUser{}}
	if _, err := store.PutUsers(context.Background(), empty); err == nil {
		t.Fatal("expected PutUsers to fail with an unscoped client")
	} else if !strings.Contains(err.Error(), "no bucket set") {
		t.Fatalf("expected bucket error, got: %v", err)
	}
}
