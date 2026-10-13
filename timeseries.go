package timeseries

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"time"
)

// TimeSeries stores one nonnegative value per interval using four bytes per
// sample. Time metadata is held privately in nanoseconds. A stored integer p
// represents p * 10^Exp. Use SetExp to change precision after adding data.
// Iteration does not change the series. Use Cursor for position-based averages.
type TimeSeries struct {
	Exp int // Decimal exponent: -2 stores values to two decimal places.

	time0    int64
	step     time.Duration
	prices   []uint32
	revision uint64
}

// New creates an empty series with a positive step and an exponent between
// -308 and 308. start is used by Append; the first Push sets its own start.
// Times must be between the Unix epoch and the maximum int64 Unix nanosecond
// timestamp. Subsecond steps and timestamps are supported. Invalid input panics.
func New(start time.Time, step time.Duration, exp int) *TimeSeries {
	ts := &TimeSeries{time0: unixNano(start), step: step, Exp: exp}
	ts.validate()
	return ts
}

func (ts *TimeSeries) validate() {
	if ts.time0 < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if ts.step <= 0 {
		panic("timeseries: step must be positive")
	}
	if ts.Exp < -308 || ts.Exp > 308 {
		panic("timeseries: exponent must be between -308 and 308")
	}
}

// StartTime returns the series' start in UTC, including for an empty series.
func (ts *TimeSeries) StartTime() time.Time { return nanoTime(ts.time0) }

// Step returns the sampling interval.
func (ts *TimeSeries) Step() time.Duration { return ts.step }

// Duration returns the duration covered by all sample intervals, including
// the final interval. An empty series has duration zero.
func (ts *TimeSeries) Duration() time.Duration { return time.Duration(ts.Size()) * ts.step }

func (ts *TimeSeries) String() string {
	if ts.Size() == 0 {
		return "<TS-empty>"
	}
	return fmt.Sprintf("<TS-%s [%s %g] [%s %g]>", ts.step, ts.StartTime().Format(time.RFC3339Nano), ts.value(0), ts.LastTime().Format(time.RFC3339Nano), ts.value(ts.Size()-1))
}

// Size returns the number of samples. A nil series has size zero.
func (ts *TimeSeries) Size() int {
	if ts == nil {
		return 0
	}
	return len(ts.prices)
}

// Precision returns the smallest value increment represented by the series.
func (ts *TimeSeries) Precision() float64 { return Pow10(ts.Exp) }

func (ts *TimeSeries) nextTime() int64 { return ts.time0 + int64(ts.Duration()) }

// NextTime returns the exclusive end in UTC, or StartTime for an empty series.
func (ts *TimeSeries) NextTime() time.Time { return nanoTime(ts.nextTime()) }

func (ts *TimeSeries) lastTime() int64 {
	if ts.Size() == 0 {
		return ts.time0
	}
	return ts.nextTime() - int64(ts.step)
}

// LastTime returns the last sample time in UTC, or StartTime if empty.
func (ts *TimeSeries) LastTime() time.Time { return nanoTime(ts.lastTime()) }

func (ts *TimeSeries) checkLength(start, count int64) {
	if count < 0 || count > int64(math.MaxInt) || count > (math.MaxInt64-start)/int64(ts.step) {
		panic("timeseries: sample count or timestamp range overflows")
	}
}

func (ts *TimeSeries) scale() float64 { return Pow10(abs(ts.Exp)) }

func decodeValue(p uint32, exp int, scale float64) float64 {
	if exp < 0 {
		return float64(p) / scale
	}
	return float64(p) * scale
}

func (ts *TimeSeries) value(i int) float64 {
	return decodeValue(ts.prices[i], ts.Exp, ts.scale())
}

func (ts *TimeSeries) index(t int64) int {
	if t < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if ts.Size() == 0 || t < ts.time0 || t >= ts.nextTime() {
		return -1
	}
	return int((t - ts.time0) / int64(ts.step))
}

// ValueOf returns the value whose interval contains t, or NaN outside the
// series. Subsecond timestamps select the interval containing t.
func (ts *TimeSeries) ValueOf(t time.Time) float64 {
	if i := ts.index(unixNano(t)); i >= 0 {
		return ts.value(i)
	}
	return math.NaN()
}

// Clone creates an independent copy of the metadata and values.
func (ts *TimeSeries) Clone() *TimeSeries {
	return &TimeSeries{time0: ts.time0, step: ts.step, Exp: ts.Exp, prices: slices.Clone(ts.prices)}
}

