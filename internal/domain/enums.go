package domain

// Level is a qualitative low/medium/high rating used for expected impact,
// effort and risk. Keeping these as a small typed enum avoids free-form strings
// leaking through the API and storage layers.
type Level string

const (
	LevelLow    Level = "low"
	LevelMedium Level = "medium"
	LevelHigh   Level = "high"
)

// Valid reports whether the level is one of the recognised values.
func (l Level) Valid() bool {
	switch l {
	case LevelLow, LevelMedium, LevelHigh:
		return true
	default:
		return false
	}
}
