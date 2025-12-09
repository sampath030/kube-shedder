package shedder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadyHandler_Returns200WhenUnderLimit(t *testing.T) {
	s := New(Config{HardLimit: 10})

	handler := s.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ready") {
		t.Errorf("expected 'ready' in body, got %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain content type, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestReadyHandler_Returns503WhenOverLimit(t *testing.T) {
	s := New(Config{HardLimit: 2})

	// Simulate 3 in-flight requests
	s.increment()
	s.increment()
	s.increment()

	handler := s.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not ready") {
		t.Errorf("expected 'not ready' in body, got %s", rec.Body.String())
	}
}

func TestReadyHandler_ReturnsAtLimit(t *testing.T) {
	s := New(Config{HardLimit: 2})

	// At exactly hard limit
	s.increment()
	s.increment()

	handler := s.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should still be ready at limit (only fails when exceeded)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 at hard limit, got %d", rec.Code)
	}
}

func TestReadyHandler_ReturnsInflightInfo(t *testing.T) {
	s := New(Config{HardLimit: 100})
	s.increment()
	s.increment()

	handler := s.ReadyHandler()
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "inflight=2") {
		t.Errorf("expected inflight info in body, got %s", body)
	}
	if !strings.Contains(body, "hardLimit=100") {
		t.Errorf("expected hardLimit info in body, got %s", body)
	}
}

func TestReadyHandlerFunc(t *testing.T) {
	s := New(Config{HardLimit: 10})

	hf := s.ReadyHandlerFunc()

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()

	hf(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHealthHandler(t *testing.T) {
	handler := HealthHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("expected 'ok', got %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain content type, got %s", rec.Header().Get("Content-Type"))
	}
}

func TestHealthHandler_AlwaysReturns200(t *testing.T) {
	handler := HealthHandler()

	// Test multiple times with different methods
	methods := []string{"GET", "HEAD", "POST"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", method, rec.Code)
		}
	}
}