// Reduce keeps the first sample of each larger interval, including a final
// partial interval. period must be a positive multiple of Step(); otherwise it
// panics. A larger period creates independent storage and preserves StartTime();
// an unchanged period returns the receiver. New cursors start at the first sample.
func (ts *TimeSeries) Reduce(period time.Duration) *TimeSeries {
	ts.validate()
	step := period
	if step < ts.step || step%ts.step != 0 {
		panic("timeseries: reduced period must be a positive multiple of step")
	}
	if step == ts.step {
		return ts
	}
	k := int64(step / ts.step)
	n := ts.Size()
	if n > 0 {
		n = int((int64(n)-1)/k + 1)
	}
	result := &TimeSeries{time0: ts.time0, step: step, Exp: ts.Exp}
	result.checkLength(result.time0, int64(n))
	result.prices = make([]uint32, n)
	for i := range result.prices {
		result.prices[i] = ts.prices[int64(i)*k]
	}
	return result
}

// Slice copies samples with timestamps in [from, to) into independent storage.
// Bounds use the same time.Time semantics as Range. Bounds outside
// the series are clamped; an empty or reversed range produces an empty series.
func (ts *TimeSeries) Slice(from, to time.Time) *TimeSeries {
	i, j := ts.rangeIndices(unixNano(from), unixNano(to))
	return &TimeSeries{
		time0:  ts.time0 + int64(i)*int64(ts.step),
		step:   ts.step,
		Exp:    ts.Exp,
		prices: slices.Clone(ts.prices[i:j]),
	}
}

// bound returns the first index whose timestamp is at least t.
func (ts *TimeSeries) bound(t int64) int {
	if t <= ts.time0 {
		return 0
	}
	if t >= ts.nextTime() {
		return ts.Size()
	}
	d := t - ts.time0
	i := d / int64(ts.step)
	if d%int64(ts.step) != 0 {
		i++
	}
	return int(i)
}

func (ts *TimeSeries) rangeIndices(from, to int64) (int, int) {
	ts.validate()
	if from < 0 || to < 0 {
		panic("timeseries: range bounds must be nonnegative")
	}
	i, j := ts.bound(from), ts.bound(to)
	return i, max(i, j)
}

func (ts *TimeSeries) tailStart(period time.Duration) int64 {
	ts.validate()
	if period < 0 {
		panic("timeseries: tail period must be nonnegative")
	}
	return max(ts.time0, ts.nextTime()-int64(period))
}

func normValue(p float64, exp int) uint32 {
	if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
		panic("timeseries: value must be finite and nonnegative")
	}
	v := math.Round(p / Pow10(exp))
	if v > math.MaxUint32 || math.IsInf(v, 0) {
		panic("timeseries: value exceeds uint32 capacity at the selected precision")
	}
	if exp > 0 && math.IsInf(v*Pow10(exp), 0) {
		panic("timeseries: rounded value exceeds float64 capacity")
	}
	return uint32(v)
}

// Push stores value in the interval containing timestamp. The first Push on
// an empty series sets its exact start without creating leading zero values.
// Missing intervals repeat the previous value; existing intervals are
// overwritten. Precision increases automatically using PrecisionExp. Invalid
// values, overflow, and timestamps before a nonempty series' start panic.
func (ts *TimeSeries) Push(timestamp time.Time, value float64) {
	ts.validate()
	t := unixNano(timestamp)
	start := ts.time0
	if ts.Size() == 0 {
		start = t
	} else if t < start {
		panic("timeseries: timestamp precedes the series start")
	}
	index := (t - start) / int64(ts.step)
	ts.checkLength(start, index+1)
	exp := min(ts.Exp, PrecisionExp(value))
	p := normValue(value, exp)
	if exp != ts.Exp {
		ts.SetExp(exp)
	}
	if ts.Size() == 0 {
		ts.time0 = start
	}
	i := int(index)
	if i < ts.Size() {
		ts.revision++
	}
	var last uint32
	if ts.Size() > 0 {
		last = ts.prices[ts.Size()-1]
	}
	for len(ts.prices) <= i {
		ts.prices = append(ts.prices, last)
	}
	ts.prices[i] = p
}

