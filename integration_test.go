package shedder_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sampath030/kube-shedder"
)

func TestIntegration_FullWorkflow(t *testing.T) {
	s := shedder.New(shedder.Config{
		HardLimit: 5,
		SoftLimit: 3,
		ShedDecider: func(r *http.Request) bool {
			return r.Header.Get("X-Priority") == "low"
		},
	})

	// Setup handlers
	mux := http.NewServeMux()
	mux.Handle("/ready", s.ReadyHandler())
	mux.Handle("/health", shedder.HealthHandler())
	mux.Handle("/api/", s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test 1: Health always returns 200
	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("health check failed: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test 2: Readiness returns 200 initially
	resp, err = http.Get(server.URL + "/ready")
	if err != nil {
		t.Fatalf("readiness check failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("initial readiness failed: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test 3: Concurrent load test
	var wg sync.WaitGroup
	var success, softShed, hardShed atomic.Int32

	// Send 10 concurrent requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", server.URL+"/api/test", nil)
			if idx%2 == 0 {
				req.Header.Set("X-Priority", "low")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case 200:
				success.Add(1)
			case 503:
				reason := resp.Header.Get("X-Shed-Reason")
				if reason == "hard_limit" {
					hardShed.Add(1)
				} else {
					softShed.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Results: success=%d, softShed=%d, hardShed=%d",
		success.Load(), softShed.Load(), hardShed.Load())

	// We expect some requests to succeed and some to be shed
	if success.Load() == 0 {
		t.Error("expected some successful requests")
	}
	total := success.Load() + softShed.Load() + hardShed.Load()
	if total != 10 {
		t.Errorf("expected 10 total responses, got %d", total)
	}
}

func TestIntegration_ReadinessReflectsLoad(t *testing.T) {
	s := shedder.New(shedder.Config{HardLimit: 2})

	mux := http.NewServeMux()
	mux.Handle("/ready", s.ReadyHandler())

	blockCh := make(chan struct{})
	enteredCh := make(chan struct{}, 3)
	mux.Handle("/api/", s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enteredCh <- struct{}{} // Signal that we entered the handler
		<-blockCh
		w.WriteHeader(http.StatusOK)
	})))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Initial readiness
	resp, _ := http.Get(server.URL + "/ready")
	if resp.StatusCode != 200 {
		t.Errorf("expected ready initially, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Start 3 blocking requests (third will be shed immediately)
	for i := 0; i < 3; i++ {
		go func() {
			http.Get(server.URL + "/api/test")
		}()
	}

	// Wait for 2 requests to enter the handler (the other will be rejected)
	<-enteredCh
	<-enteredCh

	// At this point, 2 requests are in-flight (at hard limit)
	// Readiness should be 200 since we're AT the limit, not over
	resp, _ = http.Get(server.URL + "/ready")
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200 at hard limit, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Unblock requests
	close(blockCh)
	time.Sleep(50 * time.Millisecond)

	// Readiness should still be ready
	resp, _ = http.Get(server.URL + "/ready")
	if resp.StatusCode != 200 {
		t.Errorf("expected ready after load drops, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_OnShedLogging(t *testing.T) {
	var shedEvents []string
	var mu sync.Mutex

	s := shedder.New(shedder.Config{
		HardLimit: 1,
		OnShed: func(r *http.Request, reason shedder.ShedReason) {
			mu.Lock()
			shedEvents = append(shedEvents, r.URL.Path+":"+reason.String())
			mu.Unlock()
		},
	})

	mux := http.NewServeMux()
	blockCh := make(chan struct{})
	mux.Handle("/api/", s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	})))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Start blocking request
	go func() {
		http.Get(server.URL + "/api/first")
	}()

	time.Sleep(20 * time.Millisecond)

	// This should be shed
	http.Get(server.URL + "/api/second")

	close(blockCh)
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	if len(shedEvents) != 1 {
		t.Errorf("expected 1 shed event, got %d", len(shedEvents))
	}
	if len(shedEvents) > 0 && shedEvents[0] != "/api/second:hard_limit" {
		t.Errorf("unexpected shed event: %s", shedEvents[0])
	}
	mu.Unlock()
}

func TestIntegration_HeaderBasedShedding(t *testing.T) {
	s := shedder.New(shedder.Config{
		HardLimit: 10,
		SoftLimit: 1,
		ShedHeader: &shedder.HeaderMatcher{
			Name:  "X-Priority",
			Value: "low",
		},
	})

	mux := http.NewServeMux()
	blockCh := make(chan struct{})
	mux.Handle("/api/", s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	})))

	server := httptest.NewServer(mux)
	defer server.Close()

	// Start blocking request to get above soft limit
	go func() {
		http.Get(server.URL + "/api/first")
	}()

	time.Sleep(20 * time.Millisecond)

	// High priority should be accepted (starts blocking)
	go func() {
		req, _ := http.NewRequest("GET", server.URL+"/api/high", nil)
		req.Header.Set("X-Priority", "high")
		http.DefaultClient.Do(req)
	}()

	time.Sleep(10 * time.Millisecond)

	// Low priority should be shed
	req, _ := http.NewRequest("GET", server.URL+"/api/low", nil)
	req.Header.Set("X-Priority", "low")
	resp, _ := http.DefaultClient.Do(req)

	if resp.StatusCode != 503 {
		t.Errorf("expected 503 for low priority, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Shed-Reason") != "soft_limit" {
		t.Errorf("expected soft_limit reason, got %s", resp.Header.Get("X-Shed-Reason"))
	}

	close(blockCh)
}
