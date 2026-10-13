package timeseries

import (
	"iter"
	"math"
)

// Cursor walks a TimeSeries without copying its data. Position, index bounds,
// and average caches belong to the cursor. Range and Tail create cursors with
// fixed bounds; Cursor creates a full-series cursor that also sees appended
// samples. Use separate cursors for nested or concurrent traversal; a single
// cursor is not reentrant or safe for concurrent use. Do not mutate a series
// while traversing it.
type Cursor struct {
	series   *TimeSeries
	start    int
	end      int
	index    int
	avg      map[int]average
	revision uint64
}

type average struct {
	index int
	sum   uint64
}

// Cursor creates an independent full-series cursor at sample zero. No values
// are copied. Its end follows the series length, including appended samples.
func (ts *TimeSeries) Cursor() *Cursor {
	ts.validate()
	return ts.newCursor(0, math.MaxInt)
}

func (ts *TimeSeries) newCursor(start, end int) *Cursor {
	return &Cursor{series: ts, start: start, end: end, index: start, revision: ts.revision}
}

func (c *Cursor) bounds() (int, int) {
	end := min(c.end, c.series.Size())
	return min(c.start, end), end
}

// StartIndex returns the inclusive start index in the original series.
// If the series has become shorter, the result is clamped to its current size.
func (c *Cursor) StartIndex() int {
	start, _ := c.bounds()
	return start
}

// EndIndex returns the exclusive end index in the original series. Fixed
// bounds are clamped if the series becomes shorter. A full-series cursor's
// end follows the current series length.
func (c *Cursor) EndIndex() int {
	_, end := c.bounds()
	return end
}

// Index returns the current absolute sample index, initially StartIndex().
func (c *Cursor) Index() int { return c.index }

// Time returns the current Unix timestamp in seconds. If there is no sample
// at the current position, it returns the cursor's starting boundary time.
func (c *Cursor) Time() int64 {
	start, end := c.bounds()
	index := c.index
	if index < start || index >= end {
		index = start
	}
	return c.series.Time0 + int64(index)*c.series.Step
}

// Value returns the current value, or NaN if the position is outside the
// cursor's available sample range.
func (c *Cursor) Value() float64 {
	start, end := c.bounds()
	if c.index < start || c.index >= end {
		return math.NaN()
	}
	return c.series.value(c.index)
}

// SeekTime moves to the interval containing a literal nonnegative Unix
// timestamp, clamped to this cursor's range. An empty cursor stays at its
// starting boundary index.
func (c *Cursor) SeekTime(timestamp int64) *Cursor {
	ts := c.series
	ts.validate()
	if timestamp < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if timestamp <= ts.Time0 {
		return c.SeekIndex(0)
	}
	if timestamp >= ts.NextTime() {
		return c.SeekIndex(ts.Size())
	}
	return c.SeekIndex(int((timestamp - ts.Time0) / ts.Step))
}

// SeekIndex moves to a nonnegative absolute sample index, clamped to this
// cursor's range. An empty cursor stays at its starting boundary index.
func (c *Cursor) SeekIndex(index int) *Cursor {
	if index < 0 {
		panic("timeseries: cursor index must be nonnegative")
	}
	start, end := c.bounds()
	c.index = max(start, min(index, end-1))
	return c
}

// All iterates this cursor's available range, updating its position before
// each yield. Each traversal starts at StartIndex(). Other cursors are
// unchanged. Breaking the loop leaves the cursor on the last visited sample;
// an empty range leaves its position unchanged. The loop body must not mutate
// the series.
func (c *Cursor) All() iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts := c.series
		ts.validate()
		start, end := c.bounds()
		scale, exp, step := ts.scale(), ts.Exp, ts.Step
		for i, t := start, ts.Time0+int64(start)*step; i < end; i, t = i+1, t+step {
			c.index = i
			if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
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
	start, end := c.bounds()
	if c.index < start || c.index >= end || c.index < n-1 {
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
