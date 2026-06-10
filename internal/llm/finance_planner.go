package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vingrad/dynamic-decision-engine/internal/domain"
	"github.com/vingrad/dynamic-decision-engine/internal/finance"
	"github.com/vingrad/dynamic-decision-engine/internal/marketdata"
)

// financePromptVersion identifies the finance planner's scoring contract.
const financePromptVersion = "finance-v1"

// intendedNotional is the illustrative position notional used for liquidity fit.
// It is a fixed placeholder, not a real account size — see finance/doc.go.
const intendedNotional = 1_000_000.0

// FinancePlanner is a deterministic, numeric planner for the investing domain. It
// scores candidate theses from point-in-time market data and the goal's risk
// budget, mapping transparent composite scores onto ranked moves. The LLM, if any
// (hybrid mode), only narrates; the numbers own rank, confidence and sizing.
//
// It is NOT a trading system. See internal/finance/doc.go.
type FinancePlanner struct {
	provider       marketdata.Provider
	scoring        finance.ScoringConfig
	inner          Planner // optional: thesis narration only
	now            func() time.Time
	packID         string
	packVersion    string
	promptTemplate string
}

// FinanceConfig configures the finance planner.
type FinanceConfig struct {
	Provider    marketdata.Provider
	Scoring     finance.ScoringConfig
	Inner       Planner          // optional hybrid narration
	Now         func() time.Time // defaults to time.Now; backtests inject sim time
	PackID      string
	PackVersion string
	// PromptTemplate is the pack's domain guidance. The numeric path ignores it;
	// in hybrid mode it becomes the inner narrator's system prompt override.
	PromptTemplate string
}

// NewFinancePlanner constructs the planner with sane defaults.
func NewFinancePlanner(cfg FinanceConfig) *FinancePlanner {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Scoring.Weights == (finance.ScoreWeights{}) && cfg.Scoring.Risk == (finance.RiskBudget{}) {
		cfg.Scoring = finance.DefaultScoringConfig()
	}
	if cfg.PackID == "" {
		cfg.PackID = "investing"
	}
	return &FinancePlanner{
		provider:       cfg.Provider,
		scoring:        cfg.Scoring,
		inner:          cfg.Inner,
		now:            cfg.Now,
		packID:         cfg.PackID,
		packVersion:    cfg.PackVersion,
		promptTemplate: cfg.PromptTemplate,
	}
}

// Name implements Planner.
func (*FinancePlanner) Name() string { return "finance" }

const financeDisclaimer = "Educational decision-support only. Not financial advice. Not a recommendation to buy or sell any security."

// GeneratePlan implements Planner.
func (p *FinancePlanner) GeneratePlan(ctx context.Context, req PlanRequest) (PlanResult, error) {
	g := req.Goal
	if g.Objective == "" {
		return PlanResult{}, fmt.Errorf("llm: goal objective is required")
	}
	asOf := p.now()

	// Parse the structured signal, if any. The kind comes from the signal itself;
	// the payload map may also carry it as a fallback.
	var sig *finance.MarketSignal
	if len(req.SignalPayload) > 0 {
		kind := req.SignalKind
		if kind == "" {
			kind = stringField(req.SignalPayload, "kind")
		}
		if ms, err := finance.ParseMarketSignal(kind, req.SignalPayload); err == nil {
			sig = &ms
		}
	}

	// Per-goal constraints (drawdown_limit, risk_tolerance) tighten the
	// configured risk budget before any sizing happens.
	budget, budgetNote := finance.EffectiveRiskBudget(p.scoring.Risk, g.Context.Constraints)

	tickers := candidateTickers(g, sig)
	moves := make([]domain.RankedMove, 0, len(tickers))
	for _, ticker := range tickers {
		moves = append(moves, p.scoreThesis(ctx, ticker, g, sig, asOf, budget, budgetNote))
	}
	if len(moves) == 0 {
		moves = append(moves, insufficientDataMove(g))
	}

	// Rank by confidence (which derives from the composite score) descending.
	sort.SliceStable(moves, func(i, j int) bool { return moves[i].Confidence > moves[j].Confidence })
	for i := range moves {
		moves[i].Rank = i + 1
	}

	summary := fmt.Sprintf("Ranked, numerically-scored theses toward %q. %s", g.Objective, financeDisclaimer)
	reasoning := "Each thesis is scored on expected value, risk (volatility/drawdown), liquidity and horizon fit; sizing is illustrative fractional-Kelly bounded by the risk budget."

	// Hybrid: let an inner model add narrative colour without touching the numbers.
	// The narrator sees the pack's domain guidance and the numeric scores it is
	// annotating; its output only ever lands in the reasoning text.
	var contributors []domain.ModelContribution
	if p.inner != nil {
		override := req.SystemPromptOverride
		if override == "" {
			override = p.promptTemplate
		}
		r, err := p.inner.GeneratePlan(ctx, PlanRequest{
			Goal:                 g,
			SignalNote:           narrationNote(req.SignalNote, moves),
			SystemPromptOverride: override,
		})
		if err == nil && r.Summary != "" {
			reasoning = r.Summary + " " + reasoning
			contributors = append(contributors, domain.ModelContribution{
				Planner:          p.inner.Name(),
				Model:            r.Invocation.Model,
				Role:             "narrator",
				PromptTokens:     r.Invocation.PromptTokens,
				CompletionTokens: r.Invocation.CompletionTokens,
			})
		}
	}

	prov := domain.DecisionProvenance{
		ReasoningSummary: reasoning,
		InputSnapshotID:  inputSnapshotID(g, req.SignalNote),
		Planner:          "finance",
		PromptVersion:    financePromptVersion,
		Model:            "none",
		Strategy:         "single",
		PackID:           p.packID,
		PackVersion:      p.packVersion,
		Contributors:     contributors,
	}
	return PlanResult{
		Summary:     summary,
		RankedMoves: moves,
		Provenance:  prov,
		Invocation:  domain.ModelInvocation{Model: "none", PromptVersion: financePromptVersion},
	}, nil
}

