package marketdata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// maxBodyBytes bounds a vendor response body read.
const maxBodyBytes = 4 << 20

// vendorHTTP is the shared transport for vendor adapters: it paces requests
// through the vendor's limiter, issues a context-bound GET, classifies the
// status into the package sentinels, and never lets a secret reach an error
// string — API keys travel in the query string (FMP's apikey=), so errors are
// built from the vendor name, the URL path, and the status only.
type vendorHTTP struct {
	client  *http.Client
	limiter *Limiter
	vendor  string
}

// get fetches u and returns the body. 404 maps to ErrNotFound; 429, 5xx,
// transport errors, and an exhausted budget map to ErrUnavailable; 401/403 map
// to ErrUnavailable with a check-your-key hint.
func (h *vendorHTTP) get(ctx context.Context, u *url.URL) ([]byte, error) {
	if h.limiter != nil {
		if err := h.limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("marketdata: %s: build request for %s: %w", h.vendor, u.Path, err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		// Transport errors can embed the full URL (including the key): keep only
		// the error's presence, not its text.
		return nil, fmt.Errorf("marketdata: %s: request to %s failed: %w", h.vendor, u.Path, ErrUnavailable)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("marketdata: %s: read body from %s: %w", h.vendor, u.Path, ErrUnavailable)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("marketdata: %s: %s: %w", h.vendor, u.Path, ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("marketdata: %s: %s: auth rejected, status %d (check API key): %w", h.vendor, u.Path, resp.StatusCode, ErrUnavailable)
	default:
		return nil, fmt.Errorf("marketdata: %s: %s: status %d: %w", h.vendor, u.Path, resp.StatusCode, ErrUnavailable)
	}
}
