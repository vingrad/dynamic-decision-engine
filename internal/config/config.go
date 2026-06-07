// Package config loads engine configuration from sane defaults, an optional
// YAML/JSON file, and environment variable overrides (in that order of
// precedence). Environment variables always win so deployments can be configured
// without editing files.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all runtime settings.
type Config struct {
	// HTTPAddr is the address the API server listens on, e.g. ":8080".
	HTTPAddr string `json:"http_addr" yaml:"http_addr"`
	// DatabaseURL is the PostgreSQL DSN. Empty selects the in-memory store.
	DatabaseURL string `json:"database_url" yaml:"database_url"`
	// DBMaxConns bounds the PostgreSQL connection pool.
	DBMaxConns int32 `json:"db_max_conns" yaml:"db_max_conns"`
	// LogLevel is one of debug, info, warn, error.
	LogLevel string `json:"log_level" yaml:"log_level"`
	// LogFormat is "json" or "text".
	LogFormat string `json:"log_format" yaml:"log_format"`
	// Planner selects the reasoning backend: "mock" (default), "anthropic",
	// "openai", "deepseek", or "multi" (a multi-model composition).
	Planner string `json:"planner" yaml:"planner"`
	// MultiMode selects the multi-model strategy when Planner=="multi":
	// "verify", "route" or "ensemble".
	MultiMode string `json:"multi_mode" yaml:"multi_mode"`
	// MultiProviders is the ordered provider list for the multi planner, e.g.
	// ["anthropic","openai"]. Roles depend on MultiMode: verify→[proposer,verifier];
	// route→[cheap,strong]; ensemble→all members. "mock" is allowed for offline use.
	MultiProviders []string `json:"multi_providers" yaml:"multi_providers"`
	// MultiConfidenceThreshold is the router's escalation threshold (top-move confidence).
	MultiConfidenceThreshold float64 `json:"multi_confidence_threshold" yaml:"multi_confidence_threshold"`
	// MultiEscalateOnSignal makes the router send re-plans (signals) to the strong model.
	MultiEscalateOnSignal bool `json:"multi_escalate_on_signal" yaml:"multi_escalate_on_signal"`
	// LLMModel is the model id for a real LLM planner (e.g. "claude-opus-4-8").
	LLMModel string `json:"llm_model" yaml:"llm_model"`
	// LLMMaxTokens bounds the model's output for a real LLM planner.
	LLMMaxTokens int64 `json:"llm_max_tokens" yaml:"llm_max_tokens"`
	// LLMBaseURL optionally overrides the API endpoint for OpenAI-compatible
	// planners (e.g. a gateway or self-hosted endpoint). Empty uses the provider default.
	LLMBaseURL string `json:"llm_base_url" yaml:"llm_base_url"`
	// RequestTimeout bounds the time a single HTTP request may take.
	RequestTimeout time.Duration `json:"request_timeout" yaml:"request_timeout"`
	// CORSAllowedOrigins lists origins permitted to call the API (the admin UI).
	CORSAllowedOrigins []string `json:"cors_allowed_origins" yaml:"cors_allowed_origins"`
	// PolicyFile is an optional path to per-domain tunables (DDE_POLICY).
	PolicyFile string `json:"policy_file" yaml:"policy_file"`
	// MarketDataProvider selects the market-data backend for the finance planner:
	// "offline" (default, embedded fixtures, no network) or "http" (stub).
	MarketDataProvider string `json:"market_data_provider" yaml:"market_data_provider"`
	// MarketDataVendor names the HTTP vendor when MarketDataProvider == "http".
	MarketDataVendor string `json:"market_data_vendor" yaml:"market_data_vendor"`
	// FinanceHybrid enables LLM thesis narration on top of numeric scoring.
	FinanceHybrid bool `json:"finance_hybrid" yaml:"finance_hybrid"`
	// PlanCacheSize bounds the in-memory plan cache (entries). 0 disables caching.
	PlanCacheSize int `json:"plan_cache_size" yaml:"plan_cache_size"`
	// PlanCacheTTL is how long finance (market-data-dependent) plans stay cached.
	// 0 disables finance caching. Text/LLM plans are deterministic and never expire.
	PlanCacheTTL time.Duration `json:"plan_cache_ttl" yaml:"plan_cache_ttl"`
	// ReplanAsync runs replanning on a background worker pool (serve mode).
	ReplanAsync bool `json:"replan_async" yaml:"replan_async"`
	// ReplanWorkers sets the async worker count when ReplanAsync is true.
	ReplanWorkers int `json:"replan_workers" yaml:"replan_workers"`
}

