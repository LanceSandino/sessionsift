// This module records one privacy-safe audit event for every origin request to
// the hosted MCP endpoint. It intentionally never inspects or records headers,
// query parameters, request bodies, tool arguments, or tool results.
package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	count, err := w.ResponseWriter.Write(value)
	w.bytes += count
	return count, err
}

func (w *statusRecorder) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (h *Handler) loggedMCP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	recorder := &statusRecorder{ResponseWriter: w}
	principal, verified := h.principal(recorder, r)
	if verified {
		h.mcp(recorder, r, principal)
	}
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}

	attributes := []any{
		"event", "mcp_access",
		"request_id", requestID(r.Header.Get("Cf-Ray")),
		"method", r.Method,
		"status", recorder.status,
		"duration_ms", time.Since(started).Milliseconds(),
		"response_bytes", recorder.bytes,
		"identity_verified", verified,
	}
	if verified {
		attributes = append(attributes, "principal_hash", principalHash(principal.Email))
	}
	h.logger.Log(r.Context(), slog.LevelInfo, "hosted MCP request", attributes...)
}

func requestID(ray string) string {
	ray = strings.TrimSpace(ray)
	if ray != "" && len(ray) <= 128 {
		valid := true
		for _, character := range ray {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || character == '-') {
				valid = false
				break
			}
		}
		if valid {
			return ray
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err == nil {
		return hex.EncodeToString(random)
	}
	return "unavailable"
}

func principalHash(email string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(digest[:12])
}
