package shedder

import (
	"fmt"
	"net/http"
)

// ReadyHandler returns an http.Handler that implements a Kubernetes
// readiness probe endpoint.
//
// Returns:
//   - 200 OK when in-flight requests <= HardLimit
//   - 503 Service Unavailable when in-flight requests > HardLimit
func (s *Shedder) ReadyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inflight := s.Inflight()

		if inflight > s.hardLimit {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "not ready: inflight=%d, hardLimit=%d", inflight, s.hardLimit)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ready: inflight=%d, hardLimit=%d", inflight, s.hardLimit)
	})
}

// ReadyHandlerFunc is a convenience function that returns the readiness
// handler as an http.HandlerFunc.
func (s *Shedder) ReadyHandlerFunc() http.HandlerFunc {
	return s.ReadyHandler().ServeHTTP
}

// HealthHandler returns a simple health check handler that always returns 200 OK.
// This is suitable for Kubernetes liveness probes.
func HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
}
