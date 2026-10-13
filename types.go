package timeseries

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
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

// Tick is a timestamped quote. Time is a Unix timestamp in seconds.
// TimeSeries stores Ask only; returned ticks have equal Ask and Bid values.
type Tick struct {
	Time int64
	Ask  float64
	Bid  float64
}

// ShortString formats the timestamp in UTC and the price.
func (t Tick) ShortString() string {
	return fmt.Sprintf("[%s %g]", time.Unix(t.Time, 0).UTC().Format("2006-01-02 15:04:05"), t.Ask)
}

// Ticks is a slice of timestamped quotes.
type Ticks []Tick

// TimeSeries converts ticks to a fixed-interval series. Input must be ordered
// by time. Gaps are filled with the previous price; the last tick in a bucket wins.
func (ticks Ticks) TimeSeries(period Period) *TimeSeries {
	exp := 0
	for _, tick := range ticks {
		exp = min(exp, PrecisionExp(tick.Ask))
	}
	ts := New(0, period, exp)
	for _, tick := range ticks {
		ts.Push(tick)
	}
	return ts
}

// PrecisionExp returns the decimal exponent of the shortest decimal
// representation of v. Zero has exponent zero. Non-finite values panic.
func PrecisionExp(v float64) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		panic("timeseries: price must be finite")
	}
	if v == 0 {
		return 0
	}
	s := strconv.FormatFloat(math.Abs(v), 'f', -1, 64)
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		return -(len(s) - dot - 1)
	}
	return len(s) - len(strings.TrimRight(s, "0"))
}
