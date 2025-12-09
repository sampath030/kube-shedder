// Demo server showing kube-shedder usage
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	shedder "github.com/sampathshetty/kube-shedder"
)

func main() {
	port := flag.Int("port", 8080, "Server port")
	hardLimit := flag.Int64("hard-limit", 100, "Hard limit for concurrent requests")
	softLimit := flag.Int64("soft-limit", 80, "Soft limit (0 to disable)")
	flag.Parse()

	// Create the shedder
	s := shedder.New(shedder.Config{
		HardLimit: *hardLimit,
		SoftLimit: *softLimit,
		ShedHeader: &shedder.HeaderMatcher{
			Name:  "X-Priority",
			Value: "low",
		},
		OnShed: func(r *http.Request, reason shedder.ShedReason) {
			log.Printf("Shed request: path=%s reason=%s priority=%s",
				r.URL.Path, reason, r.Header.Get("X-Priority"))
		},
	})

	// Setup routes
	mux := http.NewServeMux()

	// Health check (for liveness probe)
	mux.Handle("/health", shedder.HealthHandler())

	// Readiness check (for readiness probe)
	mux.Handle("/ready", s.ReadyHandler())

	// API endpoints with load shedding
	mux.Handle("/api/", s.Middleware(http.HandlerFunc(apiHandler)))

	// Status endpoint (no shedding)
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "inflight: %d\n", s.Inflight())
		fmt.Fprintf(w, "overloaded: %v\n", s.IsOverloaded())
		fmt.Fprintf(w, "soft_overloaded: %v\n", s.IsSoftOverloaded())
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting server on %s (hardLimit=%d, softLimit=%d)",
		addr, *hardLimit, *softLimit)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate varying response times
	delay := time.Duration(50+rand.Intn(100)) * time.Millisecond
	time.Sleep(delay)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","path":%q,"delay_ms":%d}`, r.URL.Path, delay.Milliseconds())
}