// Append adds a value at NextTime, rounding to the existing precision.
// Invalid values or uint32 overflow panic.
func (ts *TimeSeries) Append(value float64) {
	ts.validate()
	ts.checkLength(ts.time0, int64(ts.Size())+1)
	ts.prices = append(ts.prices, normValue(value, ts.Exp))
}

// SetExp changes precision, allocating new storage. Decreasing precision uses
// integer arithmetic and rounds halfway values up. Increasing precision keeps
// the existing float conversion. Invalid exponents or overflow panic without
// changing the series. Cursor average caches are invalidated automatically.
func (ts *TimeSeries) SetExp(exp int) *TimeSeries {
	ts.validate()
	if exp < -308 || exp > 308 {
		panic("timeseries: exponent must be between -308 and 308")
	}
	if exp == ts.Exp {
		return ts
	}
	prices := make([]uint32, ts.Size())
	if exp > ts.Exp {
		if delta := exp - ts.Exp; delta < 10 {
			divisor := uint64(Pow10(delta))
			for i, p := range ts.prices {
				prices[i] = uint32((uint64(p) + divisor/2) / divisor)
			}
		}
	} else {
		for i := range prices {
			prices[i] = normValue(ts.value(i), exp)
		}
	}
	if exp > 0 {
		scale := Pow10(exp)
		for _, p := range prices {
			if math.IsInf(float64(p)*scale, 0) {
				panic("timeseries: rounded value exceeds float64 capacity")
			}
		}
	}
	ts.prices, ts.Exp = prices, exp
	ts.revision++
	return ts
}

// Apply replaces each value with fn(timestamp, value), rounded to the existing
// precision. Invalid results panic without changing the series. fn must not
// mutate the receiver. Cursor average caches are invalidated automatically.
func (ts *TimeSeries) Apply(fn func(time.Time, float64) float64) *TimeSeries {
	ts.validate()
	prices := make([]uint32, ts.Size())
	scale := ts.scale()
	for i, p := range ts.prices {
		prices[i] = normValue(fn(nanoTime(ts.time0+int64(i)*int64(ts.step)), decodeValue(p, ts.Exp, scale)), ts.Exp)
	}
	ts.prices = prices
	ts.revision++
	return ts
}

// Merge feeds every sample of other through Push. Steps must match, and
// nonempty series must have matching interval alignment. Incoming values win
// in overlapping intervals; gaps repeat the preceding value. Samples before
// the receiver's start panic. An empty zero-value receiver adopts other's step
// and exponent. other is never modified. Earlier pushes remain applied if a
// later push fails.
func (ts *TimeSeries) Merge(other *TimeSeries) {
	if err := ts.merge(other); err != nil {
		panic(err)
	}
}

func (ts *TimeSeries) merge(other *TimeSeries) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("timeseries: merge: %v", failure)
		}
	}()
	if other.Size() == 0 || other == ts {
		return nil
	}
	other.validate()
	if ts.Size() == 0 && ts.step == 0 {
		ts.step, ts.Exp = other.step, other.Exp
	}
	ts.validate()
	if ts.step != other.step {
		panic("timeseries: merged series must have matching steps")
	}
	if ts.Size() > 0 && (other.time0-ts.time0)%int64(ts.step) != 0 {
		panic("timeseries: merged series must have matching interval alignment")
	}
	for timestamp, value := range other.All() {
		ts.Push(timestamp, value)
	}
	return nil
}

// Nums returns a copy of the decoded values.
func (ts *TimeSeries) Nums() []float64 {
	values := make([]float64, ts.Size())
	scale := ts.scale()
	for i, p := range ts.prices {
		values[i] = decodeValue(p, ts.Exp, scale)
	}
	return values
}

