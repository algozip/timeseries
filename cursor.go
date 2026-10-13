package timeseries

import (
	"iter"
	"math"
)

// Cursor walks a TimeSeries without copying its data. Position and average
// caches belong to the cursor, so separate cursors can walk the same immutable
// series independently. A cursor starts at the first sample. Use separate
// cursors for nested or concurrent traversal; a single cursor is not reentrant
// or safe for concurrent use. Do not mutate a series while traversing it.
type Cursor struct {
	series   *TimeSeries
	index    int
	avg      map[int]average
	revision uint64
}

type average struct {
	index int
	sum   uint64
}

// Cursor creates an independent cursor at the first sample. No values are
// copied. A cursor over a reduced, sliced, or cloned series also starts at zero.
func (ts *TimeSeries) Cursor() *Cursor {
	ts.validate()
	return &Cursor{series: ts, revision: ts.revision}
}

// Index returns the current sample index, initially zero even for an empty series.
func (c *Cursor) Index() int { return c.index }

// Time returns the current Unix timestamp in seconds, or Time0 if no sample
// is available at the current index.
func (c *Cursor) Time() int64 {
	if c.index >= c.series.Size() {
		return c.series.Time0
	}
	return c.series.Time0 + int64(c.index)*c.series.Step
}

// Value returns the current value, or NaN if no sample is available.
func (c *Cursor) Value() float64 {
	if c.index >= c.series.Size() {
		return math.NaN()
	}
	return c.series.value(c.index)
}

// SeekTime moves to the interval containing a literal nonnegative Unix
// timestamp, clamped to the series. An empty series keeps index zero.
func (c *Cursor) SeekTime(timestamp int64) *Cursor {
	ts := c.series
	ts.validate()
	if timestamp < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	switch {
	case ts.Size() == 0 || timestamp <= ts.Time0:
		c.index = 0
	case timestamp >= ts.NextTime():
		c.index = ts.Size() - 1
	default:
		c.index = int((timestamp - ts.Time0) / ts.Step)
	}
	return c
}

// SeekIndex moves to a nonnegative sample index, clamped to the series.
// An empty series keeps index zero.
func (c *Cursor) SeekIndex(index int) *Cursor {
	if index < 0 {
		panic("timeseries: cursor index must be nonnegative")
	}
	c.index = min(index, max(0, c.series.Size()-1))
	return c
}

// All iterates all samples, updating this cursor before each yield. Each
// traversal starts at the first sample. Other cursors are unchanged.
// Breaking the loop leaves the cursor on the last visited sample.
func (c *Cursor) All() iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		c.Range(c.series.Time0, c.series.NextTime())(yield)
	}
}

// Range iterates sample timestamps in [from, to), updating this cursor before
// each yield. Bounds are literal nonnegative Unix seconds, matching
// TimeSeries.Range. Other cursors are unchanged. Breaking the loop leaves this
// cursor on the last visited sample. An empty range leaves its position unchanged.
func (c *Cursor) Range(from, to int64) iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts := c.series
		start, end := ts.rangeIndices(from, to)
		scale, exp, step := ts.scale(), ts.Exp, ts.Step
		for i, t := start, ts.Time0+int64(start)*step; i < end; i, t = i+1, t+step {
			c.index = i
			if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// Tail iterates the last period seconds, updating only this cursor.
// A zero period yields nothing; a negative period panics.
func (c *Cursor) Tail(period Period) iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		c.Range(c.series.tailStart(period), c.series.NextTime())(yield)
	}
}

// Avg returns the simple moving average ending at this cursor. period is in
// seconds, rounded down to whole samples with a minimum of one. Negative
// periods panic. NaN is returned until a complete window is available.
// Sequential calls reuse a running sum per window. Changes to existing values
// or precision invalidate caches; appending values keeps them reusable.
// Earlier samples outside a cursor's iteration range are included in the window.
func (c *Cursor) Avg(period Period) float64 {
	ts := c.series
	ts.validate()
	if period < 0 {
		panic("timeseries: average period must be nonnegative")
	}
	if c.revision != ts.revision {
		c.avg = nil
		c.revision = ts.revision
	}
	n := max(1, int(int64(period)/ts.Step))
	if c.index >= ts.Size() || c.index < n-1 {
		return math.NaN()
	}
	a, ok := c.avg[n]
	if !ok || c.index < a.index || c.index-a.index >= n {
		a = average{index: c.index}
		for _, p := range ts.prices[c.index-n+1 : c.index+1] {
			a.sum += uint64(p)
		}
	} else {
		for a.index < c.index {
			a.index++
			a.sum -= uint64(ts.prices[a.index-n])
			a.sum += uint64(ts.prices[a.index])
		}
	}
	if c.avg == nil {
		c.avg = make(map[int]average)
	}
	c.avg[n] = a
	value := float64(a.sum) / float64(n)
	if ts.Exp < 0 {
		return value / ts.scale()
	}
	return value * ts.scale()
}
