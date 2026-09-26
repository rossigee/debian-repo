package apiserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsAuthMiddleware(t *testing.T) {
	next := MetricsAuthMiddleware("s3cret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("metrics"))
	}))

	// No credentials -> 401.
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	// Wrong token -> 401.
	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}

	// Correct token -> passthrough.
	req = httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	next.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "metrics" {
		t.Fatalf("expected passthrough, got %d %q", rec.Code, rec.Body.String())
	}
}
