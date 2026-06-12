package marketdata

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

//go:embed fixtures/*.json
var fixturesFS embed.FS

// OfflineProvider serves market data from JSON fixtures with no network access.
// It enforces point-in-time semantics: Quote/Fundamentals return the most recent
// snapshot at or before asOf, and HistoricalBars never returns bars dated after to.
type OfflineProvider struct {
	quotes map[string][]Quote
	funds  map[string][]Fundamentals
	bars   map[string][]Bar
}

type offlineConfig struct{ dir string }

// OfflineOption customises the offline provider.
type OfflineOption func(*offlineConfig)

// WithFixtureDir loads fixtures from a directory on disk instead of the embedded
// defaults (used by backtests to supply scenario-specific data).
func WithFixtureDir(dir string) OfflineOption {
	return func(c *offlineConfig) { c.dir = dir }
}

// NewOfflineProvider builds the provider, loading quotes.json, fundamentals.json
// and bars.json from the embedded fixtures (or a directory via WithFixtureDir).
func NewOfflineProvider(opts ...OfflineOption) (*OfflineProvider, error) {
	var cfg offlineConfig
	for _, o := range opts {
		o(&cfg)
	}
	p := &OfflineProvider{
		quotes: map[string][]Quote{},
		funds:  map[string][]Fundamentals{},
		bars:   map[string][]Bar{},
	}
	if err := loadJSON(cfg.dir, "quotes.json", &p.quotes); err != nil {
		return nil, err
	}
	if err := loadJSON(cfg.dir, "fundamentals.json", &p.funds); err != nil {
		return nil, err
	}
	if err := loadJSON(cfg.dir, "bars.json", &p.bars); err != nil {
		return nil, err
	}
	// Keep each series sorted ascending by time for deterministic as-of lookups.
	for k := range p.quotes {
		sort.Slice(p.quotes[k], func(i, j int) bool { return p.quotes[k][i].AsOf.Before(p.quotes[k][j].AsOf) })
	}
	for k := range p.funds {
		sort.Slice(p.funds[k], func(i, j int) bool { return p.funds[k][i].AsOf.Before(p.funds[k][j].AsOf) })
		// Fixture series are dated snapshots, so they honor the as-of contract.
		for i := range p.funds[k] {
			p.funds[k][i].Freshness = FreshnessPointInTime
		}
	}
	for k := range p.bars {
		sort.Slice(p.bars[k], func(i, j int) bool { return p.bars[k][i].Date.Before(p.bars[k][j].Date) })
	}
	return p, nil
}

// loadJSON reads name from dir (if set) or the embedded fixtures into dst.
func loadJSON(dir, name string, dst any) error {
	var (
		data []byte
		err  error
	)
	if dir != "" {
		data, err = os.ReadFile(filepath.Join(dir, name))
	} else {
		data, err = fixturesFS.ReadFile("fixtures/" + name)
	}
	if err != nil {
		return fmt.Errorf("marketdata: load %s: %w", name, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("marketdata: parse %s: %w", name, err)
	}
	return nil
}

// Name implements Provider.
func (*OfflineProvider) Name() string { return "offline" }

// Quote implements Provider with point-in-time semantics.
func (p *OfflineProvider) Quote(_ context.Context, ticker string, asOf time.Time) (Quote, error) {
	series := p.quotes[ticker]
	for i := len(series) - 1; i >= 0; i-- {
		if !series[i].AsOf.After(asOf) {
			return series[i], nil
		}
	}
	return Quote{}, ErrNotFound
}

// Fundamentals implements Provider with point-in-time semantics.
func (p *OfflineProvider) Fundamentals(_ context.Context, ticker string, asOf time.Time) (Fundamentals, error) {
	series := p.funds[ticker]
	for i := len(series) - 1; i >= 0; i-- {
		if !series[i].AsOf.After(asOf) {
			return series[i], nil
		}
	}
	return Fundamentals{}, ErrNotFound
}

// HistoricalBars implements Provider, returning bars within [from, to] inclusive.
func (p *OfflineProvider) HistoricalBars(_ context.Context, ticker string, from, to time.Time) ([]Bar, error) {
	series, ok := p.bars[ticker]
	if !ok {
		return nil, ErrNotFound
	}
	var out []Bar
	for _, b := range series {
		if b.Date.Before(from) || b.Date.After(to) {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}
