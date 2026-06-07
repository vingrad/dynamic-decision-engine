// Package marketdata is the market-data ingestion boundary for the finance
// planner. Every read is point-in-time: callers pass an asOf timestamp and the
// provider must only return data known at or before that instant. This is what
// lets the backtest replay a timeline without lookahead (peeking at the future).
//
// The default OfflineProvider serves embedded fixtures with no network, so the
// system stays runnable offline and in CI — the analog of the deterministic mock
// planner. A real HTTP provider is stubbed behind the same interface.
//
// Honesty note: offline fixtures and stubbed vendors are for development and
// decision-quality testing, not for live trading. Real providers also carry
// survivorship and revision-timing caveats the offline fixtures cannot model.
package marketdata

import (
	"context"
	"errors"
	"time"
)

// Quote is a point-in-time price snapshot.
type Quote struct {
	Ticker          string    `json:"ticker"`
	Price           float64   `json:"price"`
	AvgDollarVolume float64   `json:"avg_dollar_volume"`
	AsOf            time.Time `json:"as_of"`
}

// Fundamentals is a point-in-time fundamentals snapshot.
type Fundamentals struct {
	Ticker    string    `json:"ticker"`
	PE        float64   `json:"pe"`
	PB        float64   `json:"pb"`
	MarketCap float64   `json:"market_cap"`
	EPS       float64   `json:"eps"`
	AsOf      time.Time `json:"as_of"`
}

// Bar is a single OHLCV daily bar.
type Bar struct {
	Date   time.Time `json:"date"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
}

// Provider reads market data as of a point in time.
type Provider interface {
	Name() string
	// Quote returns the latest quote at or before asOf.
	Quote(ctx context.Context, ticker string, asOf time.Time) (Quote, error)
	// Fundamentals returns the latest fundamentals at or before asOf.
	Fundamentals(ctx context.Context, ticker string, asOf time.Time) (Fundamentals, error)
	// HistoricalBars returns bars in [from, to]; to bounds the as-of horizon.
	HistoricalBars(ctx context.Context, ticker string, from, to time.Time) ([]Bar, error)
}

// ErrNotFound indicates the symbol (or no data at/before asOf) was found.
var ErrNotFound = errors.New("marketdata: symbol or as-of data not found")

// ErrNotImplemented is returned by the HTTP provider stub.
var ErrNotImplemented = errors.New("marketdata: provider not implemented")
