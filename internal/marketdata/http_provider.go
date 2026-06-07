package marketdata

import (
	"context"
	"time"
)

// HTTPConfig configures a real, network-backed market-data vendor.
type HTTPConfig struct {
	APIKey  string // read from env (e.g. DDE_MARKETDATA_API_KEY); never logged
	BaseURL string
	Vendor  string // e.g. "alphavantage", "tiingo"
}

// HTTPProvider is a placeholder for a real vendor integration. It implements the
// Provider interface so wiring can select it, but every method currently returns
// ErrNotImplemented — mirroring the OpenAI planner placeholder.
//
// To implement: build the vendor request from (ticker, asOf), parse the vendor's
// JSON into the shared Quote/Fundamentals/Bar structs, respect rate limits, and
// add a caching layer (the offline provider can serve as a fixture-backed cache in
// tests). Crucially, only return data the vendor knew at or before asOf so the
// point-in-time contract holds for backtests.
type HTTPProvider struct{ cfg HTTPConfig }

// NewHTTPProvider constructs the stub.
func NewHTTPProvider(cfg HTTPConfig) *HTTPProvider { return &HTTPProvider{cfg: cfg} }

// Name implements Provider.
func (p *HTTPProvider) Name() string {
	if p.cfg.Vendor != "" {
		return "http:" + p.cfg.Vendor
	}
	return "http"
}

func (*HTTPProvider) Quote(context.Context, string, time.Time) (Quote, error) {
	return Quote{}, ErrNotImplemented
}

func (*HTTPProvider) Fundamentals(context.Context, string, time.Time) (Fundamentals, error) {
	return Fundamentals{}, ErrNotImplemented
}

func (*HTTPProvider) HistoricalBars(context.Context, string, time.Time, time.Time) ([]Bar, error) {
	return nil, ErrNotImplemented
}
