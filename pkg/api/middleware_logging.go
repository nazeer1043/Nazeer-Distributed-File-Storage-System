package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const RequestIDKey contextKey = "requestID"

// GetRequestID extracts the request ID from the context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

// sanitizeURL strips secrets, tokens, and passwords from request queries before logging.
func sanitizeURL(u *url.URL) string {
	if u.RawQuery == "" {
		return u.Path
	}
	values := u.Query()
	for key := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "pass") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "key") {
			values.Set(key, "[REDACTED]")
		}
	}
	return u.Path + "?" + values.Encode()
}

// StructuredLoggingMiddleware assigns a unique Request ID to each request and records
// structured slog telemetry without exposing credentials, secrets, or tokens.
func StructuredLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}

		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
		r = r.WithContext(ctx)

		sw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(sw, r)

		duration := time.Since(start)
		sanitizedURI := sanitizeURL(r.URL)

		slog.Info("HTTP Request",
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("uri", sanitizedURI),
			slog.Int("status", sw.statusCode),
			slog.Int64("bytes", sw.bytesWritten),
			slog.Duration("duration", duration),
			slog.String("remote_ip", r.RemoteAddr),
		)
	})
}
