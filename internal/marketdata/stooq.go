package marketdata

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	stooqDefaultBaseURL = "https://stooq.com"
	// Stooq is keyless with an unpublished daily limit: be polite.
	stooqMinInterval = time.Second
)

// StooqProvider serves EOD OHLCV bars (and bar-derived quotes) from Stooq's
// keyless CSV endpoint. It has no fundamentals — in a chain it pairs with a
// vendor that does (e.g. fmp,stooq).
//
// Availability caveat: Stooq fronts the endpoint with anti-bot protection that
// can answer non-browser clients with a JavaScript verification page. When that
// happens the adapter reports ErrUnavailable and a chain degrades gracefully;
// it does not attempt to bypass the check.
type StooqProvider struct {
	http    vendorHTTP
	baseURL string
}

// NewStooq builds the adapter; no API key is needed.
func NewStooq(cfg VendorConfig) (*StooqProvider, error) {
	base := cfg.BaseURL
	if base == "" {
		base = stooqDefaultBaseURL
	}
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = NewLimiter(stooqMinInterval, 0)
	}
	return &StooqProvider{
		http:    vendorHTTP{client: cfg.httpClient(), limiter: limiter, vendor: "stooq"},
		baseURL: base,
	}, nil
}

// Name implements Provider.
func (*StooqProvider) Name() string { return "stooq" }

// stooqSymbol maps a ticker to Stooq's naming: lowercase, with a ".us" suffix
// for plain symbols (AAPL -> aapl.us). Tickers that already carry a market
// suffix or index prefix (spy.us, ^spx) pass through unchanged.
func stooqSymbol(ticker string) string {
	s := strings.ToLower(ticker)
	if !strings.Contains(s, ".") && !strings.HasPrefix(s, "^") {
		s += ".us"
	}
	return s
}

// HistoricalBars implements Provider.
func (p *StooqProvider) HistoricalBars(ctx context.Context, ticker string, from, to time.Time) ([]Bar, error) {
	u, err := url.Parse(p.baseURL + "/q/d/l/")
	if err != nil {
		return nil, fmt.Errorf("marketdata: stooq: bad endpoint: %w", err)
	}
	params := url.Values{}
	params.Set("s", stooqSymbol(ticker))
	params.Set("i", "d")
	params.Set("d1", from.UTC().Format("20060102"))
	params.Set("d2", to.UTC().Format("20060102"))
	u.RawQuery = params.Encode()

	body, err := p.http.get(ctx, u)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || bytes.Contains(trimmed, []byte("No data")) {
		return nil, fmt.Errorf("marketdata: stooq: %s: %w", ticker, ErrNotFound)
	}
	// An HTML body is one of Stooq's interstitial pages (daily hits limit or
	// the JavaScript browser-verification challenge) instead of CSV. Either way
	// the data is unavailable to a non-browser client right now.
	if trimmed[0] == '<' {
		return nil, fmt.Errorf("marketdata: stooq: got an HTML page instead of CSV (rate limit or browser verification): %w", ErrUnavailable)
	}

	records, err := csv.NewReader(bytes.NewReader(trimmed)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("marketdata: stooq: parse CSV for %s: %w", ticker, ErrUnavailable)
	}
	var bars []Bar
	for i, rec := range records {
		if i == 0 || len(rec) < 5 { // header row: Date,Open,High,Low,Close,Volume
			continue
		}
		d, err := time.ParseInLocation("2006-01-02", rec[0], time.UTC)
		if err != nil {
			continue
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		open, err1 := strconv.ParseFloat(rec[1], 64)
		high, err2 := strconv.ParseFloat(rec[2], 64)
		low, err3 := strconv.ParseFloat(rec[3], 64)
		closing, err4 := strconv.ParseFloat(rec[4], 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		var volume float64
		if len(rec) > 5 && rec[5] != "" {
			volume, _ = strconv.ParseFloat(rec[5], 64)
		}
		bars = append(bars, Bar{Date: d, Open: open, High: high, Low: low, Close: closing, Volume: volume})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("marketdata: stooq: no parsable bars for %s: %w", ticker, ErrNotFound)
	}
	return bars, nil
}

// Quote implements Provider, derived from daily bars (Stooq's CSV is EOD data,
// which also satisfies past-asOf reads without lookahead).
func (p *StooqProvider) Quote(ctx context.Context, ticker string, asOf time.Time) (Quote, error) {
	bars, err := p.HistoricalBars(ctx, ticker, asOf.AddDate(0, 0, -barLookbackDays), asOf)
	if err != nil {
		return Quote{}, err
	}
	return quoteFromBars(ticker, bars, asOf)
}

// Fundamentals implements Provider: Stooq has none.
func (*StooqProvider) Fundamentals(context.Context, string, time.Time) (Fundamentals, error) {
	return Fundamentals{}, fmt.Errorf("marketdata: stooq has no fundamentals: %w", ErrNotSupported)
}
