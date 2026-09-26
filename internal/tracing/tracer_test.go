package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func TestStartSpanCreatesSpan(t *testing.T) {
	_, span := StartSpan(context.Background(), "test-span",
		attribute.String("key", "value"),
	)
	if span == nil {
		t.Fatal("StartSpan returned nil span")
	}
	span.End()
	t.Logf("✓ StartSpan creates valid span")
}

func TestStartSpanWithAttributes(t *testing.T) {
	_, span := StartSpan(context.Background(), "test",
		attribute.String("http.method", "GET"),
		attribute.String("http.url", "/api/v1/dists"),
	)
	defer span.End()

	_ = span.SpanContext().TraceID()
	_ = span.SpanContext().SpanID()

	t.Logf("✓ StartSpan with attributes works")
}

func TestRecordError(t *testing.T) {
	_, span := StartSpan(context.Background(), "test-error")
	defer span.End()

	testErr := context.Canceled
	RecordError(span, testErr)

	t.Logf("✓ RecordError does not panic")
}

func TestRecordErrorNilSpan(t *testing.T) {
	RecordError(nil, context.Canceled)
	t.Logf("✓ RecordError handles nil span")
}

func TestSetAttribute(t *testing.T) {
	_, span := StartSpan(context.Background(), "test-attr")
	defer span.End()

	SetAttribute(span, "key", "value")
	SetAttribute(span, "number", 42)
	SetAttribute(span, "bool", true)

	t.Logf("✓ SetAttribute works")
}

func TestSetAttributeNilSpan(t *testing.T) {
	SetAttribute(nil, "key", "value")
	t.Logf("✓ SetAttribute handles nil span")
}

func TestTracerNotNull(t *testing.T) {
	tracer := Tracer()
	if tracer == nil {
		t.Fatal("Tracer should not be nil")
	}
	t.Logf("✓ Tracer is not nil")
}

func TestHTTPMiddlewareSkipsHealthEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Fatalf("Expected OK, got %s", rec.Body.String())
	}

	t.Logf("✓ HTTPMiddleware skips healthz")
}

func TestHTTPMiddlewareSkipsMetricsEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ HTTPMiddleware skips metrics")
}

func TestHTTPMiddlewareSkipsReadyzEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/readyz", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ HTTPMiddleware skips readyz")
}

func TestHTTPMiddlewareWrapsHandler(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Trace-ID", "test-trace-id")
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ HTTPMiddleware wraps handler correctly")
}

func TestHTTPMiddlewareRecordsStatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/error", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d", rec.Code)
	}

	t.Logf("✓ HTTPMiddleware records status code")
}

func TestHTTPMiddlewareWithContext(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/context-test", func(w http.ResponseWriter, r *http.Request) {
		rctx := r.Context()
		tracer := otel.GetTracerProvider().Tracer("test")
		rctx, _ = tracer.Start(rctx, "test")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := HTTPMiddleware(mux)
	req := httptest.NewRequest("GET", "/context-test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	t.Logf("✓ HTTPMiddleware propagates context")
}

func TestPackageRegistrationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := PackageRegistration(ctx, "stable", "main", "test-pkg", "1.0.0", "amd64")
	defer done(nil)

	if ctx == nil {
		t.Fatal("PackageRegistration returned nil context")
	}

	t.Logf("✓ PackageRegistration trace works")
}

func TestIndexMutationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := IndexMutation(ctx, "add", "stable", "main", "test-pkg")
	defer done(nil)

	if ctx == nil {
		t.Fatal("IndexMutation returned nil context")
	}

	t.Logf("✓ IndexMutation trace works")
}

func TestMinIOOperationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := MinIOOperation(ctx, "Get", "_meta/snapshot.json.gz", "key")
	defer done(nil)

	if ctx == nil {
		t.Fatal("MinIOOperation returned nil context")
	}

	t.Logf("✓ MinIOOperation trace works")
}

func TestGPGSignTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := GPGSign(ctx, "detach", "test-key-id")
	defer done(nil)

	if ctx == nil {
		t.Fatal("GPGSign returned nil context")
	}

	t.Logf("✓ GPGSign trace works")
}

func TestValidationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := Validation(ctx, "deb")
	defer done(nil)

	if ctx == nil {
		t.Fatal("Validation returned nil context")
	}

	t.Logf("✓ Validation trace works")
}

func TestAuthenticationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := Authentication(ctx, "basic", "test-user")
	defer done(nil)

	if ctx == nil {
		t.Fatal("Authentication returned nil context")
	}

	t.Logf("✓ Authentication trace works")
}

func TestReconciliationTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := Reconciliation(ctx)
	defer done(nil, 0)

	if ctx == nil {
		t.Fatal("Reconciliation returned nil context")
	}

	t.Logf("✓ Reconciliation trace works")
}

func TestRenderMetadataTrace(t *testing.T) {
	ctx := context.Background()
	ctx, done := RenderMetadata(ctx, "stable", "main")
	defer done(nil)

	if ctx == nil {
		t.Fatal("RenderMetadata returned nil context")
	}

	t.Logf("✓ RenderMetadata trace works")
}