// Default returns the baseline configuration used when nothing else is set.
func Default() Config {
	return Config{
		HTTPAddr:    ":8080",
		DatabaseURL: "",
		DBMaxConns:  10,
		LogLevel:    "info",
		LogFormat:   "json",
		Planner:     "mock",
		// LLMModel is empty by default so each planner applies its own
		// provider-appropriate default (Anthropic→claude-opus-4-8,
		// OpenAI→gpt-4o, DeepSeek→deepseek-chat). Override with DDE_LLM_MODEL.
		LLMModel:                 "",
		LLMMaxTokens:             4096,
		MultiMode:                "verify",
		MultiProviders:           nil,
		MultiConfidenceThreshold: 0.6,
		MultiEscalateOnSignal:    true,
		RequestTimeout:           15 * time.Second,
		CORSAllowedOrigins:       []string{"http://localhost:3000"},
		PolicyFile:               "",
		MarketDataProvider:       "offline",
		MarketDataVendor:         "alphavantage",
		FinanceHybrid:            false,
		PlanCacheSize:            1024,
		PlanCacheTTL:             60 * time.Second,
		ReplanAsync:              false,
		ReplanWorkers:            4,
	}
}

// Load builds a Config from defaults, then an optional file (path from the
// DDE_CONFIG env var), then environment variable overrides.
func Load() (Config, error) {
	cfg := Default()

	if path := os.Getenv("DDE_CONFIG"); path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadFile decodes a YAML or JSON config file by extension.
func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	switch {
	case strings.HasSuffix(path, ".json"):
		if err := json.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("config: parse json %s: %w", path, err)
		}
	default: // .yaml, .yml or unknown -> try YAML (a superset of JSON)
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("config: parse yaml %s: %w", path, err)
		}
	}
	return nil
}

// applyEnv overlays environment variable overrides onto cfg.
func applyEnv(cfg *Config) {
	if v := os.Getenv("DDE_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	// DATABASE_URL is the conventional name; DDE_DATABASE_URL is also accepted.
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("DDE_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("DDE_DB_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DBMaxConns = int32(n)
		}
	}
	if v := os.Getenv("DDE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("DDE_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("DDE_PLANNER"); v != "" {
		cfg.Planner = v
	}
	if v := os.Getenv("DDE_LLM_MODEL"); v != "" {
		cfg.LLMModel = v
	}
	if v := os.Getenv("DDE_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.LLMMaxTokens = n
		}
	}
	if v := os.Getenv("DDE_LLM_BASE_URL"); v != "" {
		cfg.LLMBaseURL = v
	}
	if v := os.Getenv("DDE_MULTI_MODE"); v != "" {
		cfg.MultiMode = v
	}
	if v := os.Getenv("DDE_MULTI_PROVIDERS"); v != "" {
		cfg.MultiProviders = splitAndTrim(v)
	}
	if v := os.Getenv("DDE_MULTI_CONFIDENCE_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.MultiConfidenceThreshold = f
		}
	}
	if v := os.Getenv("DDE_MULTI_ESCALATE_ON_SIGNAL"); v != "" {
		cfg.MultiEscalateOnSignal = v == "true" || v == "1"
	}
	if v := os.Getenv("DDE_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RequestTimeout = d
		}
	}
	if v := os.Getenv("DDE_CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.CORSAllowedOrigins = splitAndTrim(v)
	}
	if v := os.Getenv("DDE_POLICY"); v != "" {
		cfg.PolicyFile = v
	}
	if v := os.Getenv("DDE_MARKETDATA_PROVIDER"); v != "" {
		cfg.MarketDataProvider = v
	}
	if v := os.Getenv("DDE_MARKETDATA_VENDOR"); v != "" {
		cfg.MarketDataVendor = v
	}
	if v := os.Getenv("DDE_FINANCE_HYBRID"); v != "" {
		cfg.FinanceHybrid = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("DDE_PLAN_CACHE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PlanCacheSize = n
		}
	}
	if v := os.Getenv("DDE_PLAN_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.PlanCacheTTL = d
		}
	}
	if v := os.Getenv("DDE_REPLAN_ASYNC"); v != "" {
		cfg.ReplanAsync = v == "1" || strings.EqualFold(v, "true")
	}
	if v := os.Getenv("DDE_REPLAN_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ReplanWorkers = n
		}
	}
}

// Validate checks the configuration for obviously invalid values.
func (c Config) Validate() error {
	if c.HTTPAddr == "" {
		return fmt.Errorf("config: http_addr must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: invalid log_level %q", c.LogLevel)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("config: invalid log_format %q", c.LogFormat)
	}
	switch c.Planner {
	case "mock", "anthropic", "openai", "deepseek", "finance":
	case "multi":
		switch c.MultiMode {
		case "verify", "route", "ensemble":
		default:
			return fmt.Errorf("config: invalid multi_mode %q (want verify|route|ensemble)", c.MultiMode)
		}
		if len(c.MultiProviders) < 2 {
			return fmt.Errorf("config: planner=multi needs at least 2 providers (DDE_MULTI_PROVIDERS)")
		}
	default:
		return fmt.Errorf("config: invalid planner %q", c.Planner)
	}
	switch c.MarketDataProvider {
	case "offline", "http":
	default:
		return fmt.Errorf("config: invalid market_data_provider %q", c.MarketDataProvider)
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("config: request_timeout must be positive")
	}
	return nil
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
