package finance

import (
	"regexp"
	"strconv"
	"strings"
)

var horizonRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(day|week|month|quarter|year|yr|d|w|mo|y)s?`)

// ParseHorizonDays extracts an approximate number of days from a free-text horizon
// such as "2 years", "18 months", "6 weeks", "30 days", "1y". Returns 0 when no
// duration can be found. Used to feed HorizonFit so the goal's stated horizon
// actually influences scoring.
func ParseHorizonDays(s string) int {
	m := horizonRe.FindStringSubmatch(strings.ToLower(s))
	if m == nil {
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n <= 0 {
		return 0
	}
	var perUnit float64
	switch m[2] {
	case "day", "d":
		perUnit = 1
	case "week", "w":
		perUnit = 7
	case "month", "mo":
		perUnit = 30
	case "quarter":
		perUnit = 91
	case "year", "yr", "y":
		perUnit = 365
	default:
		return 0
	}
	return int(n * perUnit)
}