// scoreThesis builds a fully-scored ranked move for one ticker.
func (p *FinancePlanner) scoreThesis(ctx context.Context, ticker string, g domain.Goal, sig *finance.MarketSignal, asOf time.Time, budget finance.RiskBudget, budgetNote string) domain.RankedMove {
	var (
		vol, maxDD, avgDollarVol float64
	)
	if q, err := p.provider.Quote(ctx, ticker, asOf); err == nil {
		avgDollarVol = q.AvgDollarVolume
	}
	if bars, err := p.provider.HistoricalBars(ctx, ticker, asOf.AddDate(-1, 0, 0), asOf); err == nil {
		vol = finance.Volatility(finance.Returns(bars))
		maxDD = finance.MaxDrawdown(bars)
	}

	// A signal hint informs the win probability only for the ticker it names
	// (an unnamed signal, e.g. macro, applies to every candidate).
	winProb := 0.5
	informed := false
	if sig != nil && (sig.Ticker == "" || strings.EqualFold(sig.Ticker, ticker)) {
		if wp, ok := sig.WinProbHint(); ok {
			winProb = wp
			informed = true
		}
	}
	lossFrac := vol
	if lossFrac < 0.05 {
		lossFrac = 0.05 // floor so sizing/EV remain well-defined on quiet fixtures
	}
	ratio := p.scoring.RewardRiskRatio
	if ratio <= 0 {
		ratio = 2.0
	}
	winFrac := ratio * lossFrac // configurable reward-to-risk assumption

	// Intended position notional drives liquidity fit. When account equity is known
	// it is equity * the concentration cap; otherwise fall back to a fixed notional.
	notional := intendedNotional
	if eq := budget.AccountEquity; eq > 0 && budget.MaxPositionPct > 0 {
		notional = eq * budget.MaxPositionPct
	}

	// With no signal hint the win probability is a flat prior, and the
	// volatility-scaled EV would rank the most volatile name highest.
	evScore := finance.NeutralEVScore
	if informed {
		evScore = finance.EVScore(finance.ExpectedValue(winProb, winFrac, lossFrac))
	}

	score := finance.ThesisScore{
		Ticker:             ticker,
		ExpectedValueScore: evScore,
		RiskScore:          finance.RiskScore(vol, maxDD),
		LiquidityFitScore:  finance.LiquidityFit(avgDollarVol, notional),
		HorizonFitScore:    finance.HorizonFit(goalHorizonDays(g), signalWindow(sig)),
	}
	score.Composite = finance.Composite(score, p.scoring.Weights)
	score.Position = finance.PositionFractionKelly(winProb, winFrac, lossFrac, budget)
	score.Explain = fmt.Sprintf(
		"ev=%.2f risk=%.2f liq=%.2f horizon=%.2f -> composite=%.2f; suggested size %.0f%% of equity (%s%s)",
		score.ExpectedValueScore, score.RiskScore, score.LiquidityFitScore, score.HorizonFitScore,
		score.Composite, score.Position.SuggestedFraction*100, score.Position.SizingMethod, capSuffix(score.Position.BindingCap),
	)
	if budgetNote != "" {
		score.Explain += "; risk budget per goal constraints: " + budgetNote
	}

	impact, effort, risk := finance.MapToLevels(score)
	confidence := finance.CompositeToConfidence(score.Composite)
	stopPct := lossFrac * 100

	move := domain.RankedMove{
		Key:            "thesis:" + ticker,
		Title:          "Thesis: " + ticker,
		Description:    fmt.Sprintf("Position in %s sized to the risk budget; entry on confirmation, exit on thesis invalidation.", ticker),
		Confidence:     clampConfidence(confidence),
		ExpectedImpact: impact,
		Effort:         effort,
		Risk:           risk,
		Rationale:      score.Explain,
		Experiment: domain.Experiment{
			Title:        "Initiate a starter position in " + ticker,
			DurationDays: 30,
			SuccessSignals: []string{
				"Thesis driver confirmed by subsequent data",
				"Position performs in line with the expected-value case",
			},
			KillCriteria: []string{
				fmt.Sprintf("Price breaches the ~%.0f%% stop distance", stopPct),
				"A thesis-invalidation event occurs (the claim is proven wrong)",
			},
		},
		FallbackMoves: []string{"Reduce to a half-size starter", "Wait for a better entry"},
		// Each thesis is an independent position; theses carry no inter-dependencies
		// and can all be pursued concurrently within the portfolio.
		ParallelGroup: "portfolio",
	}

	// A thesis-break invalidates the thesis: encode it so the standard materiality
	// evaluator naturally fires (top move/confidence changes). A break with no ticker
	// is untargeted and invalidates every candidate thesis.
	if sig != nil && sig.IsThesisBreak() && (sig.Ticker == "" || strings.EqualFold(sig.Ticker, ticker)) {
		move.Confidence = 0
		move.Risk = domain.LevelHigh
		move.ExpectedImpact = domain.LevelLow
		move.Rationale = "Thesis invalidated: " + sig.ThesisBreak.Reason + ". " + move.Rationale
	}
	return move
}

