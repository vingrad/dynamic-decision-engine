package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLimiterDailyBudget(t *testing.T) {
	clock := newTestClock("2026-06-12T12:00:00Z")
	l := NewLimiter(0, 2)
	l.now = clock.Now
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if err := l.Wait(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable when budget exhausted, got %v", err)
	}

	// The budget rolls over at UTC midnight.
	clock.Advance(13 * time.Hour)
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("expected fresh budget after midnight, got %v", err)
	}
}

func TestLimiterPacing(t *testing.T) {
	l := NewLimiter(20*time.Millisecond, 0)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// First call is immediate; two more are paced 20ms apart.
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("3 calls took %v, want >= 40ms of pacing", elapsed)
	}
}

func TestLimiterContextCancel(t *testing.T) {
	l := NewLimiter(time.Hour, 0)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded during a long pace wait, got %v", err)
	}
}

func TestLimiterFailFastDoesNotCharge(t *testing.T) {
	l := NewLimiter(time.Hour, 2)
	if err := l.Wait(context.Background()); err != nil { // immediate, used=1
		t.Fatal(err)
	}
	// The next slot is an hour away; a 10ms deadline can never reach it. The
	// limiter must fail fast without charging the budget.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected immediate DeadlineExceeded, got %v", err)
	}
	l.mu.Lock()
	used := l.used
	l.mu.Unlock()
	if used != 1 {
		t.Errorf("used = %d, want 1 (hopeless wait must not be charged)", used)
	}
}

func TestLimiterRefundOnCancel(t *testing.T) {
	l := NewLimiter(time.Hour, 2)
	if err := l.Wait(context.Background()); err != nil { // used=1
		t.Fatal(err)
	}
	// Enter the pacing sleep (no deadline, so no fail-fast), then cancel.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Wait(ctx) }()
	time.Sleep(20 * time.Millisecond) // let the goroutine reach the sleep
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	// The cancelled wait must refund its budget unit: one charge left.
	l.mu.Lock()
	used := l.used
	l.mu.Unlock()
	if used != 1 {
		t.Errorf("used = %d, want 1 (cancelled wait must refund its budget unit)", used)
	}
}
