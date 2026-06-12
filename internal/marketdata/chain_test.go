package marketdata

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider scripts per-method behavior for chain and cache tests.
type fakeProvider struct {
	name  string
	calls atomic.Int64
	quote func(string, time.Time) (Quote, error)
	funds func(string, time.Time) (Fundamentals, error)
	bars  func(string, time.Time, time.Time) ([]Bar, error)
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Quote(_ context.Context, t string, asOf time.Time) (Quote, error) {
	f.calls.Add(1)
	if f.quote == nil {
		return Quote{}, ErrNotFound
	}
	return f.quote(t, asOf)
}
func (f *fakeProvider) Fundamentals(_ context.Context, t string, asOf time.Time) (Fundamentals, error) {
	f.calls.Add(1)
	if f.funds == nil {
		return Fundamentals{}, ErrNotFound
	}
	return f.funds(t, asOf)
}
func (f *fakeProvider) HistoricalBars(_ context.Context, t string, from, to time.Time) ([]Bar, error) {
	f.calls.Add(1)
	if f.bars == nil {
		return nil, ErrNotFound
	}
	return f.bars(t, from, to)
}

func okQuote(price float64) func(string, time.Time) (Quote, error) {
	return func(ticker string, asOf time.Time) (Quote, error) {
		return Quote{Ticker: ticker, Price: price, AsOf: asOf}, nil
	}
}

func failQuote(err error) func(string, time.Time) (Quote, error) {
	return func(string, time.Time) (Quote, error) { return Quote{}, err }
}

func TestChainName(t *testing.T) {
	c, err := NewChain(&fakeProvider{name: "fmp"}, &fakeProvider{name: "stooq"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "fmp+stooq" {
		t.Errorf("name = %q, want fmp+stooq", c.Name())
	}
}

func TestChainFallsThrough(t *testing.T) {
	for _, sentinel := range []error{ErrNotSupported, ErrNotImplemented, ErrNotFound, ErrUnavailable} {
		first := &fakeProvider{name: "first", quote: failQuote(sentinel)}
		second := &fakeProvider{name: "second", quote: okQuote(42)}
		c, _ := NewChain(first, second)
		q, err := c.Quote(context.Background(), "ACME", date("2026-02-01T00:00:00Z"))
		if err != nil {
			t.Errorf("%v: expected fall-through, got %v", sentinel, err)
		}
		if q.Price != 42 {
			t.Errorf("%v: price = %v, want 42 from second vendor", sentinel, q.Price)
		}
	}
}

func TestChainStopsOnUnexpectedError(t *testing.T) {
	boom := errors.New("boom")
	first := &fakeProvider{name: "first", quote: failQuote(boom)}
	second := &fakeProvider{name: "second", quote: okQuote(42)}
	c, _ := NewChain(first, second)
	if _, err := c.Quote(context.Background(), "ACME", date("2026-02-01T00:00:00Z")); !errors.Is(err, boom) {
		t.Fatalf("expected the unexpected error to surface, got %v", err)
	}
	if second.calls.Load() != 0 {
		t.Error("second vendor must not be tried after a non-fallthrough error")
	}
}

func TestChainJoinsErrorsWhenAllFail(t *testing.T) {
	c, _ := NewChain(
		&fakeProvider{name: "first", quote: failQuote(ErrUnavailable)},
		&fakeProvider{name: "second", quote: failQuote(ErrNotFound)},
	)
	_, err := c.Quote(context.Background(), "ACME", date("2026-02-01T00:00:00Z"))
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, ErrUnavailable) {
		t.Errorf("joined error must match both sentinels, got %v", err)
	}
}

func TestChainAbortsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	first := &fakeProvider{name: "first", quote: func(string, time.Time) (Quote, error) {
		cancel() // the world ends while vendor one is failing
		return Quote{}, ErrUnavailable
	}}
	second := &fakeProvider{name: "second", quote: okQuote(42)}
	c, _ := NewChain(first, second)
	if _, err := c.Quote(ctx, "ACME", date("2026-02-01T00:00:00Z")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if second.calls.Load() != 0 {
		t.Error("second vendor must not be tried after cancellation")
	}
}

func TestNewChainFromSpec(t *testing.T) {
	// Single keyless vendor: bare adapter, no chain wrapper.
	p, err := NewChainFromSpec("stooq", VendorConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "stooq" {
		t.Errorf("name = %q, want stooq", p.Name())
	}
	// FMP without a key must fail fast.
	if _, err := NewChainFromSpec("fmp,stooq", VendorConfig{}); err == nil {
		t.Fatal("expected fail-fast for fmp without an API key")
	}
	// With a key the composed chain builds.
	p, err = NewChainFromSpec("fmp, stooq", VendorConfig{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "fmp+stooq" {
		t.Errorf("name = %q, want fmp+stooq", p.Name())
	}
	if _, err := NewChainFromSpec(" , ", VendorConfig{}); err == nil {
		t.Fatal("expected error for an empty spec")
	}
}
