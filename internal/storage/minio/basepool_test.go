package minio

import "testing"

// Equal connection settings share one base client; a different endpoint gets
// its own.
func TestBaseClientPoolSharesByConnection(t *testing.T) {
	pool := NewBaseClientPool()
	a := BaseClientConfig{Endpoint: "minio-a.example:9000", AccessKey: "ka", SecretKey: "sa"}
	b := BaseClientConfig{Endpoint: "minio-b.example:9000", AccessKey: "kb", SecretKey: "sb"}

	first, created, err := pool.Get(a)
	if err != nil || !created {
		t.Fatalf("first Get(a): created=%v err=%v", created, err)
	}
	again, created, err := pool.Get(a)
	if err != nil || created {
		t.Fatalf("second Get(a): created=%v err=%v, want shared", created, err)
	}
	if again != first {
		t.Error("equal configurations must share one client")
	}

	other, created, err := pool.Get(b)
	if err != nil || !created {
		t.Fatalf("Get(b): created=%v err=%v", created, err)
	}
	if other == first {
		t.Error("a different endpoint must get its own client, not the first repository's")
	}
}
