package marketdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChainProvider tries vendors in order and falls through on capability gaps
// (ErrNotSupported, ErrNotImplemented), missing data (ErrNotFound), and
// temporary failures (ErrUnavailable) — degrade-don't-abort, like the source
// composite: a vendor outage must not kill a decision another vendor can serve.
// Context cancellation aborts immediately. When every vendor fails, the errors
// are joined so errors.Is still matches the planner's tolerated-missing paths.
type ChainProvider struct {
	providers []Provider
	name      string
}

// NewChain builds an ordered fallback over providers.
func NewChain(providers ...Provider) (*ChainProvider, error) {
	if len(providers) == 0 {
		return nil, errors.New("marketdata: chain needs at least one provider")
	}
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return &ChainProvider{providers: providers, name: strings.Join(names, "+")}, nil
}

// NewChainFromSpec builds vendors from a comma-separated spec ("fmp,stooq")
// and chains them in order. A single-vendor spec returns the bare adapter.
func NewChainFromSpec(spec string, cfg VendorConfig) (Provider, error) {
	var providers []Provider
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		p, err := NewVendor(name, cfg)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("marketdata: empty vendor spec %q", spec)
	}
	if len(providers) == 1 {
		return providers[0], nil
	}
	return NewChain(providers...)
}

// Name implements Provider; the composed name ("fmp+stooq") flows into
// provenance so an audit sees the whole stack.
func (c *ChainProvider) Name() string { return c.name }

// fallthroughErr reports whether the chain should try the next vendor.
func fallthroughErr(err error) bool {
	return errors.Is(err, ErrNotSupported) ||
		errors.Is(err, ErrNotImplemented) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrUnavailable)
}

// Quote implements Provider.
func (c *ChainProvider) Quote(ctx context.Context, ticker string, asOf time.Time) (Quote, error) {
	var errs []error
	for _, p := range c.providers {
		q, err := p.Quote(ctx, ticker, asOf)
		if err == nil {
			return q, nil
		}
		if ctx.Err() != nil {
			return Quote{}, ctx.Err()
		}
		if !fallthroughErr(err) {
			return Quote{}, err
		}
		errs = append(errs, err)
	}
	return Quote{}, errors.Join(errs...)
}

// Fundamentals implements Provider.
func (c *ChainProvider) Fundamentals(ctx context.Context, ticker string, asOf time.Time) (Fundamentals, error) {
	var errs []error
	for _, p := range c.providers {
		f, err := p.Fundamentals(ctx, ticker, asOf)
		if err == nil {
			return f, nil
		}
		if ctx.Err() != nil {
			return Fundamentals{}, ctx.Err()
		}
		if !fallthroughErr(err) {
			return Fundamentals{}, err
		}
		errs = append(errs, err)
	}
	return Fundamentals{}, errors.Join(errs...)
}

// HistoricalBars implements Provider.
func (c *ChainProvider) HistoricalBars(ctx context.Context, ticker string, from, to time.Time) ([]Bar, error) {
	var errs []error
	for _, p := range c.providers {
		bars, err := p.HistoricalBars(ctx, ticker, from, to)
		if err == nil {
			return bars, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !fallthroughErr(err) {
			return nil, err
		}
		errs = append(errs, err)
	}
	return nil, errors.Join(errs...)
}
