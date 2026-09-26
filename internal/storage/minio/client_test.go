package minio

import (
	"context"
	"testing"
)

func newTestBaseClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewBaseClient(BaseClientConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "test",
		SecretKey: "testtest",
	})
	if err != nil {
		t.Fatalf("failed to create base client: %v", err)
	}
	return c
}

// A base client is unscoped: no bucket is bound until WithBucketAndPrefix is used.
func TestNewBaseClientIsUnscoped(t *testing.T) {
	c := newTestBaseClient(t)
	if c.Bucket() != "" {
		t.Fatalf("expected empty bucket on base client, got %q", c.Bucket())
	}
}

// Scoping a client must bind the bucket and normalize the prefix.
func TestWithBucketAndPrefixScopesBucket(t *testing.T) {
	base := newTestBaseClient(t)

	scoped := base.WithBucketAndPrefix("debs-myorgname", "")
	if scoped.Bucket() != "debs-myorgname" {
		t.Fatalf("expected bucket %q, got %q", "debs-myorgname", scoped.Bucket())
	}
	if base.Bucket() != "" {
		t.Fatalf("scoping must not mutate the base client, got bucket %q", base.Bucket())
	}

	prefixed := base.WithBucketAndPrefix("debs-myorgname", "tenant-a")
	if got := prefixed.key("pool/x.deb"); got != "tenant-a/pool/x.deb" {
		t.Fatalf("expected prefixed key, got %q", got)
	}
}

// EnsureBucket on an unscoped client must fail fast without any network call.
func TestEnsureBucketRejectsUnscopedClient(t *testing.T) {
	c := newTestBaseClient(t)
	if err := c.EnsureBucket(context.Background()); err == nil {
		t.Fatal("expected error from EnsureBucket with no bucket set")
	}
}
