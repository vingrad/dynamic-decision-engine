package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"
)

const (
	fmpDefaultBaseURL = "https://financialmodelingprep.com/api/v3"
	// Free tier allows ~250 requests/day; pace at 250ms and stop at 240 to keep
	// headroom for out-of-band use of the same key.
	fmpMinInterval = 250 * time.Millisecond
	fmpDailyCap    = 240
	// fmpLiveWindow is how far behind now an asOf may lag and still be served
	// the real-time endpoints. Anything older is a historical read: quotes are
	// derived from daily bars and fundamentals are refused, so no current value
	// ever leaks into a past asOf (not even an intraday one).
	fmpLiveWindow = 5 * time.Minute
	// fmpQuoteMemoTTL is how long a fetched /quote payload is reused inside the
	// adapter, so Quote and Fundamentals in the same planning cycle share one
	// request instead of two.
	fmpQuoteMemoTTL = 30 * time.Second
	// fmpQuoteMemoMax bounds the memo map; past it the map is reset.
	fmpQuoteMemoMax = 1024
)

// FMPProvider serves quotes, latest-only fundamentals, and EOD bars from
// Financial Modeling Prep. BYOK: the free tier is for development/demo (US
// tickers, non-commercial); production use needs a paid key.
type FMPProvider struct {
	http    vendorHTTP
	baseURL string
	apiKey  string
	now     func() time.Time

	memoMu sync.Mutex
	memo   map[string]fmpQuoteMemo
}

type fmpQuoteMemo struct {
	quote fmpQuote
	at    time.Time
}

// NewFMP builds the adapter. The API key is required: failing here (instead of
// per request) is what makes a misconfigured chain refuse to start.
func NewFMP(cfg VendorConfig) (*FMPProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("marketdata: fmp requires an API key: set DDE_MARKETDATA_API_KEY or use DDE_MARKETDATA_VENDOR=stooq")
	}
	base := cfg.BaseURL
	if base == "" {
		base = fmpDefaultBaseURL
	}
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = NewLimiter(fmpMinInterval, fmpDailyCap)
	}
	return &FMPProvider{
		http:    vendorHTTP{client: cfg.httpClient(), limiter: limiter, vendor: "fmp"},
		baseURL: base,
		apiKey:  cfg.APIKey,
		now:     time.Now,
		memo:    map[string]fmpQuoteMemo{},
	}, nil
}

// isLive reports whether asOf is close enough to now that real-time endpoints
// honor the at-or-before-asOf contract.
func (p *FMPProvider) isLive(asOf time.Time) bool {
	return !asOf.Before(p.now().Add(-fmpLiveWindow))
}

// Name implements Provider.
func (*FMPProvider) Name() string { return "fmp" }

func (p *FMPProvider) endpoint(path string, params url.Values) (*url.URL, error) {
	u, err := url.Parse(p.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("marketdata: fmp: bad endpoint %s: %w", path, err)
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("apikey", p.apiKey)
	u.RawQuery = params.Encode()
	return u, nil
}

// fmpQuote is the wire shape of one /quote element.
type fmpQuote struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	EPS       float64 `json:"eps"`
	PE        float64 `json:"pe"`
	MarketCap float64 `json:"marketCap"`
	AvgVolume float64 `json:"avgVolume"`
	Timestamp int64   `json:"timestamp"`
}

// liveQuote fetches /quote for ticker, memoizing the parsed payload for
// fmpQuoteMemoTTL so Quote and Fundamentals in the same planning cycle share
// one request.
func (p *FMPProvider) liveQuote(ctx context.Context, ticker string) (fmpQuote, error) {
	p.memoMu.Lock()
	if m, ok := p.memo[ticker]; ok && p.now().Sub(m.at) < fmpQuoteMemoTTL {
		p.memoMu.Unlock()
		return m.quote, nil
	}
	p.memoMu.Unlock()

	u, err := p.endpoint("/quote/"+url.PathEscape(ticker), nil)
	if err != nil {
		return fmpQuote{}, err
	}
	body, err := p.http.get(ctx, u)
	if err != nil {
		return fmpQuote{}, err
	}
	var quotes []fmpQuote
	if err := json.Unmarshal(body, &quotes); err != nil {
		return fmpQuote{}, fmt.Errorf("marketdata: fmp: decode quote for %s: %w", ticker, ErrUnavailable)
	}
	if len(quotes) == 0 {
		return fmpQuote{}, fmt.Errorf("marketdata: fmp: quote %s: %w", ticker, ErrNotFound)
	}
	p.memoMu.Lock()
	if len(p.memo) >= fmpQuoteMemoMax {
		p.memo = map[string]fmpQuoteMemo{}
	}
	p.memo[ticker] = fmpQuoteMemo{quote: quotes[0], at: p.now()}
	p.memoMu.Unlock()
	return quotes[0], nil
}

