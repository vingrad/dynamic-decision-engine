package marketdata

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter paces vendor HTTP requests with a minimum interval between calls and
// an optional daily request budget. It exists so free-tier vendors (FMP: ~250
// requests/day) are never hammered: pacing smooths bursts, and an exhausted
// budget surfaces as ErrUnavailable so a chain falls through to the next vendor
// instead of burning the quota further.
//
// Budget accounting is tied to requests actually sent: a wait that ends in
// context cancellation refunds its budget unit and pacing slot, and a wait that
// provably cannot finish before the context deadline fails fast without
// charging at all. Safe for concurrent use.
type Limiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	next        time.Time // earliest time the next request may start
	dailyCap    int       // 0 = unlimited
	used        int
	day         time.Time // UTC midnight the current budget window started
	now         func() time.Time
}

// NewLimiter builds a limiter. minInterval <= 0 disables pacing; dailyCap 0
// disables the budget.
func NewLimiter(minInterval time.Duration, dailyCap int) *Limiter {
	return &Limiter{minInterval: minInterval, dailyCap: dailyCap, now: time.Now}
}

// Wait blocks until the next request may start, or returns early when ctx is
// done. It returns an error wrapping ErrUnavailable once the daily budget is
// exhausted (the budget rolls over at UTC midnight).
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.now()
	day := startOfDayUTC(now)
	if l.dailyCap > 0 {
		if !day.Equal(l.day) {
			l.day = day
			l.used = 0
		}
		if l.used >= l.dailyCap {
			l.mu.Unlock()
			return fmt.Errorf("marketdata: daily request budget (%d) exhausted: %w", l.dailyCap, ErrUnavailable)
		}
	}
	var wait time.Duration
	if l.minInterval > 0 && now.Before(l.next) {
		wait = l.next.Sub(now)
	}
	// A wait that cannot complete before the deadline would only burn budget:
	// fail fast without charging or reserving anything.
	if dl, ok := ctx.Deadline(); ok && wait > 0 && now.Add(wait).After(dl) {
		l.mu.Unlock()
		return context.DeadlineExceeded
	}
	// Commit: charge the budget and reserve the pacing slot.
	if l.dailyCap > 0 {
		l.used++
	}
	if l.minInterval > 0 {
		if now.Before(l.next) {
			l.next = l.next.Add(l.minInterval)
		} else {
			l.next = now.Add(l.minInterval)
		}
	}
	l.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		l.refund(day)
		return ctx.Err()
	}
}

// refund returns a charged budget unit and pacing slot after a wait that never
// led to a request. chargeDay guards the budget against a day rollover between
// charge and refund.
func (l *Limiter) refund(chargeDay time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.minInterval > 0 {
		l.next = l.next.Add(-l.minInterval)
	}
	if l.dailyCap > 0 && l.used > 0 && l.day.Equal(chargeDay) {
		l.used--
	}
}
