package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mustOffline(t *testing.T) *OfflineProvider {
	t.Helper()
	p, err := NewOfflineProvider()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func date(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestOfflineQuotePointInTime(t *testing.T) {
	p := mustOffline(t)
	ctx := context.Background()

	// As of mid-January, only the Jan 2 snapshot is known.
	q, err := p.Quote(ctx, "ACME", date("2026-01-15T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 100.0 {
		t.Errorf("expected 100.0 as of Jan 15, got %v (lookahead?)", q.Price)
	}

	// As of mid-February, the Feb 2 snapshot is the latest known.
	q, err = p.Quote(ctx, "ACME", date("2026-02-15T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 108.0 {
		t.Errorf("expected 108.0 as of Feb 15, got %v", q.Price)
	}
}

func TestOfflineQuoteBeforeAnyData(t *testing.T) {
	p := mustOffline(t)
	// Before the first snapshot: nothing is known -> ErrNotFound (no lookahead).
	if _, err := p.Quote(context.Background(), "ACME", date("2025-12-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound before first snapshot, got %v", err)
	}
}

func TestOfflineUnknownTicker(t *testing.T) {
	p := mustOffline(t)
	if _, err := p.Quote(context.Background(), "NOPE", date("2026-06-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	if _, err := p.HistoricalBars(context.Background(), "NOPE", date("2026-01-01T00:00:00Z"), date("2026-06-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for bars, got %v", err)
	}
}

func TestOfflineHistoricalBarsRange(t *testing.T) {
	p := mustOffline(t)
	bars, err := p.HistoricalBars(context.Background(), "ACME", date("2026-01-01T00:00:00Z"), date("2026-01-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	// Jan 2, 9, 16 fall in range; Jan 23+ excluded.
	if len(bars) != 3 {
		t.Fatalf("expected 3 bars in range, got %d", len(bars))
	}
	if bars[len(bars)-1].Date.After(date("2026-01-20T00:00:00Z")) {
		t.Error("returned a bar after the to bound (lookahead)")
	}
}

func TestHTTPProviderStub(t *testing.T) {
	p := NewHTTPProvider(HTTPConfig{Vendor: "alphavantage"})
	if p.Name() != "http:alphavantage" {
		t.Errorf("unexpected name %q", p.Name())
	}
	if _, err := p.Quote(context.Background(), "ACME", time.Now()); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}
