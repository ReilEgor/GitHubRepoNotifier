// Package probe serves the service endpoints that report liveness and the running build.
package probe

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewHandler returns a handler that serves GET /health and GET /version.
// The version argument is the git commit the binary was built from.
func NewHandler(version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, map[string]string{"version": version})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, r *http.Request, body map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.ErrorContext(r.Context(), "write response", slog.String("error", err.Error()))
	}
}
