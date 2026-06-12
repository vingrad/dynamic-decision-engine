package marketdata

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// testClock is a settable clock for cache and limiter tests.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock(s string) *testClock { return &testClock{t: date(s)} }
func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestCacheQuoteHitAndTTL(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	next := &fakeProvider{name: "v", quote: func(ticker string, asOf time.Time) (Quote, error) {
		return Quote{Ticker: ticker, Price: 100, AsOf: asOf}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	asOf := date("2026-02-15T12:01:00Z")

	for i := 0; i < 3; i++ {
		if _, err := c.Quote(ctx, "ACME", asOf); err != nil {
			t.Fatal(err)
		}
	}
	if got := next.calls.Load(); got != 1 {
		t.Errorf("vendor calls = %d, want 1 (cache hits)", got)
	}

	clock.Advance(16 * time.Minute)
	if _, err := c.Quote(ctx, "ACME", asOf); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 after TTL expiry", got)
	}
}

func TestCacheLookaheadGuard(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	next := &fakeProvider{name: "v", quote: func(ticker string, asOf time.Time) (Quote, error) {
		return Quote{Ticker: ticker, Price: 100, AsOf: asOf}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()

	// Both asOf values land in the same TTL bucket; the first stores a value
	// newer than the second requests. Serving it would be lookahead.
	if _, err := c.Quote(ctx, "ACME", date("2026-02-15T12:14:00Z")); err != nil {
		t.Fatal(err)
	}
	q, err := c.Quote(ctx, "ACME", date("2026-02-15T12:05:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if q.AsOf.After(date("2026-02-15T12:05:00Z")) {
		t.Errorf("cache served a value from %s for asOf 12:05 (lookahead)", q.AsOf)
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 (guard forces a refetch)", got)
	}
}

func TestCacheImmutablePastBars(t *testing.T) {
	clock := newTestClock("2026-06-12T12:00:00Z")
	next := &fakeProvider{name: "v", bars: func(_ string, from, _ time.Time) ([]Bar, error) {
		return []Bar{{Date: from, Close: 1}}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{BarsTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	from, to := date("2026-01-01T00:00:00Z"), date("2026-02-01T00:00:00Z")

	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	clock.Advance(72 * time.Hour) // far past any TTL: history is immutable
	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 1 {
		t.Errorf("vendor calls = %d, want 1 (past range cached forever)", got)
	}
}

func TestCacheTodayBarsSplitOnlyStubExpires(t *testing.T) {
	clock := newTestClock("2026-06-12T12:00:00Z")
	next := &fakeProvider{name: "v", bars: func(_ string, from, _ time.Time) ([]Bar, error) {
		return []Bar{{Date: startOfDayUTC(from), Close: 1}}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{BarsTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	from, to := date("2026-06-01T00:00:00Z"), date("2026-06-12T12:00:00Z") // touches today

	// The range spans the stability boundary (start of yesterday, Jun 11):
	// first read fetches the immutable past chunk and the recent stub.
	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 2 {
		t.Fatalf("vendor calls = %d, want 2 (past chunk + recent stub)", got)
	}
	// Within the TTL both chunks are hits.
	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 (full hit within TTL)", got)
	}
	// After the TTL only the recent stub refetches; the year of history stays.
	clock.Advance(16 * time.Minute)
	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 3 {
		t.Errorf("vendor calls = %d, want 3 (only the recent stub expires)", got)
	}
}

func TestCacheYesterdayEndingRangeExpires(t *testing.T) {
	clock := newTestClock("2026-06-12T12:00:00Z")
	next := &fakeProvider{name: "v", bars: func(_ string, from, _ time.Time) ([]Bar, error) {
		return []Bar{{Date: startOfDayUTC(from), Close: 1}}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{BarsTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	// Ends yesterday: the vendor may not have published that EOD bar yet, so
	// the yesterday-touching stub must NOT be cached forever.
	from, to := date("2026-06-11T00:00:00Z"), date("2026-06-11T23:00:00Z")

	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	clock.Advance(16 * time.Minute)
	if _, err := c.HistoricalBars(ctx, "ACME", from, to); err != nil {
		t.Fatal(err)
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 (yesterday-ending range must expire on TTL)", got)
	}
}

func TestCacheBarsExactRangeNoKeyCollision(t *testing.T) {
	clock := newTestClock("2026-06-12T12:00:00Z")
	next := &fakeProvider{name: "v", bars: func(_ string, from, _ time.Time) ([]Bar, error) {
		d := startOfDayUTC(from)
		return []Bar{{Date: d, Close: 1}, {Date: d.AddDate(0, 0, 1), Close: 2}}, nil
	}}
	c := NewCachingProvider(next, CacheConfig{BarsTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	to := date("2026-06-03T00:00:00Z")

	// Same calendar dates, different times-of-day: both must get exactly their
	// own range despite sharing one cached day-granular payload.
	late, err := c.HistoricalBars(ctx, "ACME", date("2026-06-01T09:30:00Z"), to)
	if err != nil {
		t.Fatal(err)
	}
	if len(late) != 1 || late[0].Close != 2 {
		t.Errorf("from 09:30 must exclude the Jun 1 bar, got %+v", late)
	}
	full, err := c.HistoricalBars(ctx, "ACME", date("2026-06-01T00:00:00Z"), to)
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 2 {
		t.Errorf("from midnight must include the Jun 1 bar, got %+v", full)
	}
	if got := next.calls.Load(); got != 1 {
		t.Errorf("vendor calls = %d, want 1 (both requests share the day-granular entry)", got)
	}
}

func TestCacheZeroTTLDisablesCaching(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	next := &fakeProvider{name: "v", quote: okQuote(100)}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 0, Now: clock.Now})
	ctx := context.Background()
	asOf := date("2026-02-15T12:01:00Z")

	for i := 0; i < 3; i++ {
		if _, err := c.Quote(ctx, "ACME", asOf); err != nil {
			t.Fatal(err)
		}
	}
	if got := next.calls.Load(); got != 3 {
		t.Errorf("vendor calls = %d, want 3 (TTL=0 must disable caching)", got)
	}
}

func TestCacheJoinedUnavailableNotCached(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	// A chain joins every vendor's error: one transient, one capability gap.
	joined := errors.Join(ErrUnavailable, ErrNotSupported)
	next := &fakeProvider{name: "v", quote: failQuote(joined)}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	asOf := date("2026-02-15T12:01:00Z")

	for i := 0; i < 2; i++ {
		if _, err := c.Quote(ctx, "ACME", asOf); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected the joined error, got %v", err)
		}
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 (a join containing ErrUnavailable must never be cached)", got)
	}
}

func TestCacheNegativeEntries(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	next := &fakeProvider{name: "v", quote: failQuote(ErrNotFound)}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	asOf := date("2026-02-15T12:01:00Z")

	for i := 0; i < 3; i++ {
		if _, err := c.Quote(ctx, "GONE", asOf); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	}
	if got := next.calls.Load(); got != 1 {
		t.Errorf("vendor calls = %d, want 1 (ErrNotFound cached)", got)
	}
}

func TestCacheNeverCachesUnavailable(t *testing.T) {
	clock := newTestClock("2026-02-15T12:00:00Z")
	next := &fakeProvider{name: "v", quote: failQuote(ErrUnavailable)}
	c := NewCachingProvider(next, CacheConfig{QuoteTTL: 15 * time.Minute, Now: clock.Now})
	ctx := context.Background()
	asOf := date("2026-02-15T12:01:00Z")

	for i := 0; i < 2; i++ {
		if _, err := c.Quote(ctx, "ACME", asOf); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected ErrUnavailable, got %v", err)
		}
	}
	if got := next.calls.Load(); got != 2 {
		t.Errorf("vendor calls = %d, want 2 (ErrUnavailable never cached)", got)
	}
}

func TestCacheNameDelegates(t *testing.T) {
	c := NewCachingProvider(&fakeProvider{name: "fmp+stooq"}, CacheConfig{})
	if c.Name() != "fmp+stooq" {
		t.Errorf("cache must be invisible in provenance, got %q", c.Name())
	}
}
