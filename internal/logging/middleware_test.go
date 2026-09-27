package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLogs points the package default logger at a buffer and returns a
// function that yields the decoded log records. Init writes to os.Stdout, so
// the logger is wired up directly here instead.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := defaultLogger
	defaultLogger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { defaultLogger = prev })

	return func() []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("unmarshal log line %q: %v", line, err)
			}
			out = append(out, rec)
		}
		return out
	}
}

func completed(t *testing.T, records func() []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, rec := range records() {
		if rec["msg"] == msg {
			return rec
		}
	}
	t.Fatalf("no %q record in %v", msg, records())
	return nil
}

// A CI-authenticated request must be attributable. The auth middleware resolves
// the bearer token to an identity and writes it to X-CI-Identity, overwriting
// anything the caller sent, so the tracer can record it on the completion log.
func TestRequestTracerRecordsCIIdentity(t *testing.T) {
	records := captureLogs(t)

	rt := NewRequestTracer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// stands in for wrapAuth resolving the token
		r.Header.Set("X-CI-Identity", "repo-admin")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Job not found\n"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/admin/reconcile/recon-123", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.RemoteAddr = "100.64.0.33:37050"
	rt.ServeHTTP(httptest.NewRecorder(), req)

	rec := completed(t, records, "request completed")
	if got := rec["identity"]; got != "repo-admin" {
		t.Errorf("identity = %v, want repo-admin", got)
	}
	if rec["remote_addr"] != "100.64.0.33:37050" {
		t.Errorf("remote_addr = %v, want 100.64.0.33:37050", rec["remote_addr"])
	}
	if rec["status"] != float64(http.StatusNotFound) {
		t.Errorf("status = %v, want 404", rec["status"])
	}
}

// Requests with no CI identity, such as apt BasicAuth clients, must not gain an
// empty identity field.
func TestRequestTracerOmitsIdentityWhenUnset(t *testing.T) {
	records := captureLogs(t)

	rt := NewRequestTracer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rt.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dists/stable/InRelease", nil))

	rec := completed(t, records, "request completed")
	if _, ok := rec["identity"]; ok {
		t.Errorf("identity should be absent when no CI token was used, got %v", rec["identity"])
	}
}

// Health and metrics endpoints stay unlogged regardless.
func TestRequestTracerSkipsHealthEndpoints(t *testing.T) {
	records := captureLogs(t)

	rt := NewRequestTracer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, path := range []string{"/health", "/healthz", "/readyz", "/readiness", "/metrics"} {
		rt.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if recs := records(); len(recs) != 0 {
		t.Errorf("expected no log records for health/metrics paths, got %v", recs)
	}
}
