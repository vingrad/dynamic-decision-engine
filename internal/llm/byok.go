package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Credentials are per-request bring-your-own-key credentials: the provider to
// call and the caller's own API key. They ride the request context so a public
// demo can let each visitor supply their own key without the engine ever holding
// one.
type Credentials struct {
	Provider string // "anthropic" (default), "openai", or "deepseek"
	Key      string
}

// credsContextKey is an unexported context key type so values set here can't
// collide with any other package's context entries.
type credsContextKey struct{}

// WithCredentials returns a context carrying the given BYOK credentials.
func WithCredentials(ctx context.Context, creds Credentials) context.Context {
	return context.WithValue(ctx, credsContextKey{}, creds)
}

// CredentialsFromContext extracts BYOK credentials from the context. ok is
// false when none were set or the key is empty (an incomplete pair is treated as
// absent so callers fall back cleanly).
func CredentialsFromContext(ctx context.Context) (Credentials, bool) {
	c, ok := ctx.Value(credsContextKey{}).(Credentials)
	if !ok || strings.TrimSpace(c.Key) == "" {
		return Credentials{}, false
	}
	return c, true
}

// PlannerName is the config/provenance identifier for the BYOK planner. It is a
// shared constant so the planner switch, config validation and the cache-disable
// guard can't drift from Name() (a drift would silently re-enable the cross-user
// plan cache).
const PlannerName = "byok"

// ErrUnsupportedProvider is returned when BYOK credentials name a provider the
// engine cannot build. The API layer maps it to a 400 so the UI can tell the
// visitor their provider selection is wrong.
var ErrUnsupportedProvider = errors.New("llm: unsupported BYOK provider")

// defaultByokCacheSize bounds how many distinct per-key planners a ByokPlanner
// keeps. It exists only to stop an abusive public demo from accumulating one
// client per spoofed key indefinitely; legitimate use stays far below it.
const defaultByokCacheSize = 256

// ByokConfig configures a ByokPlanner.
type ByokConfig struct {
	// Fallback handles requests that carry no credentials. Defaults to the
	// deterministic mock so a keyless visitor still sees a (canned) plan.
	Fallback Planner
	// Model, BaseURL and MaxTokens are the defaults handed to every per-key
	// planner this builds; empty/zero means each provider's own default.
	Model     string
	BaseURL   string
	MaxTokens int64
	// MaxCached caps the per-key planner cache; <=0 uses defaultByokCacheSize.
	MaxCached int
}

// ByokPlanner is a Planner that builds a real provider planner from the
// credentials carried on each request's context, so a public demo can run on
// keys its visitors supply rather than one the operator holds. With no
// credentials it delegates to a fallback (the mock), so the demo works without a
// key. Built planners are memoised by a hash of provider+key+model — each
// distinct key constructs its client only once. Safe for concurrent use.
type ByokPlanner struct {
	fallback  Planner
	model     string
	baseURL   string
	maxTokens int64
	maxCached int

	mu    sync.Mutex
	cache map[string]Planner
}

// NewByokPlanner constructs a ByokPlanner. A nil Fallback defaults to the mock.
func NewByokPlanner(cfg ByokConfig) *ByokPlanner {
	fb := cfg.Fallback
	if fb == nil {
		fb = NewMockPlanner()
	}
	max := cfg.MaxCached
	if max <= 0 {
		max = defaultByokCacheSize
	}
	return &ByokPlanner{
		fallback:  fb,
		model:     cfg.Model,
		baseURL:   cfg.BaseURL,
		maxTokens: cfg.MaxTokens,
		maxCached: max,
		cache:     make(map[string]Planner),
	}
}

// Name implements Planner. It is constant ("byok") regardless of which provider
// a given request resolves to, so provenance records the strategy, not the key.
func (*ByokPlanner) Name() string { return PlannerName }

// GeneratePlan implements Planner. It resolves the per-request planner from the
// context credentials (or the fallback when none are present) and delegates.
func (p *ByokPlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	creds, ok := CredentialsFromContext(ctx)
	if !ok {
		return p.fallback.GeneratePlan(ctx, req)
	}
	planner, err := p.plannerFor(creds)
	if err != nil {
		return PlanResult{}, err
	}
	return planner.GeneratePlan(ctx, req)
}

// plannerFor returns the cached planner for these credentials, building and
// caching it on first use.
func (p *ByokPlanner) plannerFor(creds Credentials) (Planner, error) {
	provider := strings.ToLower(strings.TrimSpace(creds.Provider))
	if provider == "" {
		provider = "anthropic"
	}
	key := byokCacheKey(provider, creds.Key, p.model)

	p.mu.Lock()
	defer p.mu.Unlock()
	if pl, ok := p.cache[key]; ok {
		return pl, nil
	}
	pl, err := p.build(provider, creds.Key)
	if err != nil {
		return nil, err
	}
	// Bound the cache: once full, drop it wholesale. This is rare and the cost is
	// only rebuilding cheap client wrappers, so a simple reset beats LRU bookkeeping.
	if len(p.cache) >= p.maxCached {
		p.cache = make(map[string]Planner)
	}
	p.cache[key] = pl
	return pl, nil
}

// build constructs a real provider planner for the given provider and key,
// reusing the same constructors as the statically-configured planners.
func (p *ByokPlanner) build(provider, key string) (Planner, error) {
	switch provider {
	case "anthropic":
		return NewAnthropicPlanner(AnthropicConfig{
			APIKey:    key,
			Model:     p.model,
			MaxTokens: p.maxTokens,
		}), nil
	case "openai", "deepseek":
		// One OpenAI-compatible adapter backs both; NewOpenAIPlanner fills in the
		// DeepSeek default base URL from the provider when BaseURL is empty.
		return NewOpenAIPlanner(OpenAIConfig{
			Provider:  provider,
			APIKey:    key,
			Model:     p.model,
			BaseURL:   p.baseURL,
			MaxTokens: p.maxTokens,
		}), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
}

// byokCacheKey hashes the credentials so the raw key never lives in a map key and is
// never logged. provider+model are folded in so a key reused across providers or
// model defaults builds the right distinct planner.
func byokCacheKey(provider, key, model string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + key + "\x00" + model))
	return hex.EncodeToString(sum[:])
}
