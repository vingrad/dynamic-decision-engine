package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
)

// HTTPConfig configures an HTTPSource.
type HTTPConfig struct {
	Name     string        // source identity recorded in provenance
	Domain   string        // decision domain it enriches; "" means any
	Endpoint string        // base URL; goal_id/signal_kind are added as query params
	APIKey   string        // optional bearer token
	Client   *http.Client  // optional; a 10s-timeout client is used when nil
	Timeout  time.Duration // optional client timeout when Client is nil
}

// HTTPSource is a generic REST data source. It GETs Endpoint and expects a JSON body
// shaped like a ContextDelta ({"facts":[...],"assets":[...],"constraints":[...]}).
// Domain-specific transformation belongs in a purpose-built adapter; this one is the
// canonical-shape baseline modelled on marketdata.HTTPProvider. Any transport,
// status, or decode failure yields a stale Result so the decision still proceeds.
type HTTPSource struct {
	name     string
	domain   string
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewHTTPSource builds an HTTPSource from cfg.
func NewHTTPSource(cfg HTTPConfig) *HTTPSource {
	client := cfg.Client
	if client == nil {
		to := cfg.Timeout
		if to <= 0 {
			to = 10 * time.Second
		}
		client = &http.Client{Timeout: to}
	}
	name := cfg.Name
	if name == "" {
		name = "http"
	}
	return &HTTPSource{name: name, domain: cfg.Domain, endpoint: cfg.Endpoint, apiKey: cfg.APIKey, client: client}
}

// Describe implements Source.
func (s *HTTPSource) Describe() Descriptor {
	return Descriptor{
		Name:        s.name,
		Domain:      s.domain,
		Description: "REST data source returning a ContextDelta JSON document",
	}
}

// wireDelta is the JSON shape an HTTPSource endpoint must return.
type wireDelta struct {
	Facts       []string            `json:"facts"`
	Assets      []domain.Asset      `json:"assets"`
	Constraints []domain.Constraint `json:"constraints"`
}

// Fetch implements Source.
func (s *HTTPSource) Fetch(ctx context.Context, q Query) (Result, error) {
	stale := func(format string, args ...any) Result {
		return Result{SourceName: s.name, Stale: true, Err: fmt.Sprintf(format, args...)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return stale("build request: %v", err), nil
	}
	qp := req.URL.Query()
	if q.Goal.ID != "" {
		qp.Set("goal_id", q.Goal.ID)
	}
	if q.SignalKind != "" {
		qp.Set("signal_kind", q.SignalKind)
	}
	req.URL.RawQuery = qp.Encode()
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return stale("request failed: %v", err), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return stale("read body: %v", err), nil
	}
	if resp.StatusCode != http.StatusOK {
		return stale("status %d", resp.StatusCode), nil
	}
	var wd wireDelta
	if err := json.Unmarshal(body, &wd); err != nil {
		return stale("decode body: %v", err), nil
	}
	return Result{
		SourceName: s.name,
		Delta:      ContextDelta{Facts: wd.Facts, Assets: wd.Assets, Constraints: wd.Constraints},
		Raw:        json.RawMessage(body),
	}, nil
}
