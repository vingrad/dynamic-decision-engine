package finance

// PositionSize is an illustrative sizing recommendation (a fraction of account
// equity), bounded by the risk budget. It is not investment advice.
type PositionSize struct {
	SuggestedFraction float64 `json:"suggested_fraction"`
	SizingMethod      string  `json:"sizing_method"`         // "fractional_kelly" | "vol_target"
	BindingCap        string  `json:"binding_cap,omitempty"` // which cap clamped it, if any
}

// ThesisScore is the decomposed, transparent score for a single candidate thesis.
// Every sub-score is in [0,1]; Composite is their weighted blend.
type ThesisScore struct {
	Ticker             string       `json:"ticker,omitempty"`
	ExpectedValueScore float64      `json:"expected_value_score"`
	RiskScore          float64      `json:"risk_score"` // higher == safer (lower vol/drawdown)
	LiquidityFitScore  float64      `json:"liquidity_fit_score"`
	HorizonFitScore    float64      `json:"horizon_fit_score"`
	Composite          float64      `json:"composite"`
	Position           PositionSize `json:"position"`
	Explain            string       `json:"explain"`
}
