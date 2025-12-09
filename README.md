# kube-shedder

A minimal Go library for pod-level load shedding in Kubernetes services.

## Overview

kube-shedder tracks in-flight HTTP requests and provides automatic load shedding when a pod becomes overloaded. When the number of concurrent requests exceeds a configurable limit, the pod's readiness probe returns 503, signaling Kubernetes to stop routing traffic until load drops.

## Installation

```bash
go get github.com/sampath030/kube-shedder
```

## Quick Start

```go
package main

import (
    "net/http"

    "github.com/sampath030/kube-shedder"
)

func main() {
    // Create a shedder with a hard limit of 100 concurrent requests
    s := shedder.New(shedder.Config{
        HardLimit: 100,
    })

    // Add readiness endpoint for Kubernetes
    http.Handle("/ready", s.ReadyHandler())

    // Add health endpoint for liveness probe
    http.Handle("/health", shedder.HealthHandler())

    // Wrap your API handlers with the middleware
    http.Handle("/api/", s.Middleware(apiHandler))

    http.ListenAndServe(":8080", nil)
}
```

## Features

### Hard Limit

When in-flight requests exceed `HardLimit`:
- New requests receive 503 Service Unavailable
- Readiness endpoint returns 503
- Kubernetes removes the pod from load balancing

```go
s := shedder.New(shedder.Config{
    HardLimit: 100,  // Required: max concurrent requests
})
```

### Soft Limit (Optional)

Soft limit enables selective shedding of low-priority requests before reaching hard limit:

**Using a callback function:**
```go
s := shedder.New(shedder.Config{
    HardLimit: 100,
    SoftLimit: 80,
    ShedDecider: func(r *http.Request) bool {
        // Return true to shed this request
        return r.Header.Get("X-Priority") == "low"
    },
})
```

**Using header matching:**
```go
s := shedder.New(shedder.Config{
    HardLimit: 100,
    SoftLimit: 80,
    ShedHeader: &shedder.HeaderMatcher{
        Name:  "X-Priority",
        Value: "low",
    },
})
```

### Shed Notifications

Get notified when requests are shed (useful for logging/metrics):

```go
s := shedder.New(shedder.Config{
    HardLimit: 100,
    OnShed: func(r *http.Request, reason shedder.ShedReason) {
        log.Printf("Shed request: %s (reason: %s)", r.URL.Path, reason)
    },
})
```

## API

### Types

```go
// Config holds shedder configuration
type Config struct {
    HardLimit   int64                        // Required: max in-flight requests
    SoftLimit   int64                        // Optional: threshold for selective shedding
    ShedDecider func(r *http.Request) bool   // Optional: callback to decide shedding
    ShedHeader  *HeaderMatcher               // Optional: header-based shedding
    OnShed      func(r *http.Request, ShedReason) // Optional: notification callback
}

// HeaderMatcher for header-based shedding
type HeaderMatcher struct {
    Name  string  // Header name (e.g., "X-Priority")
    Value string  // Value to match (e.g., "low")
}

// ShedReason indicates why a request was shed
type ShedReason int
const (
    ShedReasonHardLimit ShedReason = iota
    ShedReasonSoftLimit
)
```

### Methods

```go
// Create a new shedder
s := shedder.New(cfg Config) *Shedder

// Convenience constructor
s := shedder.NewWithLimits(hardLimit, softLimit int64) *Shedder

// HTTP middleware
handler := s.Middleware(next http.Handler) http.Handler

// Middleware function for chains
mw := s.MiddlewareFunc() func(http.Handler) http.Handler

// Readiness handler (200 OK or 503)
handler := s.ReadyHandler() http.Handler

// Health handler (always 200 OK)
handler := shedder.HealthHandler() http.Handler

// Status methods
inflight := s.Inflight() int64
overloaded := s.IsOverloaded() bool
softOverloaded := s.IsSoftOverloaded() bool

// Notes:
// - OnShed is invoked for both hard and soft shedding events.
// - If SoftLimit > 0 but neither ShedDecider nor ShedHeader is set, soft shedding is skipped.
```

## Response Headers

Shed responses include:
- `Retry-After: 1` - Suggests retry after 1 second
- `X-Shed-Reason: hard_limit|soft_limit` - Indicates why the request was shed

## Kubernetes Integration

Configure your deployment with **separate** readiness and liveness probes:

> **Important**: Do NOT use the same endpoint for both probes. The readiness probe (`/ready`) returns 503 when overloaded, which is intentional - it removes the pod from load balancing. The liveness probe (`/health`) should always return 200 as long as the process is running. Using `/ready` for liveness would cause Kubernetes to restart healthy-but-busy pods.
>
> Set `failureThreshold` on the readiness probe low (typically `1`) so the pod is marked NotReady as soon as load shedding starts. Liveness can keep the default higher threshold since it should only trip on true process failure.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    spec:
      containers:
      - name: app
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          periodSeconds: 5
          failureThreshold: 1
```

## Framework Compatibility

kube-shedder uses standard `net/http` types and works with any Go HTTP framework:

**Chi:**
```go
r := chi.NewRouter()
r.Use(s.MiddlewareFunc())
```

**Gin:**
```go
r := gin.New()

// Wrap a standard http.Handler with kube-shedder, then register via gin.WrapH
api := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("ok"))
}))
r.Any("/api/*path", gin.WrapH(api))
```

**Echo:**
```go
e := echo.New()
e.Use(echo.WrapMiddleware(s.MiddlewareFunc()))
```

## License

MIT
