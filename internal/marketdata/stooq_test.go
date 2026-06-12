package marketdata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newStooqServer serves the CSV fixture for acme.us and a configurable body
// for everything else.
func newStooqServer(t *testing.T, fallback func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("s") == "acme.us" {
			data, err := os.ReadFile(filepath.Join("testdata", "stooq", "acme_daily.csv"))
			if err != nil {
				panic(err)
			}
			_, _ = w.Write(data)
			return
		}
		fallback(w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestStooq(t *testing.T, srv *httptest.Server) *StooqProvider {
	t.Helper()
	p, err := NewStooq(VendorConfig{BaseURL: srv.URL, Limiter: NewLimiter(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStooqSymbol(t *testing.T) {
	cases := map[string]string{
		"ACME":   "acme.us",
		"aapl":   "aapl.us",
		"SPY.US": "spy.us",
		"^SPX":   "^spx",
		"BMW.DE": "bmw.de",
	}
	for in, want := range cases {
		if got := stooqSymbol(in); got != want {
			t.Errorf("stooqSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStooqBarsAndQuote(t *testing.T) {
	srv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
	p := newTestStooq(t, srv)
	ctx := context.Background()

	bars, err := p.HistoricalBars(ctx, "ACME", date("2026-01-01T00:00:00Z"), date("2026-02-07T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 5 { // Jan 9 .. Feb 6
		t.Fatalf("got %d bars, want 5", len(bars))
	}
	if bars[0].Volume != 1700000 {
		t.Errorf("volume = %v, want 1700000", bars[0].Volume)
	}

	q, err := p.Quote(ctx, "ACME", date("2026-01-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 103.0 { // close of Jan 16, the last bar at or before asOf
		t.Errorf("price = %v, want 103.0", q.Price)
	}
	if q.AsOf.After(date("2026-01-20T00:00:00Z")) {
		t.Error("quote AsOf after requested asOf (lookahead)")
	}
}

func TestStooqNoData(t *testing.T) {
	srv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
	p := newTestStooq(t, srv)
	if _, err := p.HistoricalBars(context.Background(), "NOPE", date("2026-01-01T00:00:00Z"), date("2026-02-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStooqHeaderOnlyCSV(t *testing.T) {
	srv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("Date,Open,High,Low,Close,Volume\n")) })
	p := newTestStooq(t, srv)
	if _, err := p.HistoricalBars(context.Background(), "NOPE", date("2026-01-01T00:00:00Z"), date("2026-02-01T00:00:00Z")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStooqHTMLLimitPage(t *testing.T) {
	srv := newStooqServer(t, func(w http.ResponseWriter) {
		data, err := os.ReadFile(filepath.Join("testdata", "stooq", "ratelimited.html"))
		if err != nil {
			panic(err)
		}
		_, _ = w.Write(data)
	})
	p := newTestStooq(t, srv)
	if _, err := p.HistoricalBars(context.Background(), "NOPE", date("2026-01-01T00:00:00Z"), date("2026-02-01T00:00:00Z")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("expected ErrUnavailable for the HTML limit page, got %v", err)
	}
}

func TestStooqFundamentalsNotSupported(t *testing.T) {
	srv := newStooqServer(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("No data")) })
	p := newTestStooq(t, srv)
	if _, err := p.Fundamentals(context.Background(), "ACME", date("2026-02-01T00:00:00Z")); !errors.Is(err, ErrNotSupported) {
		t.Errorf("expected ErrNotSupported, got %v", err)
	}
}
