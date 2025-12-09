package shedder

import (
	"net/http"
	"testing"
)

func TestNew_PanicsOnInvalidHardLimit(t *testing.T) {
	tests := []struct {
		name      string
		hardLimit int64
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic for invalid HardLimit")
				}
			}()
			New(Config{HardLimit: tt.hardLimit})
		})
	}
}

func TestNew_ValidConfig(t *testing.T) {
	s := New(Config{HardLimit: 100})
	if s.hardLimit != 100 {
		t.Errorf("expected hardLimit 100, got %d", s.hardLimit)
	}
	if s.Inflight() != 0 {
		t.Errorf("expected initial inflight 0, got %d", s.Inflight())
	}
}

func TestNew_WithSoftLimit(t *testing.T) {
	s := New(Config{HardLimit: 100, SoftLimit: 80})
	if s.softLimit != 80 {
		t.Errorf("expected softLimit 80, got %d", s.softLimit)
	}
}

func TestNewWithLimits(t *testing.T) {
	s := NewWithLimits(100, 80)
	if s.hardLimit != 100 {
		t.Errorf("expected hardLimit 100, got %d", s.hardLimit)
	}
	if s.softLimit != 80 {
		t.Errorf("expected softLimit 80, got %d", s.softLimit)
	}
}

func TestShedder_IncrementDecrement(t *testing.T) {
	s := New(Config{HardLimit: 100})

	// Increment
	if val := s.increment(); val != 1 {
		t.Errorf("expected 1 after increment, got %d", val)
	}
	if s.Inflight() != 1 {
		t.Errorf("expected inflight 1, got %d", s.Inflight())
	}

	// Another increment
	if val := s.increment(); val != 2 {
		t.Errorf("expected 2 after second increment, got %d", val)
	}

	// Decrement
	s.decrement()
	if s.Inflight() != 1 {
		t.Errorf("expected inflight 1 after decrement, got %d", s.Inflight())
	}

	s.decrement()
	if s.Inflight() != 0 {
		t.Errorf("expected inflight 0 after second decrement, got %d", s.Inflight())
	}
}

func TestShedder_IsOverloaded(t *testing.T) {
	s := New(Config{HardLimit: 2})

	if s.IsOverloaded() {
		t.Error("should not be overloaded initially")
	}

	s.increment() // 1
	if s.IsOverloaded() {
		t.Error("should not be overloaded at 1")
	}

	s.increment() // 2
	if s.IsOverloaded() {
		t.Error("should not be overloaded at hard limit")
	}

	s.increment() // 3
	if !s.IsOverloaded() {
		t.Error("should be overloaded above hard limit")
	}

	s.decrement() // back to 2
	if s.IsOverloaded() {
		t.Error("should not be overloaded after decrement")
	}
}

func TestShedder_IsSoftOverloaded(t *testing.T) {
	s := New(Config{HardLimit: 10, SoftLimit: 5})

	// Under soft limit
	for i := 0; i < 5; i++ {
		s.increment()
	}
	if s.IsSoftOverloaded() {
		t.Error("should not be soft overloaded at soft limit")
	}

	// Above soft limit, below hard limit
	s.increment() // 6
	if !s.IsSoftOverloaded() {
		t.Error("should be soft overloaded")
	}

	// At hard limit
	for i := 0; i < 4; i++ {
		s.increment()
	}
	// Now at 10
	if !s.IsSoftOverloaded() {
		t.Error("should still be soft overloaded at hard limit")
	}

	// Above hard limit - no longer "soft" overloaded, just overloaded
	s.increment() // 11
	if s.IsSoftOverloaded() {
		t.Error("should not be soft overloaded above hard limit")
	}
}

func TestShedder_SoftLimitDisabledWhenZero(t *testing.T) {
	s := New(Config{HardLimit: 10, SoftLimit: 0})

	for i := 0; i < 10; i++ {
		s.increment()
	}
	if s.IsSoftOverloaded() {
		t.Error("soft overload should be disabled when SoftLimit is 0")
	}
}

func TestShedder_SoftLimitDisabledWhenNegative(t *testing.T) {
	s := New(Config{HardLimit: 10, SoftLimit: -1})

	for i := 0; i < 10; i++ {
		s.increment()
	}
	if s.IsSoftOverloaded() {
		t.Error("soft overload should be disabled when SoftLimit is negative")
	}
}

func TestShedReason_String(t *testing.T) {
	tests := []struct {
		reason   ShedReason
		expected string
	}{
		{ShedReasonHardLimit, "hard_limit"},
		{ShedReasonSoftLimit, "soft_limit"},
		{ShedReason(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.reason.String(); got != tt.expected {
			t.Errorf("ShedReason(%d).String() = %q, want %q", tt.reason, got, tt.expected)
		}
	}
}

func TestNew_WithHeaderMatcher(t *testing.T) {
	s := New(Config{
		HardLimit: 100,
		SoftLimit: 80,
		ShedHeader: &HeaderMatcher{
			Name:  "X-Priority",
			Value: "low",
		},
	})

	if s.shedDecider == nil {
		t.Error("shedDecider should be set when ShedHeader is provided")
	}
}

func TestNew_ShedDeciderTakesPrecedence(t *testing.T) {
	customCalled := false
	s := New(Config{
		HardLimit: 100,
		SoftLimit: 80,
		ShedDecider: func(r *http.Request) bool {
			customCalled = true
			return true
		},
		ShedHeader: &HeaderMatcher{
			Name:  "X-Priority",
			Value: "low",
		},
	})

	// Call the decider
	s.shedDecider(nil)
	if !customCalled {
		t.Error("custom ShedDecider should take precedence over ShedHeader")
	}
}