// Quote implements Provider. A live asOf (within fmpLiveWindow of now) is
// served from the vendor's real-time quote; anything older — including an
// intraday-past asOf — is derived from daily bars so the point-in-time
// contract holds (no current value leaks into a historical read).
func (p *FMPProvider) Quote(ctx context.Context, ticker string, asOf time.Time) (Quote, error) {
	if !p.isLive(asOf) {
		bars, err := p.HistoricalBars(ctx, ticker, asOf.AddDate(0, 0, -barLookbackDays), asOf)
		if err != nil {
			return Quote{}, err
		}
		return quoteFromBars(ticker, bars, asOf)
	}
	q, err := p.liveQuote(ctx, ticker)
	if err != nil {
		return Quote{}, err
	}
	at := p.now()
	if q.Timestamp > 0 {
		at = time.Unix(q.Timestamp, 0).UTC()
	}
	// Clamp: a vendor timestamp slightly ahead of asOf (clock skew, rounding)
	// would violate the at-or-before contract and make every cache lookup miss.
	// Within the live window the value is current for asOf too.
	if at.After(asOf) {
		at = asOf
	}
	return Quote{
		Ticker:          ticker,
		Price:           q.Price,
		AvgDollarVolume: q.AvgVolume * q.Price,
		AsOf:            at,
	}, nil
}

// Fundamentals implements Provider. FMP's free tier has no historical
// fundamentals, so any non-live asOf is refused (ErrNotSupported) rather than
// served with current values — even an intraday-past asOf, since a filing
// between asOf and now would otherwise leak. A live asOf returns the latest
// snapshot marked FreshnessLatestOnly.
func (p *FMPProvider) Fundamentals(ctx context.Context, ticker string, asOf time.Time) (Fundamentals, error) {
	if !p.isLive(asOf) {
		return Fundamentals{}, fmt.Errorf("marketdata: fmp: historical fundamentals (asOf %s): %w", asOf.Format(time.RFC3339), ErrNotSupported)
	}
	q, err := p.liveQuote(ctx, ticker)
	if err != nil {
		return Fundamentals{}, err
	}
	at := p.now().UTC()
	if at.After(asOf) {
		at = asOf // same clamp as Quote: keep AsOf at-or-before the request
	}
	f := Fundamentals{
		Ticker:    ticker,
		PE:        q.PE,
		MarketCap: q.MarketCap,
		EPS:       q.EPS,
		AsOf:      at,
		Freshness: FreshnessLatestOnly,
	}
	// PB comes from a second endpoint; its absence degrades to 0 (the planner
	// treats a zero ratio as unknown) rather than failing the whole read.
	u, err := p.endpoint("/ratios-ttm/"+url.PathEscape(ticker), nil)
	if err != nil {
		return f, nil
	}
	body, err := p.http.get(ctx, u)
	if err != nil {
		return f, nil
	}
	var ratios []struct {
		PriceToBookRatioTTM float64 `json:"priceToBookRatioTTM"`
	}
	if err := json.Unmarshal(body, &ratios); err == nil && len(ratios) > 0 {
		f.PB = ratios[0].PriceToBookRatioTTM
	}
	return f, nil
}

// HistoricalBars implements Provider.
func (p *FMPProvider) HistoricalBars(ctx context.Context, ticker string, from, to time.Time) ([]Bar, error) {
	params := url.Values{}
	params.Set("from", from.UTC().Format("2006-01-02"))
	params.Set("to", to.UTC().Format("2006-01-02"))
	u, err := p.endpoint("/historical-price-full/"+url.PathEscape(ticker), params)
	if err != nil {
		return nil, err
	}
	body, err := p.http.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Historical []struct {
			Date   string  `json:"date"`
			Open   float64 `json:"open"`
			High   float64 `json:"high"`
			Low    float64 `json:"low"`
			Close  float64 `json:"close"`
			Volume float64 `json:"volume"`
		} `json:"historical"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("marketdata: fmp: decode bars for %s: %w", ticker, ErrUnavailable)
	}
	if len(payload.Historical) == 0 {
		return nil, fmt.Errorf("marketdata: fmp: bars %s: %w", ticker, ErrNotFound)
	}
	// The vendor returns newest-first; the Provider contract is ascending.
	bars := make([]Bar, 0, len(payload.Historical))
	for i := len(payload.Historical) - 1; i >= 0; i-- {
		h := payload.Historical[i]
		d, err := time.ParseInLocation("2006-01-02", h.Date, time.UTC)
		if err != nil {
			continue
		}
		if d.Before(from) || d.After(to) {
			continue
		}
		bars = append(bars, Bar{Date: d, Open: h.Open, High: h.High, Low: h.Low, Close: h.Close, Volume: h.Volume})
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("marketdata: fmp: bars %s in range: %w", ticker, ErrNotFound)
	}
	return bars, nil
}
