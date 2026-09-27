package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// Before hydration completes, handlers that serve rendered metadata must answer
// 503 with Retry-After rather than serving a half-rendered index or a connection
// error. The process is already listening at this point, so apt needs an
// explicit retryable signal.
func TestMetadataRoutesUnavailableUntilHydrated(t *testing.T) {
	s := newTestServer(t)
	rp := s.registry.All()[0]

	if rp.Hydrated() {
		t.Fatal("test repo should start unhydrated")
	}

	mux := http.NewServeMux()
	s.Register(mux)

	for _, path := range []string{
		"/dists/stable/InRelease",
		"/dists/stable/main/binary-amd64/Packages",
		"/index.json",
		"/install.html",
		"/gpg-key.html",
		"/packages/",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503 while hydrating", path, rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got == "" {
			t.Errorf("%s: missing Retry-After header while hydrating", path)
		}
	}
}

// Liveness must stay 200 throughout: it is what tells the process is up, and a
// load balancer should not pull the instance just because metadata is loading.
func TestHealthzStaysUpWhileHydrating(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.Register(mux)

	for _, path := range []string{"/healthz", "/health"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 while hydrating", path, rec.Code)
		}
	}
}

// Readiness reports 503 with Retry-After until the repo is marked hydrated, then
// 200. It must not be inferred from the index contents: a repository with no
// distributions is legitimately hydrated and ready.
func TestReadyzTracksHydrationNotIndexContents(t *testing.T) {
	s := newTestServer(t)
	rp := s.registry.All()[0]
	rp.IndexMgr.SetIndex(model.NewIndex())
	mux := http.NewServeMux()
	s.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("before hydration: status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("before hydration: missing Retry-After")
	}

	rp.MarkHydrated()

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("after hydration: status = %d, want 200 (empty index is still ready)", rec.Code)
	}
}

// Metadata routes must get past the gate once hydrated, so a slow or failed
// hydration cannot wedge the service permanently. The handler may still fail for
// its own reasons (the fixture has no rendered metadata), so assert specifically
// that the gate is no longer the thing answering.
func TestMetadataRoutesServeAfterHydration(t *testing.T) {
	s := newTestServer(t)
	s.registry.All()[0].MarkHydrated()

	mux := http.NewServeMux()
	s.Register(mux)

	for _, path := range []string{"/dists/stable/InRelease", "/index.json", "/install.html"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

		if body := rec.Body.String(); strings.Contains(body, "still loading") {
			t.Errorf("%s: gate still rejecting after hydration: %q", path, body)
		}
	}
}

// The public key does not depend on the rendered index: the signer is loaded
// before the listener starts, so it must stay available while hydrating.
func TestPublicKeyAvailableWhileHydrating(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/pubkey.gpg", nil))
	if rec.Code == http.StatusServiceUnavailable {
		t.Error("/pubkey.gpg returned 503 while hydrating; it does not need the index")
	}
}
