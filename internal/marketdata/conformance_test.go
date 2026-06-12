package marketdata

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestProviderConformance runs the shared Provider contract against every
// implementation: the offline fixtures, both real adapters on fake servers,
// the fallback chain, and the cache-wrapped chain. The contract is what the
// finance planner and backtest rely on: point-in-time reads never look ahead,
// missing data is ErrNotFound (or ErrNotSupported), and concurrent use is safe.
func TestProviderConformance(t *testing.T) {
	// All fake-backed providers know ACME bars from 2026-01-09 to 2026-02-13 and
	// use a clock frozen at 2026-02-15T12:00Z; the offline fixtures cover the
	// same window.
	asOfLive := date("2026-02-15T13:00:00Z")

	providers := map[string]func(t *testing.T) Provider{
		"offline": func(t *testing.T) Provider { return mustOffline(t) },
		"fmp": func(t *testing.T) Provider {
			srv, _ := newFMPServer(t)
			return newTestFMP(t, srv)
		},
		"stooq": func(t *testing.T) Provider {
			srv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
			return newTestStooq(t, srv)
		},
		"chain": func(t *testing.T) Provider {
			fmpSrv, _ := newFMPServer(t)
			stooqSrv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
			c, err := NewChain(newTestFMP(t, fmpSrv), newTestStooq(t, stooqSrv))
			if err != nil {
				t.Fatal(err)
			}
			return c
		},
		"cached-chain": func(t *testing.T) Provider {
			fmpSrv, _ := newFMPServer(t)
			stooqSrv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
			c, err := NewChain(newTestFMP(t, fmpSrv), newTestStooq(t, stooqSrv))
			if err != nil {
				t.Fatal(err)
			}
			return NewCachingProvider(c, CacheConfig{
				QuoteTTL:        15 * time.Minute,
				FundamentalsTTL: 24 * time.Hour,
				BarsTTL:         15 * time.Minute,
			})
		},
	}

	for name, mk := range providers {
		t.Run(name, func(t *testing.T) {
			p := mk(t)
			ctx := context.Background()

			t.Run("quote never looks ahead", func(t *testing.T) {
				q, err := p.Quote(ctx, "ACME", asOfLive)
				if err != nil {
					t.Fatal(err)
				}
				if q.AsOf.After(asOfLive) {
					t.Errorf("quote AsOf %s is after asOf %s", q.AsOf, asOfLive)
				}
			})

			t.Run("quote before any data is not found", func(t *testing.T) {
				if _, err := p.Quote(ctx, "ACME", date("2020-01-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
					t.Errorf("expected ErrNotFound, got %v", err)
				}
			})

			t.Run("unknown ticker is not found", func(t *testing.T) {
				if _, err := p.Quote(ctx, "NOPE", asOfLive); !errors.Is(err, ErrNotFound) {
					t.Errorf("expected ErrNotFound, got %v", err)
				}
			})

			t.Run("bars stay inside the range", func(t *testing.T) {
				from, to := date("2026-01-10T00:00:00Z"), date("2026-02-07T00:00:00Z")
				bars, err := p.HistoricalBars(ctx, "ACME", from, to)
				if err != nil {
					t.Fatal(err)
				}
				for i, b := range bars {
					if b.Date.Before(from) || b.Date.After(to) {
						t.Errorf("bar %d (%s) outside [%s, %s]", i, b.Date, from, to)
					}
					if i > 0 && !bars[i-1].Date.Before(b.Date) {
						t.Errorf("bars not ascending at %d", i)
					}
				}
			})

			t.Run("fundamentals declare freshness", func(t *testing.T) {
				f, err := p.Fundamentals(ctx, "ACME", asOfLive)
				if errors.Is(err, ErrNotSupported) {
					return // a capability gap is an honest answer
				}
				if err != nil {
					t.Fatal(err)
				}
				if f.Freshness != FreshnessPointInTime && f.Freshness != FreshnessLatestOnly {
					t.Errorf("fundamentals without a freshness label: %q", f.Freshness)
				}
			})

			t.Run("concurrent use is safe", func(t *testing.T) {
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						asOf := asOfLive.Add(-time.Duration(i) * 24 * time.Hour)
						_, _ = p.Quote(ctx, "ACME", asOf)
						_, _ = p.HistoricalBars(ctx, "ACME", asOf.AddDate(0, -1, 0), asOf)
						_, _ = p.Fundamentals(ctx, "ACME", asOf)
					}(i)
				}
				wg.Wait()
			})
		})
	}
}
