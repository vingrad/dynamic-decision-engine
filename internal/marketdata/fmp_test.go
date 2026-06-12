package marketdata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAPIKey = "sekrit-test-key"

// fmpHits counts requests per endpoint family.
type fmpHits struct{ quote, ratios, historical atomic.Int64 }

// newFMPServer serves the golden fixtures for ACME and 404s anything else.
func newFMPServer(t *testing.T) (*httptest.Server, *fmpHits) {
	t.Helper()
	hits := &fmpHits{}
	serve := func(w http.ResponseWriter, name string) {
		data, err := os.ReadFile(filepath.Join("testdata", "fmp", name))
		if err != nil {
			panic(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ACME") {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/quote/"):
			hits.quote.Add(1)
			serve(w, "quote.json")
		case strings.HasPrefix(r.URL.Path, "/ratios-ttm/"):
			hits.ratios.Add(1)
			serve(w, "ratios_ttm.json")
		case strings.HasPrefix(r.URL.Path, "/historical-price-full/"):
			hits.historical.Add(1)
			serve(w, "historical.json")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// newTestFMP builds an adapter against srv with a frozen clock (2026-02-15 12:00
// UTC) and no pacing, so "live" vs "past" asOf is deterministic.
func newTestFMP(t *testing.T, srv *httptest.Server) *FMPProvider {
	t.Helper()
	p, err := NewFMP(VendorConfig{
		APIKey:  testAPIKey,
		BaseURL: srv.URL,
		Limiter: NewLimiter(0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return date("2026-02-15T12:00:00Z") }
	return p
}

func TestFMPRequiresKey(t *testing.T) {
	if _, err := NewFMP(VendorConfig{}); err == nil {
		t.Fatal("expected an error without an API key")
	}
}

func TestFMPQuoteLive(t *testing.T) {
	srv, hits := newFMPServer(t)
	p := newTestFMP(t, srv)

	asOf := date("2026-02-15T13:00:00Z")
	q, err := p.Quote(context.Background(), "ACME", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 110.5 {
		t.Errorf("price = %v, want 110.5", q.Price)
	}
	if want := 2000000 * 110.5; q.AvgDollarVolume != want {
		t.Errorf("avg dollar volume = %v, want %v", q.AvgDollarVolume, want)
	}
	// The fixture timestamp (2026-02-16T00:00Z) is ahead of asOf: the adapter
	// must clamp AsOf so the at-or-before contract holds despite vendor skew.
	if q.AsOf.After(asOf) {
		t.Errorf("quote AsOf %s exceeds the requested asOf (clamp missing)", q.AsOf)
	}
	if got := hits.quote.Load(); got != 1 {
		t.Errorf("quote endpoint hits = %d, want 1", got)
	}
}

func TestFMPIntradayPastNotServedLive(t *testing.T) {
	srv, hits := newFMPServer(t)
	p := newTestFMP(t, srv) // frozen now = 2026-02-15T12:00Z

	// 3 hours before now, same day: outside the live window — must come from
	// bars, never the real-time endpoint.
	asOf := date("2026-02-15T09:00:00Z")
	q, err := p.Quote(context.Background(), "ACME", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 108.9 { // close of the 2026-02-13 bar, the last at or before asOf
		t.Errorf("price = %v, want 108.9 (Feb 13 close)", q.Price)
	}
	if got := hits.quote.Load(); got != 0 {
		t.Errorf("live quote endpoint hit %d times for an intraday-past asOf, want 0", got)
	}
	if _, err := p.Fundamentals(context.Background(), "ACME", asOf); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported for intraday-past fundamentals, got %v", err)
	}
}

func TestFMPQuoteMemoSharedWithFundamentals(t *testing.T) {
	srv, hits := newFMPServer(t)
	p := newTestFMP(t, srv)
	ctx := context.Background()
	asOf := date("2026-02-15T13:00:00Z")

	if _, err := p.Quote(ctx, "ACME", asOf); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Fundamentals(ctx, "ACME", asOf); err != nil {
		t.Fatal(err)
	}
	if got := hits.quote.Load(); got != 1 {
		t.Errorf("/quote hits = %d, want 1 (Fundamentals must reuse the memoized payload)", got)
	}
	if got := hits.ratios.Load(); got != 1 {
		t.Errorf("/ratios-ttm hits = %d, want 1", got)
	}
}

func TestFMPQuotePastServedFromBars(t *testing.T) {
	srv, hits := newFMPServer(t)
	p := newTestFMP(t, srv)

	// Past asOf: must come from daily bars, never the live quote endpoint.
	q, err := p.Quote(context.Background(), "ACME", date("2026-01-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 103.0 { // close of the 2026-01-16 bar, the last at or before asOf
		t.Errorf("price = %v, want 103.0 (close of Jan 16 bar)", q.Price)
	}
	if q.AsOf.After(date("2026-01-20T00:00:00Z")) {
		t.Errorf("quote AsOf %s is after the requested asOf (lookahead)", q.AsOf)
	}
	if got := hits.quote.Load(); got != 0 {
		t.Errorf("live quote endpoint hit %d times for a past asOf, want 0", got)
	}
	if got := hits.historical.Load(); got != 1 {
		t.Errorf("historical endpoint hits = %d, want 1", got)
	}
}

func TestFMPFundamentalsLive(t *testing.T) {
	srv, _ := newFMPServer(t)
	p := newTestFMP(t, srv)

	f, err := p.Fundamentals(context.Background(), "ACME", date("2026-02-15T13:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if f.PE != 20.09 || f.EPS != 5.5 || f.MarketCap != 1105000000 {
		t.Errorf("unexpected fundamentals from quote payload: %+v", f)
	}
	if f.PB != 3.2 {
		t.Errorf("PB = %v, want 3.2 from ratios-ttm", f.PB)
	}
	if f.Freshness != FreshnessLatestOnly {
		t.Errorf("freshness = %q, want %q", f.Freshness, FreshnessLatestOnly)
	}
}

func TestFMPFundamentalsPastNotSupported(t *testing.T) {
	srv, hits := newFMPServer(t)
	p := newTestFMP(t, srv)

	_, err := p.Fundamentals(context.Background(), "ACME", date("2026-01-20T00:00:00Z"))
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported for past-asOf fundamentals, got %v", err)
	}
	if got := hits.quote.Load() + hits.ratios.Load(); got != 0 {
		t.Errorf("refusal must not spend requests, got %d hits", got)
	}
}

func TestFMPHistoricalBarsAscendingAndBounded(t *testing.T) {
	srv, _ := newFMPServer(t)
	p := newTestFMP(t, srv)

	from, to := date("2026-01-10T00:00:00Z"), date("2026-02-07T00:00:00Z")
	bars, err := p.HistoricalBars(context.Background(), "ACME", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 4 { // Jan 16, 23, 30, Feb 6
		t.Fatalf("got %d bars, want 4", len(bars))
	}
	for i := 1; i < len(bars); i++ {
		if !bars[i-1].Date.Before(bars[i].Date) {
			t.Fatal("bars are not ascending")
		}
	}
	if bars[len(bars)-1].Date.After(to) {
		t.Error("bar after the to bound (lookahead)")
	}
}

func TestFMPUnknownTicker(t *testing.T) {
	srv, _ := newFMPServer(t)
	p := newTestFMP(t, srv)
	if _, err := p.Quote(context.Background(), "NOPE", date("2026-02-15T13:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFMPVendorErrorsMapToUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusUnauthorized} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		p := newTestFMP(t, srv)
		_, err := p.Quote(context.Background(), "ACME", date("2026-02-15T13:00:00Z"))
		srv.Close()
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("status %d: expected ErrUnavailable, got %v", status, err)
		}
		if err != nil && strings.Contains(err.Error(), testAPIKey) {
			t.Errorf("status %d: error leaks the API key: %v", status, err)
		}
	}
}
