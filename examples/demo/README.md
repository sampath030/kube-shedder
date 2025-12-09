# kube-shedder Demo

A demonstration of how to use kube-shedder as a library in your Go service.

## Installation

Add kube-shedder to your project:

```bash
go get github.com/sampathshetty/kube-shedder
```

## Usage

```go
package main

import (
    "net/http"

    "github.com/sampathshetty/kube-shedder"
)

func main() {
    // Create a shedder with limits
    s := shedder.New(shedder.Config{
        HardLimit: 100,
        SoftLimit: 80,
        ShedHeader: &shedder.HeaderMatcher{
            Name:  "X-Priority",
            Value: "low",
        },
    })

    mux := http.NewServeMux()

    // Register health/readiness at your preferred paths
    mux.Handle("/health", shedder.HealthHandler())
    mux.Handle("/ready", s.ReadyHandler())

    // Wrap your API handlers with the middleware
    mux.Handle("/api/", s.Middleware(yourAPIHandler))

    http.ListenAndServe(":8080", mux)
}
```

## Running the Demo

From the examples/demo directory:

```bash
go run main.go --hard-limit=10 --soft-limit=8
```

## Endpoints

- `/health` - Liveness probe (always returns 200)
- `/ready` - Readiness probe (503 when overloaded)
- `/api/*` - API endpoints with load shedding enabled
- `/status` - Current shedder status (no load shedding)

## Testing

```bash
# Check status
curl http://localhost:8080/status

# Normal request
curl http://localhost:8080/api/test

# Low-priority request (will be shed under soft limit pressure)
curl -H "X-Priority: low" http://localhost:8080/api/test

# Check readiness
curl http://localhost:8080/ready

# Load test
hey -n 100 -c 20 http://localhost:8080/api/test
```

## Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: app
        image: my-app:latest
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
