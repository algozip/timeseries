package timeseries

import (
	"fmt"
	"math"
	"strconv"
)

// Period is a sampling interval in seconds.
type Period int64

const (
	Period1s Period = 1
	Period1m        = 60 * Period1s
	Period1h        = 60 * Period1m
	Period4h        = 4 * Period1h
	Period1D        = 24 * Period1h
)

func (p Period) String() string {
	for _, unit := range []struct {
		seconds Period
		suffix  string
	}{{Period1D, "d"}, {Period1h, "h"}, {Period1m, "m"}, {Period1s, "s"}} {
		if p > 0 && p%unit.seconds == 0 {
			return fmt.Sprintf("%d%s", p/unit.seconds, unit.suffix)
		}
	}
	return fmt.Sprintf("%ds", p)
}

// PeriodByString parses a positive integer followed by s, m, h, or d.
// It returns zero for invalid input.
func PeriodByString(s string) Period {
	if len(s) < 2 {
		return 0
	}
	units := map[byte]int64{'s': 1, 'm': 60, 'h': 3600, 'd': 86400}
	unit := units[s[len(s)-1]]
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || unit == 0 || n <= 0 || n > math.MaxInt64/unit {
		return 0
	}
	return Period(n * unit)
}

// PrecisionExp returns the original finance decimal exponent, ignoring
// absolute floating-point differences of at most 1e-15. v must be finite.
func PrecisionExp(v float64) (exp int) {
	if v = abs(v); v != 0 {
		exp = int(math.Floor(math.Log10(v)))
		for abs(math.Round(v/Pow10(exp))/Pow10(-exp)-v) > 1e-15 {
			exp--
		}
	}
	return
}

var pow10 = [...]float64{
	1e-20, 1e-19, 1e-18, 1e-17, 1e-16, 1e-15, 1e-14, 1e-13, 1e-12, 1e-11, 1e-10, 1e-9, 1e-8, 1e-7, 1e-6, 1e-5, 1e-4, 1e-3, 1e-2, 1e-1,
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20,
}

// Pow10 returns 10 raised to exp, using a table for exponents -20 through 20.
func Pow10(exp int) float64 {
	if exp >= -20 && exp <= 20 {
		return pow10[exp+20]
	}
	return math.Pow10(exp)
}

func abs[T int | int32 | int64 | float32 | float64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}
