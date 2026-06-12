package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// VendorConfig configures one HTTP vendor adapter.
type VendorConfig struct {
	APIKey  string       // BYOK; read from env, never logged
	BaseURL string       // override for tests/proxies; "" uses the vendor default
	Client  *http.Client // optional; a 10s-timeout client is used when nil
	Limiter *Limiter     // optional; the vendor's free-tier default when nil
}

func (c VendorConfig) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// VendorFactory builds a vendor adapter from its config.
type VendorFactory func(VendorConfig) (Provider, error)

// vendorRegistry maps vendor names to factories. Adding a vendor is one
// adapter file plus one entry here.
func vendorRegistry() map[string]VendorFactory {
	return map[string]VendorFactory{
		"fmp":   func(cfg VendorConfig) (Provider, error) { return NewFMP(cfg) },
		"stooq": func(cfg VendorConfig) (Provider, error) { return NewStooq(cfg) },
		// Placeholders: selectable for forward compatibility, every call returns
		// ErrNotImplemented (the planner degrades, nothing crashes).
		"alphavantage": notImplementedFactory("alphavantage"),
		"tiingo":       notImplementedFactory("tiingo"),
	}
}

// KnownVendors lists the registered vendor names, sorted. It is the single
// source of truth for vendor validation (config and wiring both consult it).
func KnownVendors() []string {
	reg := vendorRegistry()
	names := make([]string, 0, len(reg))
	for name := range reg {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewVendor builds the named vendor adapter.
func NewVendor(name string, cfg VendorConfig) (Provider, error) {
	factory, ok := vendorRegistry()[name]
	if !ok {
		return nil, fmt.Errorf("marketdata: unknown vendor %q (known: %v)", name, KnownVendors())
	}
	return factory(cfg)
}

// notImplementedProvider keeps a registered-but-unintegrated vendor selectable.
type notImplementedProvider struct{ vendor string }

func notImplementedFactory(name string) VendorFactory {
	return func(VendorConfig) (Provider, error) { return notImplementedProvider{vendor: name}, nil }
}

func (p notImplementedProvider) Name() string { return "http:" + p.vendor }
func (p notImplementedProvider) Quote(context.Context, string, time.Time) (Quote, error) {
	return Quote{}, fmt.Errorf("marketdata: %s: %w", p.vendor, ErrNotImplemented)
}
func (p notImplementedProvider) Fundamentals(context.Context, string, time.Time) (Fundamentals, error) {
	return Fundamentals{}, fmt.Errorf("marketdata: %s: %w", p.vendor, ErrNotImplemented)
}
func (p notImplementedProvider) HistoricalBars(context.Context, string, time.Time, time.Time) ([]Bar, error) {
	return nil, fmt.Errorf("marketdata: %s: %w", p.vendor, ErrNotImplemented)
}

// barLookbackDays is how far back a bar-derived quote searches for the last
// trading day (covers holidays and halts with margin).
const barLookbackDays = 40

// avgDollarVolumeWindow is the trailing bar count for AvgDollarVolume.
const avgDollarVolumeWindow = 20

// quoteFromBars derives a point-in-time Quote from daily bars: price is the
// last close at or before asOf, AvgDollarVolume the trailing mean of
// close*volume. Used for past-asOf reads so no live value leaks into history.
func quoteFromBars(ticker string, bars []Bar, asOf time.Time) (Quote, error) {
	last := -1
	for i := len(bars) - 1; i >= 0; i-- {
		if !bars[i].Date.After(asOf) {
			last = i
			break
		}
	}
	if last < 0 {
		return Quote{}, fmt.Errorf("marketdata: no bar at or before %s: %w", asOf.Format("2006-01-02"), ErrNotFound)
	}
	start := last - avgDollarVolumeWindow + 1
	if start < 0 {
		start = 0
	}
	var sum float64
	for _, b := range bars[start : last+1] {
		sum += b.Close * b.Volume
	}
	return Quote{
		Ticker:          ticker,
		Price:           bars[last].Close,
		AvgDollarVolume: sum / float64(last+1-start),
		AsOf:            bars[last].Date,
	}, nil
}

// startOfDayUTC truncates t to UTC midnight; asOf values from then on are
// "live" reads a real-time vendor may serve directly.
func startOfDayUTC(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}
