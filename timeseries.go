package timeseries

import (
	"fmt"
	"iter"
	"math"
	"slices"
)

// TimeSeries stores one nonnegative value per interval using four bytes per
// sample. A stored integer p represents p * 10^Exp. Set Time0, Step, and Exp
// before adding data; use SetExp to change precision afterwards.
// Iteration does not change the series. Use Cursor for position-based averages.
type TimeSeries struct {
	Time0 int64 // Unix timestamp of the first sample, in seconds.
	Step  int64 // Positive sampling interval, in seconds.
	Exp   int   // Decimal exponent: -2 stores values to two decimal places.

	prices   []uint32
	revision uint64
}

// New creates an empty series with a positive period and an exponent between
// -308 and 308. time0 must be nonnegative and is used by Append. The first Push
// sets Time0 to its own timestamp instead. Invalid metadata panics.
func New(time0 int64, period Period, exp int) *TimeSeries {
	ts := &TimeSeries{Time0: time0, Step: int64(period), Exp: exp}
	ts.validate()
	return ts
}

func (ts *TimeSeries) validate() {
	if ts.Time0 < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if ts.Step <= 0 {
		panic("timeseries: step must be positive")
	}
	if ts.Exp < -308 || ts.Exp > 308 {
		panic("timeseries: exponent must be between -308 and 308")
	}
}

// FullPeriod returns the duration covered by the series, in seconds.
func (ts *TimeSeries) FullPeriod() int64 { return int64(ts.Size()) * ts.Step }

func (ts *TimeSeries) String() string {
	if ts.Size() == 0 {
		return "<TS-empty>"
	}
	return fmt.Sprintf("<TS-%s [%d %g] [%d %g]>", ts.Period(), ts.Time0, ts.value(0), ts.LastTime(), ts.value(ts.Size()-1))
}

// Period returns the sampling interval.
func (ts *TimeSeries) Period() Period { return Period(ts.Step) }

// Size returns the number of samples. A nil series has size zero.
func (ts *TimeSeries) Size() int {
	if ts == nil {
		return 0
	}
	return len(ts.prices)
}

// Precision returns the smallest value increment represented by the series.
func (ts *TimeSeries) Precision() float64 { return Pow10(ts.Exp) }

// NextTime returns the exclusive end time, or Time0 for an empty series.
func (ts *TimeSeries) NextTime() int64 { return ts.Time0 + ts.Step*int64(ts.Size()) }

// LastTime returns the last sample time, or Time0 for an empty series.
func (ts *TimeSeries) LastTime() int64 {
	if ts.Size() == 0 {
		return ts.Time0
	}
	return ts.NextTime() - ts.Step
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
	if ts.Size() == 0 || t < ts.Time0 || t >= ts.NextTime() {
		return -1
	}
	return int((t - ts.Time0) / ts.Step)
}

// ValueOf returns the value whose interval contains t, or NaN outside the
// series. t is a literal nonnegative Unix timestamp in seconds.
func (ts *TimeSeries) ValueOf(t int64) float64 {
	if i := ts.index(t); i >= 0 {
		return ts.value(i)
	}
	return math.NaN()
}

// Clone creates an independent copy of the metadata and values.
func (ts *TimeSeries) Clone() *TimeSeries {
	return &TimeSeries{Time0: ts.Time0, Step: ts.Step, Exp: ts.Exp, prices: slices.Clone(ts.prices)}
}

// Reduce keeps the first sample of each larger interval, including a final
// partial interval. period must be a positive multiple of Step; otherwise it
// panics. A larger period creates independent storage and preserves Time0;
// an unchanged period returns the receiver. New cursors start at the first sample.
func (ts *TimeSeries) Reduce(period Period) *TimeSeries {
	ts.validate()
	step := int64(period)
	if step < ts.Step || step%ts.Step != 0 {
		panic("timeseries: reduced period must be a positive multiple of step")
	}
	if step == ts.Step {
		return ts
	}
	k := step / ts.Step
	n := ts.Size()
	if n > 0 {
		n = int((int64(n)-1)/k + 1)
	}
	result := &TimeSeries{Time0: ts.Time0, Step: step, Exp: ts.Exp, prices: make([]uint32, n)}
	for i := range result.prices {
		result.prices[i] = ts.prices[int64(i)*k]
	}
	return result
}

// Slice copies samples with timestamps in [from, to) into independent storage.
// Bounds are literal nonnegative Unix seconds, just like Range. Bounds outside
// the series are clamped; an empty or reversed range produces an empty series.
func (ts *TimeSeries) Slice(from, to int64) *TimeSeries {
	i, j := ts.rangeIndices(from, to)
	return &TimeSeries{
		Time0:  ts.Time0 + int64(i)*ts.Step,
		Step:   ts.Step,
		Exp:    ts.Exp,
		prices: slices.Clone(ts.prices[i:j]),
	}
}

