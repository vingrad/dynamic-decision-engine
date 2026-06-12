package marketdata

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// CacheConfig tunes the caching decorator. A TTL of 0 disables caching for
// that method (the same convention as PlanCacheTTL); MaxEntries and Now get
// defaults when unset.
type CacheConfig struct {
	QuoteTTL        time.Duration    // 0 disables quote caching
	FundamentalsTTL time.Duration    // 0 disables fundamentals caching
	BarsTTL         time.Duration    // 0 disables bars caching; otherwise only recent ranges expire
	MaxEntries      int              // default 4096, size cap
	Now             func() time.Time // injectable clock for tests
}

func (c CacheConfig) withDefaults() CacheConfig {
	if c.MaxEntries <= 0 {
		c.MaxEntries = 4096
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

type cacheEntry struct {
	quote     Quote
	funds     Fundamentals
	bars      []Bar
	err       error     // negative cache: ErrNotFound/ErrNotSupported
	expiresAt time.Time // zero = never expires (immutable past bar ranges)
}

// CachingProvider memoizes Provider reads. It is the outermost layer of the
// stack: a hit costs no vendor request and no rate-limit budget, which is what
// makes free-tier daily caps workable. Semantics:
//
//   - Quote/Fundamentals are keyed by a TTL-bucketed asOf, with a lookahead
//     guard: a cached value newer than the requested asOf is never served.
//   - Bar ranges are fetched at whole-day granularity (matching the cache key)
//     and re-filtered to the caller's exact [from, to] per request. A range
//     ending before the start of *yesterday* is immutable and cached without
//     expiry — the one-day grace covers vendors that backfill EOD bars late. A
//     range spanning that boundary is split into an immutable past chunk and a
//     short-TTL recent stub, so steady-state refreshes refetch only the stub.
//   - ErrNotFound/ErrNotSupported results are cached for the method's TTL so a
//     missing ticker doesn't burn a request per planner cycle. Errors that also
//     wrap ErrUnavailable (e.g. a chain where one vendor was rate-limited) are
//     never cached.
//
// Safe for concurrent use.
type CachingProvider struct {
	next Provider
	cfg  CacheConfig

	mu      sync.Mutex
	entries map[string]cacheEntry

	flightMu sync.Mutex
	inflight map[string]*flight
}

// flight is one in-progress fetch shared by every concurrent caller of the
// same key. The strategy selector fans its children out in parallel, so a
// cold key would otherwise issue N identical vendor requests at once — a
// pure TTL cache only helps the SECOND decision, singleflight helps the
// first.
type flight struct {
	done chan struct{}
	e    cacheEntry
	err  error
}

// NewCachingProvider wraps next with the cache.
func NewCachingProvider(next Provider, cfg CacheConfig) *CachingProvider {
	return &CachingProvider{
		next:     next,
		cfg:      cfg.withDefaults(),
		entries:  map[string]cacheEntry{},
		inflight: map[string]*flight{},
	}
}

// once coalesces concurrent fetches of one key: the first caller runs fetch
// (which also stores per its own policy), everyone else waits for and shares
// the leader's outcome. Waiters block on the leader's completion rather than
// their own context — vendor pacing keeps fetches short, and a failed or
// cancelled leader simply propagates its (uncached, transient) error.
func (c *CachingProvider) once(key string, fetch func() (cacheEntry, error)) (cacheEntry, error) {
	c.flightMu.Lock()
	if f, ok := c.inflight[key]; ok {
		c.flightMu.Unlock()
		<-f.done
		return f.e, f.err
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[key] = f
	c.flightMu.Unlock()

	// Double-check the cache after winning leadership: this caller may have
	// missed BEFORE a previous flight stored and retired, and fetching again
	// would defeat the coalescing it just raced past. A cached negative entry
	// propagates as its error, matching the ordinary hit path.
	if e, ok := c.lookup(key); ok {
		f.e, f.err = e, e.err
	} else {
		f.e, f.err = fetch()
	}

	c.flightMu.Lock()
	delete(c.inflight, key)
	c.flightMu.Unlock()
	close(f.done)
	return f.e, f.err
}

// Name implements Provider; the cache is invisible in provenance.
func (c *CachingProvider) Name() string { return c.next.Name() }

// cacheableErr reports whether err is a stable negative result. An error that
// also wraps ErrUnavailable is transient even when joined with cacheable
// sentinels (a chain joins every vendor's error), so it must never be cached.
func cacheableErr(err error) bool {
	if errors.Is(err, ErrUnavailable) {
		return false
	}
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotSupported)
}

func (c *CachingProvider) lookup(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return cacheEntry{}, false
	}
	if !e.expiresAt.IsZero() && c.cfg.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return cacheEntry{}, false
	}
	return e, true
}

func (c *CachingProvider) store(key string, e cacheEntry, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	if len(c.entries) >= c.cfg.MaxEntries {
		// First reclaim expired garbage (TTL-bucketed keys are never looked up
		// again once their bucket passes); only then evict arbitrarily, so
		// valuable never-expiring bar histories aren't dropped while dead
		// entries remain.
		for k, old := range c.entries {
			if !old.expiresAt.IsZero() && now.After(old.expiresAt) {
				delete(c.entries, k)
			}
		}
		for k := range c.entries {
			if len(c.entries) < c.cfg.MaxEntries {
				break
			}
			delete(c.entries, k)
		}
	}
	if ttl > 0 {
		e.expiresAt = now.Add(ttl)
	}
	c.entries[key] = e
}