// All iterates timestamp-value pairs with exact UTC timestamps. Each
// traversal starts at the first sample and leaves all cursors unchanged.
// The loop body must not mutate the series. Breaking stops iteration.
func (ts *TimeSeries) All() iter.Seq2[time.Time, float64] {
	return func(yield func(time.Time, float64) bool) {
		ts.validate()
		start, end := 0, ts.Size()
		scale, exp, step := ts.scale(), ts.Exp, int64(ts.step)
		t0 := ts.time0 + int64(start)*step
		if step%int64(time.Second) == 0 {
			step /= int64(time.Second)
			nanos := t0 % int64(time.Second)
			for i, seconds := start, t0/int64(time.Second); i < end; i, seconds = i+1, seconds+step {
				if !yield(time.Unix(seconds, nanos).UTC(), decodeValue(ts.prices[i], exp, scale)) {
					return
				}
			}
			return
		}
		for i, t := start, nanoTime(t0); i < end; i, t = i+1, t.Add(time.Duration(step)) {
			if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// AllUnix iterates timestamp-value pairs using Unix seconds, matching
// time.Time.Unix. Subsecond timestamps are truncated and may repeat; use All
// to preserve full timestamp precision. Whole-second steps use integer
// increments with conversion outside the loop. Cursors are unchanged.
// The loop body must not mutate the series. Breaking stops iteration.
func (ts *TimeSeries) AllUnix() iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts.validate()
		scale, exp, step := ts.scale(), ts.Exp, int64(ts.step)
		end := ts.Size()
		if step%int64(time.Second) == 0 {
			step /= int64(time.Second)
			for i, t := 0, ts.time0/int64(time.Second); i < end; i, t = i+1, t+step {
				if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
					return
				}
			}
			return
		}
		for i, t := 0, ts.time0; i < end; i, t = i+1, t+step {
			if !yield(t/int64(time.Second), decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// Range creates an independent cursor for sample timestamps in [from, to).
// Bounds preserve nanosecond precision and are clamped to the series. Empty
// or reversed ranges create empty cursors. Index bounds are fixed at creation
// and do not grow when samples are appended. No values are copied.
func (ts *TimeSeries) Range(from, to time.Time) *Cursor {
	start, end := ts.rangeIndices(unixNano(from), unixNano(to))
	return ts.newCursor(start, end)
}

// Tail creates an independent cursor for the final duration of the series.
// Its bounds are fixed at creation. A zero duration creates an empty cursor;
// a negative duration panics. No values are copied.
func (ts *TimeSeries) Tail(period time.Duration) *Cursor {
	start, end := ts.rangeIndices(ts.tailStart(period), ts.nextTime())
	return ts.newCursor(start, end)
}

// Iterator visits all samples until fn returns false. It leaves all cursors
// unchanged. The callback must not mutate the series.
func (ts *TimeSeries) Iterator(fn func(time.Time, float64) bool) { ts.All()(fn) }

// Fetch visits every sample without changing any cursor. The callback must
// not mutate the series.
func (ts *TimeSeries) Fetch(fn func(time.Time, float64)) {
	for timestamp, value := range ts.All() {
		fn(timestamp, value)
	}
}

// FetchFromTo visits sample timestamps in [from, to), using the same time.Time
// bounds as Range. It leaves all cursors unchanged.
// The callback must not mutate the series.
func (ts *TimeSeries) FetchFromTo(from, to time.Time, fn func(time.Time, float64)) {
	for timestamp, value := range ts.Range(from, to).All() {
		fn(timestamp, value)
	}
}

// FetchMergedTimeSeries visits the common time range of all series, starting
// at or after from. Steps and alignment must match; otherwise it panics.
// It leaves all cursors unchanged. The values
// slice is reused between calls; copy it to retain values. The callback must
// not mutate the series or the input slice.
func FetchMergedTimeSeries(series []*TimeSeries, from time.Time, fn func(time.Time, []float64)) {
	t0 := unixNano(from)
	if len(series) == 0 || series[0].Size() == 0 {
		return
	}
	first := series[0]
	first.validate()
	start, end := max(t0, first.time0), first.nextTime()
	for _, ts := range series {
		if ts.Size() == 0 {
			return
		}
		ts.validate()
		if ts.step != first.step || (ts.time0-first.time0)%int64(first.step) != 0 {
			panic("timeseries: merged iteration requires matching steps and alignment")
		}
		start, end = max(start, ts.time0), min(end, ts.nextTime())
	}
	if start >= end {
		return
	}
	start = first.time0 + int64(first.bound(start))*int64(first.step)
	indices := make([]int, len(series))
	scales := make([]float64, len(series))
	values := make([]float64, len(series))
	for i, ts := range series {
		indices[i], scales[i] = int((start-ts.time0)/int64(ts.step)), ts.scale()
	}
	for t, timestamp := start, nanoTime(start); t < end; t, timestamp = t+int64(first.step), timestamp.Add(first.step) {
		for i, ts := range series {
			values[i] = decodeValue(ts.prices[indices[i]], ts.Exp, scales[i])
			indices[i]++
		}
		fn(timestamp, values)
	}
}
