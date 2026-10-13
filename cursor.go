package timeseries

import (
	"iter"
	"math"
	"time"
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

// Time returns the current timestamp in UTC. If there is no sample
// at the current position, it returns the cursor's starting boundary time.
func (c *Cursor) Time() time.Time {
	start, end := c.bounds()
	index := c.index
	if index < start || index >= end {
		index = start
	}
	return nanoTime(c.series.time0 + int64(index)*int64(c.series.step))
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

// Seek moves to the interval containing timestamp, clamped to this cursor's
// range. An empty cursor stays at its starting boundary index.
func (c *Cursor) Seek(at time.Time) *Cursor {
	timestamp := unixNano(at)
	ts := c.series
	ts.validate()
	start, end := c.bounds()
	index := start
	if timestamp >= ts.nextTime() {
		index = end - 1
	} else if timestamp > ts.time0 {
		index = int((timestamp - ts.time0) / int64(ts.step))
	}
	c.index = max(start, min(index, end-1))
	return c
}

// All iterates this cursor's available range, updating its position before
// each yield. Each traversal starts at StartIndex(). Other cursors are
// unchanged. Breaking the loop leaves the cursor on the last visited sample;
// an empty range leaves its position unchanged. The loop body must not mutate
// the series.
func (c *Cursor) All() iter.Seq2[time.Time, float64] {
	return func(yield func(time.Time, float64) bool) {
		ts := c.series
		ts.validate()
		start, end := c.bounds()
		scale, exp, step := ts.scale(), ts.Exp, int64(ts.step)
		t0 := ts.time0 + int64(start)*step
		if step%int64(time.Second) == 0 {
			step /= int64(time.Second)
			nanos := t0 % int64(time.Second)
			for i, seconds := start, t0/int64(time.Second); i < end; i, seconds = i+1, seconds+step {
				c.index = i
				if !yield(time.Unix(seconds, nanos).UTC(), decodeValue(ts.prices[i], exp, scale)) {
					return
				}
			}
			return
		}
		for i, t := start, nanoTime(t0); i < end; i, t = i+1, t.Add(time.Duration(step)) {
			c.index = i
			if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// AllUnix iterates this cursor's range using Unix seconds, matching
// time.Time.Unix. Subsecond timestamps are truncated and may repeat; use All
// for full precision. Position and stopping behavior match All. The loop body
// must not mutate the series.
func (c *Cursor) AllUnix() iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts := c.series
		ts.validate()
		start, end := c.bounds()
		scale, exp, step := ts.scale(), ts.Exp, int64(ts.step)
		t0 := ts.time0 + int64(start)*step
		if step%int64(time.Second) == 0 {
			step /= int64(time.Second)
			for i, t := start, t0/int64(time.Second); i < end; i, t = i+1, t+step {
				c.index = i
				if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
					return
				}
			}
			return
		}
		for i, t := start, t0; i < end; i, t = i+1, t+step {
			c.index = i
			if !yield(t/int64(time.Second), decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// Avg returns the simple moving average ending at this cursor. The duration
// is rounded down to whole samples with a minimum of one. Negative
// periods panic. NaN is returned until a complete window is available.
// Sequential calls reuse a running sum per window. Changes to existing values
// or precision invalidate caches; appending values keeps them reusable.
// Earlier samples outside a cursor's iteration range are included in the window.
func (c *Cursor) Avg(period time.Duration) float64 {
	ts := c.series
	ts.validate()
	if period < 0 {
		panic("timeseries: average period must be nonnegative")
	}
	if c.revision != ts.revision {
		c.avg = nil
		c.revision = ts.revision
	}
	window := period / ts.step
	if window > time.Duration(math.MaxInt) {
		return math.NaN()
	}
	n := max(1, int(window))
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