// bound returns the first index whose timestamp is at least t.
func (ts *TimeSeries) bound(t int64) int {
	if t <= ts.Time0 {
		return 0
	}
	if t >= ts.NextTime() {
		return ts.Size()
	}
	d := t - ts.Time0
	i := d / ts.Step
	if d%ts.Step != 0 {
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

func (ts *TimeSeries) tailStart(period Period) int64 {
	ts.validate()
	if period < 0 {
		panic("timeseries: tail period must be nonnegative")
	}
	return max(ts.Time0, ts.NextTime()-int64(period))
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
// an empty series sets Time0 to timestamp without creating leading zero values.
// Subsequent intervals are relative to Time0. Missing intervals repeat the
// previous value; existing intervals are overwritten. Precision increases
// automatically using PrecisionExp. Invalid values, overflow, negative times,
// and timestamps before a nonempty series' start panic.
func (ts *TimeSeries) Push(timestamp int64, value float64) {
	ts.validate()
	if timestamp < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if ts.Size() > 0 && timestamp < ts.Time0 {
		panic("timeseries: timestamp precedes the series start")
	}
	exp := min(ts.Exp, PrecisionExp(value))
	p := normValue(value, exp)
	if exp != ts.Exp {
		ts.SetExp(exp)
	}
	if ts.Size() == 0 {
		ts.Time0 = timestamp
	}
	i := int((timestamp - ts.Time0) / ts.Step)
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
func (ts *TimeSeries) Apply(fn func(int64, float64) float64) *TimeSeries {
	ts.validate()
	prices := make([]uint32, ts.Size())
	scale := ts.scale()
	for i, p := range ts.prices {
		prices[i] = normValue(fn(ts.Time0+int64(i)*ts.Step, decodeValue(p, ts.Exp, scale)), ts.Exp)
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
	if ts.Size() == 0 && ts.Step == 0 {
		ts.Step, ts.Exp = other.Step, other.Exp
	}
	ts.validate()
	if ts.Step != other.Step {
		panic("timeseries: merged series must have matching steps")
	}
	if ts.Size() > 0 && (other.Time0-ts.Time0)%ts.Step != 0 {
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

// All returns an iterator over all timestamp-value pairs. Each traversal starts
// at the first sample and leaves all cursors unchanged. The loop body must not
// mutate the series. Breaking the loop stops iteration.
func (ts *TimeSeries) All() iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts.Range(ts.Time0, ts.NextTime())(yield)
	}
}

// Range iterates sample timestamps in [from, to). Bounds are literal
// nonnegative Unix seconds: zero is the Unix epoch, not an open bound.
// Bounds outside the series are clamped; empty or reversed ranges yield
// nothing. Iteration leaves all cursors unchanged. The loop body must not
// mutate the series. Breaking the loop stops iteration.
func (ts *TimeSeries) Range(from, to int64) iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		start, end := ts.rangeIndices(from, to)
		scale, exp, step := ts.scale(), ts.Exp, ts.Step
		for i, t := start, ts.Time0+int64(start)*step; i < end; i, t = i+1, t+step {
			if !yield(t, decodeValue(ts.prices[i], exp, scale)) {
				return
			}
		}
	}
}

// Tail iterates the last period seconds, using the same [from, to) semantics
// as Range. A zero period yields nothing; a negative period panics. Iteration
// leaves all cursors unchanged. The loop body must not mutate the series.
func (ts *TimeSeries) Tail(period Period) iter.Seq2[int64, float64] {
	return func(yield func(int64, float64) bool) {
		ts.Range(ts.tailStart(period), ts.NextTime())(yield)
	}
}

// Iterator visits all samples until fn returns false. It leaves all cursors
// unchanged. The callback must not mutate the series.
func (ts *TimeSeries) Iterator(fn func(int64, float64) bool) { ts.All()(fn) }

// Fetch visits every sample without changing any cursor. The callback must
// not mutate the series.
func (ts *TimeSeries) Fetch(fn func(int64, float64)) {
	for timestamp, value := range ts.All() {
		fn(timestamp, value)
	}
}

// FetchFromTo visits sample timestamps in [from, to), using the same literal
// nonnegative Unix seconds as Range. It leaves all cursors unchanged.
// The callback must not mutate the series.
func (ts *TimeSeries) FetchFromTo(from, to int64, fn func(int64, float64)) {
	for timestamp, value := range ts.Range(from, to) {
		fn(timestamp, value)
	}
}

// FetchMergedTimeSeries visits the common time range of all series, starting
// at or after t0, a nonnegative Unix timestamp in seconds. Steps and alignment
// must match; otherwise it panics. It leaves all cursors unchanged. The values
// slice is reused between calls; copy it to retain values. The callback must
// not mutate the series or the input slice.
func FetchMergedTimeSeries(series []*TimeSeries, t0 int64, fn func(int64, []float64)) {
	if t0 < 0 {
		panic("timeseries: timestamp must be nonnegative")
	}
	if len(series) == 0 || series[0].Size() == 0 {
		return
	}
	first := series[0]
	first.validate()
	start, end := max(t0, first.Time0), first.NextTime()
	for _, ts := range series {
		if ts.Size() == 0 {
			return
		}
		ts.validate()
		if ts.Step != first.Step || (ts.Time0-first.Time0)%first.Step != 0 {
			panic("timeseries: merged iteration requires matching steps and alignment")
		}
		start, end = max(start, ts.Time0), min(end, ts.NextTime())
	}
	if start >= end {
		return
	}
	start = first.Time0 + int64(first.bound(start))*first.Step
	indices := make([]int, len(series))
	scales := make([]float64, len(series))
	values := make([]float64, len(series))
	for i, ts := range series {
		indices[i], scales[i] = int((start-ts.Time0)/ts.Step), ts.scale()
	}
	for t := start; t < end; t += first.Step {
		for i, ts := range series {
			values[i] = decodeValue(ts.prices[indices[i]], ts.Exp, scales[i])
			indices[i]++
		}
		fn(t, values)
	}
}
