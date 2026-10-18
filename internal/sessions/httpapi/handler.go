// Package httpapi exposes the Sessions HTTP transport.
package httpapi

import "net/http"

// NewHandler returns the foundation router. Health indicates process liveness,
// not database readiness or availability of the future messaging API.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		// A write failure means the client disconnected; no sensitive request
		// data is needed in logs and there is no useful response to retry here.
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	return mux
}
