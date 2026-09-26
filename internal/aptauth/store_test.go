package aptauth

import (
	"context"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"golang.org/x/crypto/bcrypt"
)

func hashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	return string(hash)
}

func TestVerify(t *testing.T) {
	s := NewStore()
	s.Load([]model.AptUser{
		{Username: "alice", PasswordHash: hashPassword(t, "s3cret"), Disabled: false},
		{Username: "bob", PasswordHash: hashPassword(t, "s3cret"), Disabled: true},
	})

	if !s.Verify("alice", "s3cret") {
		t.Fatal("expected valid credentials to verify")
	}
	if s.Verify("alice", "wrong") {
		t.Fatal("expected wrong password to fail verification")
	}
	if s.Verify("unknown", "s3cret") {
		t.Fatal("expected unknown user to fail verification")
	}
	if s.Verify("bob", "s3cret") {
		t.Fatal("expected disabled user to fail verification")
	}
}

// ReloadFrom must surface backend errors (wrapped) instead of silently
// keeping stale credentials.
func TestReloadFromPropagatesBackendError(t *testing.T) {
	base, err := minio.NewBaseClient(minio.BaseClientConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "test",
		SecretKey: "testtest",
	})
	if err != nil {
		t.Fatalf("failed to create base client: %v", err)
	}
	// Deliberately unscoped: no bucket bound.
	backend := minio.NewAptUserStore(base, "_meta/apt-users.json.gz")

	s := NewStore()
	err = s.ReloadFrom(context.Background(), backend)
	if err == nil {
		t.Fatal("expected ReloadFrom to fail with an unscoped backend client")
	}
	if !strings.Contains(err.Error(), "failed to fetch apt users from MinIO") {
		t.Fatalf("expected wrapped fetch error, got: %v", err)
	}
}

// A successful reload replaces the in-memory user set.
func TestReloadFromReplacesUsers(t *testing.T) {
	s := NewStore()
	s.Load([]model.AptUser{{Username: "stale", PasswordHash: hashPassword(t, "old")}})

	s.Load([]model.AptUser{{Username: "fresh", PasswordHash: hashPassword(t, "new")}})
	if s.Verify("stale", "old") {
		t.Fatal("expected stale user to be gone after reload")
	}
	if !s.Verify("fresh", "new") {
		t.Fatal("expected fresh user to verify after reload")
	}
}
