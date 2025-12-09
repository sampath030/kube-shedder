package shedder

import (
	"net/http"
)

// Middleware returns an http.Handler that wraps the given handler with
// load shedding logic.
//
// The middleware:
//  1. Increments the in-flight counter
//  2. Checks if HardLimit is exceeded - if so, returns 503 immediately
//  3. If SoftLimit is exceeded and ShedDecider returns true, returns 503
//  4. Otherwise, calls the wrapped handler
//  5. Decrements the in-flight counter when done (even on panic)
func (s *Shedder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Increment before checking limits
		current := s.increment()

		// Always decrement when we're done (handles panics too)
		defer s.decrement()

		// Check hard limit
		if current > s.hardLimit {
			s.shed(w, r, ShedReasonHardLimit)
			return
		}

		// Check soft limit
		if s.softLimit > 0 && current > s.softLimit {
			if s.shedDecider != nil && s.shedDecider(r) {
				s.shed(w, r, ShedReasonSoftLimit)
				return
			}
		}

		// Serve the request
		next.ServeHTTP(w, r)
	})
}

// MiddlewareFunc is a convenience wrapper that returns a function
// suitable for use with middleware chains that expect func(http.Handler) http.Handler.
func (s *Shedder) MiddlewareFunc() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.Middleware(next)
	}
}

// shed writes a 503 response and invokes the OnShed callback if configured.
func (s *Shedder) shed(w http.ResponseWriter, r *http.Request, reason ShedReason) {
	if s.onShed != nil {
		s.onShed(r, reason)
	}

	w.Header().Set("Retry-After", "1")
	w.Header().Set("X-Shed-Reason", reason.String())
	http.Error(w, "Service Unavailable: load shedding active", http.StatusServiceUnavailable)
}
