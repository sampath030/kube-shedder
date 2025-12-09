// Package shedder provides pod-level load shedding for Kubernetes services.
//
// The library tracks how many HTTP requests are currently in flight for a
// given pod. When the number exceeds a configurable HardLimit, the readiness
// endpoint returns 503, signaling Kubernetes to remove the pod from load
// balancing until load drops.
//
// # Basic Usage
//
//	// Create a shedder with a hard limit of 100 concurrent requests
//	s := shedder.New(shedder.Config{
//	    HardLimit: 100,
//	})
//
//	// Use the middleware with your HTTP server
//	http.Handle("/api/", s.Middleware(apiHandler))
//
//	// Add the readiness endpoint
//	http.Handle("/ready", s.ReadyHandler())
//
// # Soft Limit with Callback
//
// An optional SoftLimit enables selective shedding of low-priority requests
// before reaching the HardLimit:
//
//	s := shedder.New(shedder.Config{
//	    HardLimit: 100,
//	    SoftLimit: 80,
//	    ShedDecider: func(r *http.Request) bool {
//	        // Shed requests with low priority header
//	        return r.Header.Get("X-Priority") == "low"
//	    },
//	})
//
// # Soft Limit with Header Matching
//
// Alternatively, use HeaderMatcher for simple header-based shedding:
//
//	s := shedder.New(shedder.Config{
//	    HardLimit: 100,
//	    SoftLimit: 80,
//	    ShedHeader: &shedder.HeaderMatcher{
//	        Name:  "X-Priority",
//	        Value: "low",
//	    },
//	})
//
// # Integration with Kubernetes
//
// Important: Use SEPARATE endpoints for liveness and readiness probes.
// The readiness probe should use ReadyHandler (returns 503 when overloaded).
// The liveness probe should use HealthHandler (always returns 200).
// Using the readiness endpoint for liveness would cause Kubernetes to
// restart healthy-but-busy pods.
//
// The handlers are path-agnostic - register them at any path you prefer:
//
//	// Liveness - always 200 if process is running
//	http.Handle("/healthz", shedder.HealthHandler())
//
//	// Readiness - 503 when overloaded
//	http.Handle("/readyz", s.ReadyHandler())
//
// Kubernetes deployment configuration (paths must match your registration):
//
//	livenessProbe:
//	  httpGet:
//	    path: /healthz
//	    port: 8080
//	readinessProbe:
//	  httpGet:
//	    path: /readyz
//	    port: 8080
//	  periodSeconds: 5
//	  failureThreshold: 1
package shedder
