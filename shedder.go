package shedder

import (
	"net/http"
	"sync/atomic"
)

// ShedDecider is a callback function that determines whether a request
// should be shed when in soft overload state.
// It receives the incoming request and returns true if the request should be rejected.
type ShedDecider func(r *http.Request) bool

// Config holds the configuration for a Shedder instance.
type Config struct {
	// HardLimit is the maximum number of in-flight requests before the
	// readiness endpoint returns 503. This is required and must be > 0.
	HardLimit int64

	// SoftLimit is the threshold for soft overload behavior.
	// If SoftLimit > 0 and inflight > SoftLimit (but <= HardLimit),
	// the ShedDecider is consulted to determine if requests should be fast-failed.
	// If SoftLimit is 0 or negative, soft overload behavior is disabled.
	SoftLimit int64

	// ShedDecider is called when in soft overload state to determine
	// whether to shed a request. If nil and SoftLimit > 0, soft shedding
	// is effectively disabled unless ShedHeader is set.
	ShedDecider ShedDecider

	// ShedHeader specifies a header name and value for automatic shedding.
	// When in soft overload state, requests with this header matching will be shed.
	// This is an alternative to ShedDecider for simple priority-based shedding.
	// If both ShedDecider and ShedHeader are set, ShedDecider takes precedence.
	ShedHeader *HeaderMatcher

	// OnShed is an optional callback invoked when a request is shed.
	// Useful for logging or metrics (without adding direct dependencies).
	OnShed func(r *http.Request, reason ShedReason)
}

// HeaderMatcher defines a header name and value to match for shedding.
type HeaderMatcher struct {
	Name  string // Header name, e.g., "X-Priority"
	Value string // Header value to match, e.g., "low"
}

// ShedReason indicates why a request was shed.
type ShedReason int

const (
	// ShedReasonHardLimit indicates the request was shed because
	// in-flight requests exceeded HardLimit.
	ShedReasonHardLimit ShedReason = iota

	// ShedReasonSoftLimit indicates the request was shed because
	// in-flight requests exceeded SoftLimit and the ShedDecider
	// (or header match) determined it should be shed.
	ShedReasonSoftLimit
)

func (r ShedReason) String() string {
	switch r {
	case ShedReasonHardLimit:
		return "hard_limit"
	case ShedReasonSoftLimit:
		return "soft_limit"
	default:
		return "unknown"
	}
}

// Shedder tracks in-flight requests and provides load shedding capabilities.
type Shedder struct {
	hardLimit   int64
	softLimit   int64
	inflight    atomic.Int64
	shedDecider ShedDecider
	onShed      func(r *http.Request, reason ShedReason)
}

// New creates a new Shedder with the given configuration.
// It panics if HardLimit is <= 0.
func New(cfg Config) *Shedder {
	if cfg.HardLimit <= 0 {
		panic("shedder: HardLimit must be > 0")
	}

	s := &Shedder{
		hardLimit: cfg.HardLimit,
		softLimit: cfg.SoftLimit,
		onShed:    cfg.OnShed,
	}

	// Determine the shed decider to use
	if cfg.ShedDecider != nil {
		s.shedDecider = cfg.ShedDecider
	} else if cfg.ShedHeader != nil {
		// Create a header-based decider
		s.shedDecider = func(r *http.Request) bool {
			return r.Header.Get(cfg.ShedHeader.Name) == cfg.ShedHeader.Value
		}
	}
	// If neither is set, shedDecider remains nil (soft shedding disabled)

	return s
}

// NewWithLimits creates a new Shedder with just hard and soft limits.
// This is a convenience function for simple use cases without callbacks.
func NewWithLimits(hardLimit, softLimit int64) *Shedder {
	return New(Config{
		HardLimit: hardLimit,
		SoftLimit: softLimit,
	})
}

// Inflight returns the current number of in-flight requests.
func (s *Shedder) Inflight() int64 {
	return s.inflight.Load()
}

// IsOverloaded returns true if in-flight requests exceed HardLimit.
func (s *Shedder) IsOverloaded() bool {
	return s.inflight.Load() > s.hardLimit
}

// IsSoftOverloaded returns true if soft limit is configured and
// in-flight requests exceed SoftLimit (but not HardLimit).
func (s *Shedder) IsSoftOverloaded() bool {
	if s.softLimit <= 0 {
		return false
	}
	inflight := s.inflight.Load()
	return inflight > s.softLimit && inflight <= s.hardLimit
}

// increment adds one to the in-flight counter and returns the new value.
func (s *Shedder) increment() int64 {
	return s.inflight.Add(1)
}

// decrement subtracts one from the in-flight counter.
func (s *Shedder) decrement() {
	s.inflight.Add(-1)
}
