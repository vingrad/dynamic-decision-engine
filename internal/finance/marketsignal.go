package finance

import (
	"encoding/json"
	"fmt"
)

// SignalKind enumerates the structured investing signals carried in a
// domain.Signal.Payload map.
type SignalKind string

const (
	KindPriceMove       SignalKind = "price_move"
	KindEarnings        SignalKind = "earnings"
	KindMacro           SignalKind = "macro"
	KindValuationChange SignalKind = "valuation_change"
	KindThesisBreak     SignalKind = "thesis_break"
)

// Payload structs mirror the keys expected inside domain.Signal.Payload.

type PriceMovePayload struct {
	PctChange  float64 `json:"pct_change"`
	FromPrice  float64 `json:"from_price"`
	ToPrice    float64 `json:"to_price"`
	WindowDays int     `json:"window_days"`
}

type EarningsPayload struct {
	EpsActual   float64 `json:"eps_actual"`
	EpsEstimate float64 `json:"eps_estimate"`
	Surprise    float64 `json:"surprise"` // fractional surprise, e.g. 0.10 == +10%
}

type MacroPayload struct {
	Indicator string  `json:"indicator"`
	Value     float64 `json:"value"`
	Prior     float64 `json:"prior"`
}

type ValuationPayload struct {
	Metric    string  `json:"metric"`
	Value     float64 `json:"value"`
	FairValue float64 `json:"fair_value"`
	GapPct    float64 `json:"gap_pct"` // (fair-value - value)/value; >0 == undervalued
}

type ThesisBreakPayload struct {
	Reason      string  `json:"reason"`
	BrokenLevel float64 `json:"broken_level"`
	Hard        bool    `json:"hard"`
}

// MarketSignal is the typed form of a structured investing signal. Exactly one of
// the payload pointers is set, matching Kind.
type MarketSignal struct {
	Kind        SignalKind
	Ticker      string
	PriceMove   *PriceMovePayload
	Earnings    *EarningsPayload
	Macro       *MacroPayload
	Valuation   *ValuationPayload
	ThesisBreak *ThesisBreakPayload
}

// ParseMarketSignal converts a kind + raw payload map into a typed MarketSignal by
// round-tripping the map through JSON into the matching payload struct.
func ParseMarketSignal(kind string, payload map[string]any) (MarketSignal, error) {
	ms := MarketSignal{Kind: SignalKind(kind)}
	if t, ok := payload["ticker"].(string); ok {
		ms.Ticker = t
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ms, fmt.Errorf("finance: marshal signal payload: %w", err)
	}
	switch ms.Kind {
	case KindPriceMove:
		var p PriceMovePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return ms, err
		}
		ms.PriceMove = &p
	case KindEarnings:
		var p EarningsPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return ms, err
		}
		ms.Earnings = &p
	case KindMacro:
		var p MacroPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return ms, err
		}
		ms.Macro = &p
	case KindValuationChange:
		var p ValuationPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return ms, err
		}
		ms.Valuation = &p
	case KindThesisBreak:
		var p ThesisBreakPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return ms, err
		}
		ms.ThesisBreak = &p
	default:
		return ms, fmt.Errorf("finance: unknown signal kind %q", kind)
	}
	return ms, nil
}

// IsThesisBreak reports whether the signal invalidates the thesis (forcing a
// material replan downstream).
func (m MarketSignal) IsThesisBreak() bool { return m.Kind == KindThesisBreak }

// WinProbHint derives a rough win probability from the signal's data (valuation
// gap or earnings surprise), feeding ExpectedValue. It returns ok=false when no
// probability can be inferred. This is a heuristic prior, not a calibrated
// forecast — see the package doc.
func (m MarketSignal) WinProbHint() (prob float64, ok bool) {
	switch {
	case m.Valuation != nil:
		// Undervalued (positive gap) tilts probability above 0.5.
		return clamp01(0.5 + m.Valuation.GapPct/2), true
	case m.Earnings != nil:
		return clamp01(0.5 + m.Earnings.Surprise), true
	case m.PriceMove != nil:
		// A sharp drop with an intact thesis tilts mildly toward mean reversion.
		return clamp01(0.5 - m.PriceMove.PctChange/4), true
	default:
		return 0, false
	}
}
