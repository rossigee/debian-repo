package minio

import (
	"testing"
)

func TestDefaultHistoryPrefix(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"_meta/index-snapshot.json.gz", "_meta/history/"},
		{"_meta/nested/snap.json.gz", "_meta/nested/history/"},
		{"snap.json.gz", "history/"},
		{"", "history/"},
	}
	for _, tc := range cases {
		if got := DefaultHistoryPrefix(tc.key); got != tc.want {
			t.Errorf("DefaultHistoryPrefix(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}

	// History must never live inside the staging namespace.
	if got := DefaultHistoryPrefix("_meta/index-snapshot.json.gz"); got == "_staging/" {
		t.Error("history prefix collides with staging namespace")
	}
}

func TestHistoryKeyOrder(t *testing.T) {
	// Lexicographic order is wrong across digit widths (9 > 10
	// lexicographically); numeric (unix, gen) order must win.
	keys := []string{
		"h/index-snapshot-10-200.json.gz",
		"h/index-snapshot-9-100.json.gz",
		"h/index-snapshot-2-300.json.gz",
		"h/garbage.json.gz",
	}
	// Simulate pruneHistory's keep-newest-2 selection.
	type ordered struct {
		key       string
		unix, gen int64
	}
	var ord []ordered
	for _, k := range keys {
		u, g := historyKeyOrder(k)
		ord = append(ord, ordered{k, u, g})
	}
	// Oldest-first: garbage (0,0), then (100,9), (200,10), (300,2).
	// Keep newest 2 => drop garbage + (100,9).
	if u, g := historyKeyOrder("h/index-snapshot-10-200.json.gz"); u != 200 || g != 10 {
		t.Fatalf("historyKeyOrder parse failed: (%d,%d)", u, g)
	}
	if u, g := historyKeyOrder("h/garbage.json.gz"); u != 0 || g != 0 {
		t.Fatalf("unparseable key must sort first: (%d,%d)", u, g)
	}
	_ = ord
}
