package shedder

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMiddleware_AllowsRequestsUnderLimit(t *testing.T) {
	s := New(Config{HardLimit: 10})

	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Inflight should be 0 after request completes
	if s.Inflight() != 0 {
		t.Errorf("expected inflight 0 after request, got %d", s.Inflight())
	}
}

func TestMiddleware_Rejects503WhenOverHardLimit(t *testing.T) {
	s := New(Config{HardLimit: 1})

	// Create a handler that blocks until we signal it
	blockCh := make(chan struct{})
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))

	// Start first request (will block)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	// Give time for first request to start
	time.Sleep(10 * time.Millisecond)

	// Second request should be rejected
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if rec.Header().Get("X-Shed-Reason") != "hard_limit" {
		t.Errorf("expected X-Shed-Reason header to be 'hard_limit', got %q", rec.Header().Get("X-Shed-Reason"))
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Errorf("expected Retry-After header to be '1', got %q", rec.Header().Get("Retry-After"))
	}

	// Unblock and cleanup
	close(blockCh)
	wg.Wait()
}

func TestMiddleware_SoftLimitWithDecider(t *testing.T) {
	s := New(Config{
		HardLimit: 10,
		SoftLimit: 1,
		ShedDecider: func(r *http.Request) bool {
			return r.Header.Get("X-Priority") == "low"
		},
	})

	blockCh := make(chan struct{})
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))

	// Start first request to get above soft limit
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	time.Sleep(10 * time.Millisecond)

	// High priority request should be allowed (starts but blocks)
	wg.Add(1)
	go func() {
		defer wg.Done()
		reqHigh := httptest.NewRequest("GET", "/", nil)
		reqHigh.Header.Set("X-Priority", "high")
		recHigh := httptest.NewRecorder()
		handler.ServeHTTP(recHigh, reqHigh)
	}()

	time.Sleep(10 * time.Millisecond)

	// Low priority request should be shed
	reqLow := httptest.NewRequest("GET", "/", nil)
	reqLow.Header.Set("X-Priority", "low")
	recLow := httptest.NewRecorder()
	handler.ServeHTTP(recLow, reqLow)

	if recLow.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for low priority, got %d", recLow.Code)
	}
	if recLow.Header().Get("X-Shed-Reason") != "soft_limit" {
		t.Errorf("expected X-Shed-Reason header to be 'soft_limit', got %q", recLow.Header().Get("X-Shed-Reason"))
	}

	close(blockCh)
	wg.Wait()
}

func TestMiddleware_HeaderMatcher(t *testing.T) {
	s := New(Config{
		HardLimit: 10,
		SoftLimit: 1,
		ShedHeader: &HeaderMatcher{
			Name:  "X-Priority",
			Value: "low",
		},
	})

	blockCh := make(chan struct{})
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))

	// Start blocking request
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	time.Sleep(10 * time.Millisecond)

	// Low priority should be shed
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Priority", "low")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	close(blockCh)
	wg.Wait()
}

func TestMiddleware_OnShedCallback(t *testing.T) {
	var shedCount atomic.Int32
	var lastReason ShedReason

	s := New(Config{
		HardLimit: 1,
		OnShed: func(r *http.Request, reason ShedReason) {
			shedCount.Add(1)
			lastReason = reason
		},
	})

	blockCh := make(chan struct{})
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))

	// Start blocking request
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	time.Sleep(10 * time.Millisecond)

	// This should be shed
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if shedCount.Load() != 1 {
		t.Errorf("expected OnShed to be called once, got %d", shedCount.Load())
	}
	if lastReason != ShedReasonHardLimit {
		t.Errorf("expected ShedReasonHardLimit, got %v", lastReason)
	}

	close(blockCh)
	wg.Wait()
}

func TestMiddleware_DecrementOnPanic(t *testing.T) {
	s := New(Config{HardLimit: 10})

	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			recover()
		}()
		handler.ServeHTTP(rec, req)
	}()

	if s.Inflight() != 0 {
		t.Errorf("expected inflight 0 after panic, got %d", s.Inflight())
	}
}

func TestMiddlewareFunc(t *testing.T) {
	s := New(Config{HardLimit: 10})

	// Test that MiddlewareFunc returns a proper middleware function
	mw := s.MiddlewareFunc()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_SoftLimitNoDecider(t *testing.T) {
	// When SoftLimit is set but no ShedDecider or ShedHeader, requests should pass through
	s := New(Config{
		HardLimit: 10,
		SoftLimit: 1,
	})

	blockCh := make(chan struct{})
	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))

	// Start first request
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()

	time.Sleep(10 * time.Millisecond)

	// Second request should pass through (no decider to reject it)
	wg.Add(1)
	var secondRec *httptest.ResponseRecorder
	go func() {
		defer wg.Done()
		req := httptest.NewRequest("GET", "/", nil)
		secondRec = httptest.NewRecorder()
		handler.ServeHTTP(secondRec, req)
	}()

	time.Sleep(10 * time.Millisecond)
	close(blockCh)
	wg.Wait()

	if secondRec.Code != http.StatusOK {
		t.Errorf("expected 200 when no decider, got %d", secondRec.Code)
	}
}

func TestMiddleware_ConcurrentRequests(t *testing.T) {
	s := New(Config{HardLimit: 5})

	var activeCount atomic.Int64
	var maxActive atomic.Int64

	handler := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := activeCount.Add(1)
		for {
			max := maxActive.Load()
			if current <= max || maxActive.CompareAndSwap(max, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		activeCount.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}()
	}

	wg.Wait()

	// Should have limited concurrent requests
	if maxActive.Load() > 5 {
		t.Errorf("expected max 5 concurrent requests, got %d", maxActive.Load())
	}
}
