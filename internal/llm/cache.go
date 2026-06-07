package llm

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// PlanCache stores planner results keyed by a fingerprint of the request. The
// in-memory LRU below is the default; a Redis/Postgres-backed implementation can
// be slotted in behind this interface for horizontal scale.
type PlanCache interface {
	Get(key string) (PlanResult, bool)
	Put(key string, r PlanResult)
}

// CacheObserver receives cache hit/miss events labelled by domain. app.Metrics
// satisfies it; declaring it here keeps llm free of an app dependency.
type CacheObserver interface {
	PlanCacheHit(domain string)
	PlanCacheMiss(domain string)
}

// CachingPlanner memoises a base planner by the decision-relevant inputs. Because
// it sits inside the GuidedPlanner (Guided(Caching(base))), the request's prompt
// override and signal payload are already populated, so identical decisions —
// including coalesced bursts of the same signal — reuse one model call.
type CachingPlanner struct {
	base  Planner
	cache PlanCache
	obs   CacheObserver
}

// NewCachingPlanner wraps base with the given cache. obs may be nil.
func NewCachingPlanner(base Planner, cache PlanCache, obs CacheObserver) *CachingPlanner {
	return &CachingPlanner{base: base, cache: cache, obs: obs}
}

// Name implements Planner.
func (c *CachingPlanner) Name() string { return c.base.Name() }

// GeneratePlan returns a cached result when the request fingerprint matches,
// otherwise computes and stores it.
func (c *CachingPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	key := cacheKey(c.base.Name(), req)
	if r, ok := c.cache.Get(key); ok {
		if c.obs != nil {
			c.obs.PlanCacheHit(req.Goal.Domain)
		}
		return r, nil
	}
	if c.obs != nil {
		c.obs.PlanCacheMiss(req.Goal.Domain)
	}
	r, err := c.base.GeneratePlan(ctx, req)
	if err != nil {
		return PlanResult{}, err
	}
	c.cache.Put(key, r)
	return r, nil
}

// cacheKey fingerprints everything that can change the output: the base planner,
// the decision-input snapshot (goal+context+domain+signal note), the prompt
// override (which encodes the pack/prompt version), and any structured signal
// payload that numeric planners read.
func cacheKey(name string, req PlanRequest) string {
	payload, _ := json.Marshal(req.SignalPayload)
	h := sha256.Sum256(append([]byte(req.SystemPromptOverride+"\x00"), payload...))
	return name + "|" + inputSnapshotID(req.Goal, req.SignalNote) + "|" + hex.EncodeToString(h[:12])
}

// MemoryCache is a concurrency-safe, fixed-capacity LRU PlanCache with optional
// per-entry TTL. With ttl == 0 entries never expire (the right choice for
// deterministic text planners); with ttl > 0 a stored entry is treated as a miss
// once it ages past ttl (the right choice for the time-varying finance planner,
// whose output depends on as-of market data).
type MemoryCache struct {
	mu    sync.Mutex
	cap   int
	ttl   time.Duration
	now   func() time.Time
	ll    *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key       string
	val       PlanResult
	expiresAt time.Time // zero == never expires
}

// NewMemoryCache returns an LRU cache (no TTL) holding up to capacity entries.
func NewMemoryCache(capacity int) *MemoryCache {
	return NewMemoryCacheTTL(capacity, 0, time.Now)
}

// NewMemoryCacheTTL returns an LRU cache whose entries expire after ttl (0 == no
// expiry). now defaults to time.Now; inject a fake clock in tests.
func NewMemoryCacheTTL(capacity int, ttl time.Duration, now func() time.Time) *MemoryCache {
	if capacity < 1 {
		capacity = 1
	}
	if now == nil {
		now = time.Now
	}
	return &MemoryCache{
		cap:   capacity,
		ttl:   ttl,
		now:   now,
		ll:    list.New(),
		items: make(map[string]*list.Element, capacity),
	}
}

// Get implements PlanCache. An expired entry is removed and reported as a miss.
func (m *MemoryCache) Get(key string) (PlanResult, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.items[key]
	if !ok {
		return PlanResult{}, false
	}
	ent := el.Value.(*cacheEntry)
	if !ent.expiresAt.IsZero() && !m.now().Before(ent.expiresAt) {
		m.ll.Remove(el)
		delete(m.items, key)
		return PlanResult{}, false
	}
	m.ll.MoveToFront(el)
	return ent.val, true
}

// Put implements PlanCache, evicting the least-recently-used entry when full.
func (m *MemoryCache) Put(key string, r PlanResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var exp time.Time
	if m.ttl > 0 {
		exp = m.now().Add(m.ttl)
	}
	if el, ok := m.items[key]; ok {
		ent := el.Value.(*cacheEntry)
		ent.val = r
		ent.expiresAt = exp
		m.ll.MoveToFront(el)
		return
	}
	el := m.ll.PushFront(&cacheEntry{key: key, val: r, expiresAt: exp})
	m.items[key] = el
	if m.ll.Len() > m.cap {
		oldest := m.ll.Back()
		if oldest != nil {
			m.ll.Remove(oldest)
			delete(m.items, oldest.Value.(*cacheEntry).key)
		}
	}
}
