package wire

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/llm"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
	"github.com/vingrad/dynamic-decision-engine/internal/pack"
	"github.com/vingrad/dynamic-decision-engine/internal/policy"
)

// DataSource is the generic marker for an external data provider wired into the
// planner deps. Concrete sources (e.g. marketdata.Provider) are recovered by a
// builder via a type assertion. Keeping it a marker keeps PlannerDeps free of any
// single domain's data shape.
type DataSource interface {
	Name() string
}

// PlannerBuilder constructs the planner for a domain that declares a PlannerKind.
// Returning a nil planner declines (e.g. the required data source is not wired),
// in which case BuildPlannerRouter falls back to the guided/base text planner.
type PlannerBuilder func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.Planner, error)

// plannerBuilders maps a descriptor's PlannerKind to its builder. Adding a new
// numeric domain registers a builder here and sets the descriptor's PlannerKind —
// no edit to BuildPlannerRouter.
var plannerBuilders = map[string]PlannerBuilder{
	"finance": buildFinancePlanner,
}

// buildFinancePlanner builds the SINGLE blended numeric finance planner for a
// "finance"-kind domain — strategy competition is assembled separately and
// generically (buildStrategySelector), so this builder only serves domains
// (or deployments) running without selection. It pulls the "marketdata" data
// source; if it is absent or of the wrong type, it declines (returns nil) so
// the domain falls back to the guided text planner — matching the prior
// "investing without a provider" behaviour.
func buildFinancePlanner(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.Planner, error) {
	provider, ok := deps.DataSources[marketDataKey].(marketdata.Provider)
	if !ok {
		return nil, nil // no market data wired → decline to guided fallback
	}

	cfg := llm.FinanceConfig{
		Provider:       provider,
		Scoring:        effectiveScoring(d, pol),
		Inner:          deps.FinanceInner,
		Now:            deps.FinanceNow,
		PackID:         d.ID,
		PackVersion:    d.Version,
		PromptTemplate: d.PromptTemplate,
	}
	if dp, ok := pol.For(d.ID); ok && dp.Calibration != nil {
		cfg.Calibration = dp.Calibration
	}
	fin := llm.Planner(llm.NewFinancePlanner(cfg))
	// Finance is cached only via a TTL cache so plans refresh as market data moves;
	// never via the non-expiring text cache.
	if deps.FinanceCache != nil {
		fin = llm.NewCachingPlanner(fin, deps.FinanceCache, deps.CacheObs)
	}
	return fin, nil
}

// selectionEnabled reports whether the strategy competition is on for a domain.
// An explicit policy setting wins; otherwise the PACK's declared default
// decides — a default-on flip must be earned per domain (investing earned it
// via TestStrategyMatrixGates), never inherited just by declaring strategies.
func selectionEnabled(d pack.Descriptor, pol policy.Policy) bool {
	if dp, ok := pol.For(d.ID); ok && dp.Strategy != nil && dp.Strategy.Enabled != nil {
		return *dp.Strategy.Enabled
	}
	return d.SelectionDefaultOn
}

// activeStrategies returns the descriptor's strategies minus any the policy
// disables. Unknown IDs in the disable list are simply ignored so a policy
// file survives pack evolution.
func activeStrategies(d pack.Descriptor, pol policy.Policy) []pack.StrategyDescriptor {
	disabled := map[string]bool{}
	if dp, ok := pol.For(d.ID); ok && dp.Strategy != nil {
		for _, id := range dp.Strategy.Disable {
			disabled[id] = true
		}
	}
	var out []pack.StrategyDescriptor
	for _, s := range d.Strategies {
		if !disabled[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// StrategyKit is everything a planner kind contributes to a strategy
// competition: how to build one strategy child, how to classify the context
// that gates strategies (with the CLOSED set of labels strategies may
// declare), and whether the kind's data dependency is wired at all. The kit
// keeps child-building and classification together because they share the
// same data dependency — the finance classifier reads the same market data
// the finance children do.
type StrategyKit struct {
	// BuildChild builds one strategy child. inner is the optional narrator
	// (nil for competing children — narration runs once on the winner, inside
	// the selector — and the kit's narrator for a pinned single strategy).
	BuildChild func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, s pack.StrategyDescriptor, inner llm.Planner) (llm.Planner, error)
	// BuildClassifier returns the context classifier and the labels strategies
	// may declare. A nil RegimeFn means no gating for this kind.
	BuildClassifier func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.RegimeFn, []string, error)
	// Available reports whether the kind's data dependency is wired. When it
	// is not, the WHOLE domain declines to its non-selection fallback exactly
	// as it would without strategies — the competition must never degrade a
	// kind domain into mis-parameterized text children.
	Available func(deps PlannerDeps) bool
}

// strategyKits maps a descriptor's PlannerKind to its strategy kit. A new
// numeric domain that wants strategy competition registers a kit here next to
// its plannerBuilders entry.
var strategyKits = map[string]*StrategyKit{
	"finance": financeStrategyKit(),
}

// financeStrategyKit adapts the finance lens builder and regime classifier to
// the generic kit contract.
func financeStrategyKit() *StrategyKit {
	mustProvider := func(deps PlannerDeps) (marketdata.Provider, error) {
		provider, ok := deps.DataSources[marketDataKey].(marketdata.Provider)
		if !ok {
			return nil, fmt.Errorf("finance strategy kit: no market data wired")
		}
		return provider, nil
	}
	return &StrategyKit{
		Available: func(deps PlannerDeps) bool {
			_, ok := deps.DataSources[marketDataKey].(marketdata.Provider)
			return ok
		},
		BuildChild: func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, s pack.StrategyDescriptor, inner llm.Planner) (llm.Planner, error) {
			provider, err := mustProvider(deps)
			if err != nil {
				return nil, err
			}
			return buildFinanceChild(d, pol, deps, provider, s, inner), nil
		},
		BuildClassifier: func(d pack.Descriptor, pol policy.Policy, deps PlannerDeps) (llm.RegimeFn, []string, error) {
			provider, err := mustProvider(deps)
			if err != nil {
				return nil, nil, err
			}
			labels := []string{string(finance.RegimeTrend), string(finance.RegimeRange), string(finance.RegimeHighVol)}
			return financeRegimeFn(provider, deps.FinanceNow), labels, nil
		},
	}
}