// narrationNote builds the inner narrator's signal note: the triggering signal
// (if any) plus the authoritative numeric scores, so the narrative can reference
// the numbers it annotates without ever owning them.
func narrationNote(signalNote string, moves []domain.RankedMove) string {
	var b strings.Builder
	if signalNote != "" {
		b.WriteString(signalNote)
		b.WriteString("\n")
	}
	b.WriteString("Numeric thesis scores (authoritative; narrate, do not change them):")
	for _, m := range moves {
		b.WriteString("\n- ")
		b.WriteString(m.Title)
		b.WriteString(": ")
		b.WriteString(m.Rationale)
	}
	return b.String()
}

// candidateTickers gathers tickers from goal context assets (Kind=="ticker") and
// the triggering signal, de-duplicated and order-stable.
func candidateTickers(g domain.Goal, sig *finance.MarketSignal) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		t = strings.ToUpper(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, a := range g.Context.Assets {
		if strings.EqualFold(a.Kind, "ticker") {
			add(a.Name)
		}
	}
	if sig != nil {
		add(sig.Ticker)
	}
	return out
}

// goalHorizonDays extracts an approximate horizon (in days) from the goal's
// context — a constraint or asset of kind "time_horizon" — so the horizon-fit score
// reflects the goal the investing pack asks callers to state. Returns 0 if absent.
func goalHorizonDays(g domain.Goal) int {
	for _, c := range g.Context.Constraints {
		if strings.EqualFold(c.Kind, "time_horizon") {
			if d := finance.ParseHorizonDays(c.Description); d > 0 {
				return d
			}
			if d := finance.ParseHorizonDays(c.Name); d > 0 {
				return d
			}
		}
	}
	for _, a := range g.Context.Assets {
		if strings.EqualFold(a.Kind, "time_horizon") {
			if d := finance.ParseHorizonDays(a.Description); d > 0 {
				return d
			}
			if d := finance.ParseHorizonDays(a.Name); d > 0 {
				return d
			}
		}
	}
	return 0
}

func signalWindow(sig *finance.MarketSignal) int {
	if sig != nil && sig.PriceMove != nil {
		return sig.PriceMove.WindowDays
	}
	return 0
}

func insufficientDataMove(g domain.Goal) domain.RankedMove {
	return domain.RankedMove{
		Key:            "insufficient-market-data",
		Title:          "Insufficient market data",
		Description:    fmt.Sprintf("No tickers were provided for %q, so no thesis could be scored.", g.Objective),
		Confidence:     0.2,
		ExpectedImpact: domain.LevelLow,
		Effort:         domain.LevelLow,
		Risk:           domain.LevelLow,
		Rationale:      "Add assets with kind \"ticker\" (or a signal naming a ticker) to enable numeric scoring.",
		Experiment: domain.Experiment{
			Title:          "Specify the investable universe",
			DurationDays:   1,
			SuccessSignals: []string{"At least one ticker is available to score"},
			KillCriteria:   []string{"No instruments can be identified for this goal"},
		},
		FallbackMoves: []string{"Use the qualitative investing planner instead"},
	}
}

func capSuffix(bound string) string {
	if bound == "" {
		return ""
	}
	return ", capped by " + bound
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