// Quote implements Provider.
func (c *CachingProvider) Quote(ctx context.Context, ticker string, asOf time.Time) (Quote, error) {
	if c.cfg.QuoteTTL <= 0 {
		return c.next.Quote(ctx, ticker, asOf)
	}
	key := fmt.Sprintf("quote|%s|%d", ticker, asOf.Truncate(c.cfg.QuoteTTL).Unix())
	if e, ok := c.lookup(key); ok {
		if e.err != nil {
			return Quote{}, e.err
		}
		// Lookahead guard: a bucket can hold a value up to one TTL newer than
		// this request's asOf.
		if !e.quote.AsOf.After(asOf) {
			return e.quote, nil
		}
	}
	e, err := c.once(key, func() (cacheEntry, error) {
		q, err := c.next.Quote(ctx, ticker, asOf)
		if err != nil {
			if cacheableErr(err) {
				c.store(key, cacheEntry{err: err}, c.cfg.QuoteTTL)
			}
			return cacheEntry{}, err
		}
		c.store(key, cacheEntry{quote: q}, c.cfg.QuoteTTL)
		return cacheEntry{quote: q}, nil
	})
	if err != nil {
		return Quote{}, err
	}
	// Re-check the lookahead guard: the flight leader may have fetched with a
	// later asOf in the same TTL bucket than this waiter requested.
	if e.quote.AsOf.After(asOf) {
		return c.next.Quote(ctx, ticker, asOf)
	}
	return e.quote, nil
}

// Fundamentals implements Provider.
func (c *CachingProvider) Fundamentals(ctx context.Context, ticker string, asOf time.Time) (Fundamentals, error) {
	if c.cfg.FundamentalsTTL <= 0 {
		return c.next.Fundamentals(ctx, ticker, asOf)
	}
	key := fmt.Sprintf("funds|%s|%d", ticker, asOf.Truncate(c.cfg.FundamentalsTTL).Unix())
	if e, ok := c.lookup(key); ok {
		if e.err != nil {
			return Fundamentals{}, e.err
		}
		if !e.funds.AsOf.After(asOf) {
			return e.funds, nil
		}
	}
	e, err := c.once(key, func() (cacheEntry, error) {
		f, err := c.next.Fundamentals(ctx, ticker, asOf)
		if err != nil {
			if cacheableErr(err) {
				c.store(key, cacheEntry{err: err}, c.cfg.FundamentalsTTL)
			}
			return cacheEntry{}, err
		}
		c.store(key, cacheEntry{funds: f}, c.cfg.FundamentalsTTL)
		return cacheEntry{funds: f}, nil
	})
	if err != nil {
		return Fundamentals{}, err
	}
	if e.funds.AsOf.After(asOf) {
		return c.next.Fundamentals(ctx, ticker, asOf)
	}
	return e.funds, nil
}

// HistoricalBars implements Provider. See the type comment for the caching
// strategy (day-granular fetch, exact per-request filter, immutable past chunk
// + short-TTL recent stub).
func (c *CachingProvider) HistoricalBars(ctx context.Context, ticker string, from, to time.Time) ([]Bar, error) {
	if c.cfg.BarsTTL <= 0 {
		return c.next.HistoricalBars(ctx, ticker, from, to)
	}
	// Fetch at whole-day granularity so the date-formatted cache key truthfully
	// describes the cached payload; the caller's exact bounds are re-applied in
	// the filter below.
	fetchFrom := startOfDayUTC(from)
	// Bars dated before the start of yesterday are immutable; yesterday's and
	// today's bars may still be published or revised.
	stable := startOfDayUTC(c.cfg.Now()).AddDate(0, 0, -1)

	var bars []Bar
	var err error
	if fetchFrom.Before(stable) && !to.Before(stable) {
		var past, recent []Bar
		past, err = c.barsRange(ctx, ticker, fetchFrom, stable.Add(-time.Nanosecond), stable)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		recent, err = c.barsRange(ctx, ticker, stable, to, stable)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		bars = append(past, recent...)
		if len(bars) > 0 {
			err = nil
		}
	} else {
		bars, err = c.barsRange(ctx, ticker, fetchFrom, to, stable)
	}
	if err != nil {
		return nil, err
	}
	// Restore the caller's exact range (and never alias the cached slice).
	out := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.Date.Before(from) || b.Date.After(to) {
			continue
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("marketdata: no bars in range: %w", ErrNotFound)
	}
	return out, nil
}

// barsRange serves one cached day-granular sub-range. Ranges ending before
// stable are immutable history (no expiry); negative results always expire,
// since history can be backfilled.
func (c *CachingProvider) barsRange(ctx context.Context, ticker string, from, to, stable time.Time) ([]Bar, error) {
	key := fmt.Sprintf("bars|%s|%s|%s", ticker, from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02"))
	if e, ok := c.lookup(key); ok {
		return e.bars, e.err
	}
	// The key is day-granular, so concurrent callers of one flight requested
	// the same fetch range — no per-waiter re-check needed.
	e, err := c.once(key, func() (cacheEntry, error) {
		bars, err := c.next.HistoricalBars(ctx, ticker, from, to)
		if err != nil {
			if cacheableErr(err) {
				c.store(key, cacheEntry{err: err}, c.cfg.BarsTTL)
			}
			return cacheEntry{}, err
		}
		ttl := c.cfg.BarsTTL
		if to.Before(stable) {
			ttl = 0 // immutable history: cache without expiry
		}
		c.store(key, cacheEntry{bars: bars}, ttl)
		return cacheEntry{bars: bars}, nil
	})
	if err != nil {
		return nil, err
	}
	return e.bars, nil
}