// buildStrategySelector assembles a domain's strategy competition, for ANY
// domain: kit children for a registered planner kind, prompt-variant text
// children otherwise. It returns (nil, nil) when the domain runs without
// competition — selection disabled, no strategies declared, or the kind's
// kit unavailable (the decline rule) — and the caller proceeds with the
// normal single-planner path. Declared-but-invalid strategy configuration is
// an error, never a silent fallback. The selector itself is never cached:
// selection is cheap and pure, and its hysteresis input (the incumbent
// strategy) must stay live.
func buildStrategySelector(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, textChild func(pack.Descriptor, pack.StrategyDescriptor) llm.Planner) (llm.Planner, error) {
	strategies := activeStrategies(d, pol)
	if !selectionEnabled(d, pol) || len(strategies) == 0 {
		return nil, nil
	}
	if err := d.ValidateStrategies(); err != nil {
		return nil, err
	}

	kit := strategyKits[d.PlannerKind]
	if d.PlannerKind != "" && kit == nil {
		// A numeric kind without a kit cannot compete: its strategies carry
		// kind-specific tuning that text children cannot interpret.
		return nil, fmt.Errorf("domain kind %q declares strategies but registers no strategy kit", d.PlannerKind)
	}
	if kit != nil && !kit.Available(deps) {
		return nil, nil // decline: the domain falls back exactly as without strategies
	}

	// The classifier's label set is CLOSED: a declared label outside it would
	// silently gate the strategy out of every classified context (fail-closed),
	// so it fails the build instead.
	var regimeFn llm.RegimeFn
	var labels []string
	if kit != nil {
		var err error
		regimeFn, labels, err = kit.BuildClassifier(d, pol, deps)
		if err != nil {
			return nil, err
		}
	}
	for _, s := range strategies {
		for _, r := range s.Regimes {
			if !slices.Contains(labels, r) {
				return nil, fmt.Errorf("strategy %q declares unknown regime %q (valid: %v)", s.ID, r, labels)
			}
		}
	}

	// Text children differ ONLY by prompt template, which is also their cache
	// key: an empty or duplicated template would silently alias two children
	// onto one cached plan and score it twice.
	if kit == nil {
		seen := map[string]string{}
		for _, s := range strategies {
			if strings.TrimSpace(s.PromptTemplate) == "" {
				return nil, fmt.Errorf("text strategy %q declares no prompt template", s.ID)
			}
			if prev, dup := seen[s.PromptTemplate]; dup {
				return nil, fmt.Errorf("text strategies %q and %q share one prompt template", prev, s.ID)
			}
			seen[s.PromptTemplate] = s.ID
		}
	}

	buildChild := func(s pack.StrategyDescriptor, inner llm.Planner) (llm.Planner, error) {
		if kit != nil {
			return kit.BuildChild(d, pol, deps, s, inner)
		}
		return textChild(d, s), nil
	}
	// Narration is a kit concern (finance hybrid mode); text children already
	// produce complete summaries.
	var narrator llm.Planner
	if kit != nil {
		narrator = deps.FinanceInner
	}

	if len(strategies) == 1 {
		// Exactly one strategy left (policy disabled the rest): pin that lens
		// as the single planner — no competition, but its tuning still applies.
		return buildChild(strategies[0], narrator)
	}

	children := make([]llm.StrategyChild, 0, len(strategies))
	for _, s := range strategies {
		child, err := buildChild(s, nil)
		if err != nil {
			return nil, err
		}
		children = append(children, llm.StrategyChild{ID: s.ID, Planner: child, Regimes: s.Regimes})
	}

	dp, hasPolicy := pol.For(d.ID)
	selCfg := llm.SelectorConfig{
		Children:       children,
		Inner:          narrator,
		PromptTemplate: d.PromptTemplate,
		Regime:         regimeFn,
		// The penalty quantum is the domain's materiality threshold, so a
		// disagreement haircut is always either zero or material.
		PenaltyStep: effectiveDelta(d, pol),
	}
	if hasPolicy && dp.Strategy != nil {
		selCfg.Weights = dp.Strategy.Weights
		if dp.Strategy.IncumbentMargin != nil {
			selCfg.IncumbentMargin = *dp.Strategy.IncumbentMargin
		}
		if dp.Strategy.Comparator == "verify" {
			// The reviewer is the domain's (or global) raw base when it can
			// verify. Wrapping a nil verifier is deliberate: review then fails
			// at decision time and the selector's all-or-nothing degradation
			// records WHY the competition ran unreviewed, instead of the build
			// silently downgrading the comparator.
			base := deps.Base
			if deps.BaseFor != nil {
				if p := deps.BaseFor(d.ID); p != nil {
					base = p
				}
			}
			verifier, _ := base.(llm.PlanVerifier)
			selCfg.Reviewer = llm.NewVerifyReviewer(verifier)
		}
	}
	return llm.NewSelectorPlanner(selCfg)
}

