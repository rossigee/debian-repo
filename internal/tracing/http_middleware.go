package tracing

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

// HTTPMiddleware wraps an HTTP handler with automatic span creation and tracing
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip tracing for health and metrics endpoints
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract trace context from incoming HTTP headers (W3C Trace Context)
		propagator := otel.GetTextMapPropagator()
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		ctx, span := StartSpan(ctx, r.Method+" "+r.URL.Path,
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("net.peer.ip", r.RemoteAddr),
		)
		defer span.End()

		// Get correlation ID from header or X-Trace-ID
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = span.SpanContext().TraceID().String()
		}
		span.SetAttributes(attribute.String("correlation_id", traceID))

		// Add auth info to span
		if user, _, ok := r.BasicAuth(); ok {
			span.SetAttributes(attribute.String("http.auth_user", user))
		}
		if auth := r.Header.Get("Authorization"); auth != "" && len(auth) > 7 {
			span.SetAttributes(attribute.String("http.auth_type", auth[:6]))
		}

		// Wrap response writer to capture status
		wrapped := &statusCaptureWriter{ResponseWriter: w, statusCode: http.StatusOK}

		start := time.Now()
		next.ServeHTTP(wrapped, r.WithContext(ctx))
		duration := time.Since(start)

		// Record response details
		span.SetAttributes(
			attribute.Int("http.status_code", wrapped.statusCode),
			attribute.Int64("http.response_bytes", wrapped.bytesWritten),
			attribute.Int64("duration_ms", duration.Milliseconds()),
		)

		// Set status based on HTTP code
		if wrapped.statusCode >= 400 {
			span.SetStatus(codes.Error, http.StatusText(wrapped.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	})
}

// statusCaptureWriter wraps ResponseWriter to capture status code
type statusCaptureWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (w *statusCaptureWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCaptureWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}
