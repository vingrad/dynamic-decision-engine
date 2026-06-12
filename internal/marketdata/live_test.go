package marketdata

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live smoke tests hit real vendor APIs and are skipped unless explicitly
// enabled:
//
//	DDE_MARKETDATA_LIVE_TEST=1 DDE_MARKETDATA_API_KEY=<fmp key> \
//	  go test ./internal/marketdata -run Live -v

func liveEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("DDE_MARKETDATA_LIVE_TEST") != "1" {
		t.Skip("live vendor tests disabled; set DDE_MARKETDATA_LIVE_TEST=1")
	}
}

func TestLiveFMP(t *testing.T) {
	liveEnabled(t)
	key := os.Getenv("DDE_MARKETDATA_API_KEY")
	if key == "" {
		t.Skip("set DDE_MARKETDATA_API_KEY for the FMP live test")
	}
	p, err := NewFMP(VendorConfig{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	q, err := p.Quote(ctx, "AAPL", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if q.Price <= 0 {
		t.Errorf("implausible live price %v", q.Price)
	}
	bars, err := p.HistoricalBars(ctx, "AAPL", time.Now().AddDate(0, -1, 0), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) == 0 {
		t.Error("no bars in the last month")
	}
}

func TestLiveStooq(t *testing.T) {
	liveEnabled(t)
	p, err := NewStooq(VendorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bars, err := p.HistoricalBars(ctx, "AAPL", time.Now().AddDate(0, -1, 0), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) == 0 {
		t.Error("no bars in the last month")
	}
}