// buildFinanceChild builds one strategy lens as a finance planner: the lens's
// params overlay the domain's base scoring, its prior tilts apply, and its
// strategy-suffixed name keys the TTL cache separately per lens.
func buildFinanceChild(d pack.Descriptor, pol policy.Policy, deps PlannerDeps, provider marketdata.Provider, s pack.StrategyDescriptor, inner llm.Planner) llm.Planner {
	dp, hasPolicy := pol.For(d.ID)
	params := strategyParams(s, dp, hasPolicy)
	base := effectiveScoring(d, pol).Normalize()
	cfg := llm.FinanceConfig{
		Provider:       provider,
		Scoring:        params.Apply(base),
		Inner:          inner,
		Now:            deps.FinanceNow,
		PackID:         d.ID,
		PackVersion:    d.Version,
		PromptTemplate: d.PromptTemplate,
		StrategyID:     s.ID,
		PriorWeights:   params.Prior,
		// All lenses state confidence on the domain's BASE weighting, so the
		// selector compares like with like (same scale, different evidence).
		ConfidenceWeights: base.Weights,
	}
	if hasPolicy && dp.Calibration != nil {
		cfg.Calibration = dp.Calibration
	}
	child := llm.Planner(llm.NewFinancePlanner(cfg))
	if deps.FinanceCache != nil {
		child = llm.NewCachingPlanner(child, deps.FinanceCache, deps.CacheObs)
	}
	return child
}

// financeRegimeFn classifies the goal-level market regime from one year of
// point-in-time bars per ticker asset, as of the (possibly simulated) clock.
// Fetch failures and thin history simply leave that ticker unknown — an
// unknown regime gates nothing, so degraded data can only widen the field,
// never narrow it. The regime is recorded in provenance either way, which is
// what makes per-regime outcome fitting possible later.
func financeRegimeFn(provider marketdata.Provider, now func() time.Time) llm.RegimeFn {
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context, g domain.Goal) (string, error) {
		asOf := now()
		var readings []finance.RegimeReading
		for _, a := range g.Context.Assets {
			if !strings.EqualFold(a.Kind, "ticker") {
				continue
			}
			bars, err := provider.HistoricalBars(ctx, strings.ToUpper(strings.TrimSpace(a.Name)), asOf.AddDate(-1, 0, 0), asOf)
			if err != nil {
				continue
			}
			readings = append(readings, finance.ClassifyRegime(bars))
		}
		return string(finance.CombineRegimes(readings)), nil
	}
}

// strategyParams resolves one strategy's numeric tuning: the descriptor's
// opaque Scoring (when it holds *finance.StrategyParams) is the base, and a
// policy override MERGES onto it field-by-field — a policy that tunes one
// knob keeps the lens's other parameters (its prior tilts in particular)
// instead of silently resetting them.
func strategyParams(s pack.StrategyDescriptor, dp policy.DomainPolicy, hasPolicy bool) finance.StrategyParams {
	params := finance.StrategyParams{Name: s.ID}
	if p, ok := s.Scoring.(*finance.StrategyParams); ok && p != nil {
		params = *p
	}
	if hasPolicy && dp.Strategy != nil {
		if p, ok := dp.Strategy.Params[s.ID]; ok && p != nil {
			params = params.Merge(*p)
		}
	}
	return params
}

// marketDataKey is the DataSources registry key for the market-data provider.
const marketDataKey = "marketdata"

// effectiveScoring resolves a domain's finance scoring config: a policy override
// wins, else the descriptor's opaque Scoring (if it holds a *finance.ScoringConfig).
// A zero config is fine — NewFinancePlanner substitutes DefaultScoringConfig().
func effectiveScoring(d pack.Descriptor, pol policy.Policy) finance.ScoringConfig {
	if dp, ok := pol.For(d.ID); ok && dp.Scoring != nil {
		return *dp.Scoring
	}
	if sc, ok := d.Scoring.(*finance.ScoringConfig); ok && sc != nil {
		return *sc
	}
	return finance.ScoringConfig{}
}
