package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// RequestTracer wraps an HTTP handler with request/response logging
type RequestTracer struct {
	handler http.Handler
}

// NewRequestTracer creates a new request tracing middleware
func NewRequestTracer(handler http.Handler) *RequestTracer {
	return &RequestTracer{handler: handler}
}

// ServeHTTP logs incoming request and response (skip for health/metrics endpoints)
func (rt *RequestTracer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Skip logging for health, readiness, and metrics endpoints (low-value noise from monitoring)
	skipLogging := r.URL.Path == "/healthz" || r.URL.Path == "/readyz" ||
		r.URL.Path == "/health" || r.URL.Path == "/readiness" ||
		r.URL.Path == "/metrics"

	correlationID := r.Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = generateRequestID()
	}

	// Create logger with correlation ID for this request
	reqLogger := defaultLogger.With(
		slog.String("correlation_id", correlationID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("remote_addr", r.RemoteAddr),
	)

	// Log incoming request (unless skip logging is enabled)
	if !skipLogging {
		if user, _, ok := r.BasicAuth(); ok {
			reqLogger = reqLogger.With(slog.String("user", user))
		}
		if auth := r.Header.Get("Authorization"); auth != "" && len(auth) > 7 {
			reqLogger = reqLogger.With(slog.String("auth_type", auth[:6]))
		}

		reqLogger.Debug("request received")
	}

	// Wrap response writer to capture status and size
	wrapped := &wrappedResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

	// Serve request
	rt.handler.ServeHTTP(wrapped, r)

	// Log response (unless skip logging is enabled)
	if !skipLogging {
		duration := time.Since(start).Milliseconds()
		fields := []slog.Attr{
			slog.Int("status", wrapped.statusCode),
			slog.Int64("duration_ms", duration),
			slog.Int64("bytes_written", wrapped.bytesWritten),
		}

		level := slog.LevelInfo
		if wrapped.statusCode >= 400 {
			level = slog.LevelWarn
		}

		// Record the resolved CI token identity on the completion line. The
		// auth middleware sets X-CI-Identity from the token it matched, so this
		// is server-derived and cannot be spoofed by the caller. Without it a
		// CI-authenticated request is indistinguishable from any other bearer
		// caller in the logs.
		if identity := r.Header.Get("X-CI-Identity"); identity != "" {
			fields = append(fields, slog.String("identity", identity))
		}

		reqLogger.LogAttrs(context.Background(), level, "request completed", fields...)
	}
}

// wrappedResponseWriter captures status code and bytes written
type wrappedResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (w *wrappedResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *wrappedResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

// generateRequestID creates a simple request ID
func generateRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}
