package source

import (
	"context"
	"sync"
)

// CachingSource wraps a primary Source with a last-good fallback. A successful,
// fresh fetch is remembered per decision key; if a later fetch fails or comes back
// stale, the remembered value is served instead — marked Stale so provenance records
// that the data is not current. This keeps a transient source outage from dropping
// useful context entirely, while never blocking or faking freshness.
//
// It wraps any Source (HTTP, MCP, agent), so the degradation policy is uniform.
type CachingSource struct {
	primary Source
	mu      sync.Mutex
	last    map[string]Result
}

// NewCachingSource wraps primary with a last-good cache.
func NewCachingSource(primary Source) *CachingSource {
	return &CachingSource{primary: primary, last: map[string]Result{}}
}

// Describe implements Source, passing through the primary's descriptor.
func (c *CachingSource) Describe() Descriptor { return c.primary.Describe() }

// Fetch implements Source. On a fresh success it caches and returns the result; on a
// degraded result it serves the last-good value (marked stale) when one exists, else
// passes the degraded result through.
func (c *CachingSource) Fetch(ctx context.Context, q Query) (Result, error) {
	key := cacheKey(q)
	out, err := c.primary.Fetch(ctx, q)
	if err == nil && !out.Stale {
		c.mu.Lock()
		c.last[key] = out
		c.mu.Unlock()
		return out, nil
	}

	c.mu.Lock()
	prev, ok := c.last[key]
	c.mu.Unlock()
	if !ok {
		// Nothing cached yet — pass the degraded result through unchanged.
		return out, err
	}
	prev.Stale = true
	if err != nil {
		prev.Err = "served last-good after error: " + err.Error()
	} else if out.Err != "" {
		prev.Err = "served last-good after: " + out.Err
	} else {
		prev.Err = "served last-good (primary stale)"
	}
	return prev, nil
}

// cacheKey identifies a decision for last-good caching: the goal's domain and id.
func cacheKey(q Query) string {
	return q.Goal.Domain + "\x00" + q.Goal.ID
}
