package timeseries

import (
	"fmt"
	"math"
	"slices"
)

// TimeSeries stores prices at a fixed interval using four bytes per sample.
// A stored integer p represents p * 10^Exp. Set Time0, Step, and Exp before
// adding data; use SetExp to change precision after adding data.
type TimeSeries struct {
	Time0 int64 // Unix timestamp of the first sample, in seconds.
	Step  int64 // Positive sampling interval, in seconds.
	Exp   int   // Decimal exponent: -2 stores prices to two decimal places.

	prices []uint32
	cur    int
	avg    map[int]average
}

type average struct {
	index int
	sum   uint64
}

// New creates an empty series. time0 is aligned down to the interval boundary.
// period must be positive and exp must be between -308 and 308; otherwise New panics.
func New(time0 int64, period Period, exp int) *TimeSeries {
	ts := &TimeSeries{Time0: time0, Step: int64(period), Exp: exp}
	ts.validate()
	ts.Time0 = floorTime(time0, ts.Step)
	return ts
}

func (ts *TimeSeries) validate() {
	if ts.Step <= 0 {
		panic("timeseries: step must be positive")
	}
	if ts.Exp < -308 || ts.Exp > 308 {
		panic("timeseries: exponent must be between -308 and 308")
	}
}

func floorTime(t, step int64) int64 {
	q := t / step
	if t%step < 0 {
		q--
	}
	return q * step
}

// FullPeriod returns the duration covered by the series, in seconds.
func (ts *TimeSeries) FullPeriod() int64 { return int64(ts.Size()) * ts.Step }

func (ts *TimeSeries) String() string {
	if ts.Size() == 0 {
		return "<TS-empty>"
	}
	return fmt.Sprintf("<TS-%s-%s-%s>", ts.Period(), ts.First().ShortString(), ts.Last().ShortString())
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

// Precision returns the smallest price increment represented by the series.
func (ts *TimeSeries) Precision() float64 { return math.Pow10(ts.Exp) }

// NextTime returns the exclusive end time, or Time0 for an empty series.
func (ts *TimeSeries) NextTime() int64 { return ts.Time0 + ts.Step*int64(ts.Size()) }

// LastTime returns the last sample time, or Time0-Step for an empty series.
func (ts *TimeSeries) LastTime() int64 { return ts.NextTime() - ts.Step }

// First returns the first sample, or a zero Tick if the series is empty.
func (ts *TimeSeries) First() Tick { return ts.tick(0) }

// Last returns the last sample, or a zero Tick if the series is empty.
func (ts *TimeSeries) Last() Tick { return ts.tick(ts.Size() - 1) }

// Ticks returns a copy of all samples as quotes with equal Ask and Bid.
func (ts *TimeSeries) Ticks() Ticks {
	ticks := make(Ticks, ts.Size())
	for i := range ticks {
		ticks[i] = ts.tick(i)
	}
	return ticks
}

// CurTickIndex returns the cursor position used by iteration and Avg.
func (ts *TimeSeries) CurTickIndex() int { return ts.cur }

// CurTick returns the sample at the cursor, or a zero Tick if empty.
func (ts *TimeSeries) CurTick() Tick { return ts.tick(ts.cur) }

func (ts *TimeSeries) value(i int) float64 {
	if ts.Exp < 0 {
		return float64(ts.prices[i]) / math.Pow10(-ts.Exp)
	}
	return float64(ts.prices[i]) * ts.Precision()
}

func (ts *TimeSeries) tick(i int) Tick {
	if i < 0 || i >= ts.Size() {
		return Tick{}
	}
	p := ts.value(i)
	return Tick{Time: ts.Time0 + ts.Step*int64(i), Ask: p, Bid: p}
}

func (ts *TimeSeries) index(t int64) int {
	if ts.Size() == 0 || t < ts.Time0 || t >= ts.NextTime() {
		return -1
	}
	return int((t - ts.Time0) / ts.Step)
}

// TickOf returns the sample whose interval contains t, or a zero Tick outside the series.
func (ts *TimeSeries) TickOf(t int64) Tick { return ts.tick(ts.index(t)) }

// ValueOf returns the price whose interval contains t, or NaN outside the series.
func (ts *TimeSeries) ValueOf(t int64) float64 {
	if i := ts.index(t); i >= 0 {
		return ts.value(i)
	}
	return math.NaN()
}

// CleanCopy creates an independent cursor over shared prices without average caches.
// Treat both series as read-only; use Clone before modifying prices or precision.
func (ts *TimeSeries) CleanCopy() *TimeSeries {
	return &TimeSeries{Time0: ts.Time0, Step: ts.Step, Exp: ts.Exp, prices: ts.prices}
}

// Clone copies the prices and resets the cursor and average caches.
func (ts *TimeSeries) Clone() *TimeSeries {
	copy := ts.CleanCopy()
	copy.prices = slices.Clone(ts.prices)
	return copy
}

// Reduce keeps the first sample of each larger interval, including a final
// partial interval. period must be a positive multiple of Step; otherwise it panics.
// An unchanged period returns the receiver.
func (ts *TimeSeries) Reduce(period Period) *TimeSeries {
	ts.validate()
	step := int64(period)
	if step < ts.Step || step%ts.Step != 0 {
		panic("timeseries: reduced period must be a positive multiple of step")
	}
	if step == ts.Step {
		return ts
	}
	result := New(ts.Time0, period, ts.Exp)
	result.Time0 = ts.Time0
	k := step / ts.Step
	for i := int64(0); i < int64(ts.Size()); i += k {
		result.prices = append(result.prices, ts.prices[i])
	}
	return result
}

// Slice returns a read-only view of samples with timestamps in [t1, t2).
// Zero bounds select the respective series boundary. Negative bounds are
// offsets in seconds from NextTime. Bounds are clamped to the series.
// Use Clone on the result before modifying it.
func (ts *TimeSeries) Slice(t1, t2 int64) *TimeSeries {
	ts.validate()
	if t1 < 0 {
		t1 += ts.NextTime()
	} else if t1 == 0 {
		t1 = ts.Time0
	}
	if t2 <= 0 {
		t2 += ts.NextTime()
	}
	i, j := ts.bound(t1), ts.bound(t2)
	j = max(i, j)
	return &TimeSeries{Time0: ts.Time0 + int64(i)*ts.Step, Step: ts.Step, Exp: ts.Exp, prices: ts.prices[i:j:j]}
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

func normValue(p float64, exp int) uint32 {
	if p < 0 || math.IsNaN(p) || math.IsInf(p, 0) {
		panic("timeseries: price must be finite and nonnegative")
	}
	v := math.Round(p / math.Pow10(exp))
	if v > math.MaxUint32 || math.IsInf(v, 0) {
		panic("timeseries: price exceeds uint32 capacity at the selected precision")
	}
	if exp > 0 && math.IsInf(v*math.Pow10(exp), 0) {
		panic("timeseries: rounded price exceeds float64 capacity")
	}
	return uint32(v)
}

// Push stores tick.Ask in its interval and advances the cursor to that sample.
// Missing intervals repeat the preceding price. An empty series with Time0 == 0
// starts at the first tick. Precision increases automatically when needed.
// Invalid prices, overflow, and timestamps before Time0 panic.
func (ts *TimeSeries) Push(tick Tick) {
	ts.validate()
	t := floorTime(tick.Time, ts.Step)
	if ts.Size() > 0 || ts.Time0 != 0 {
		if t < ts.Time0 {
			panic("timeseries: tick precedes the series start")
		}
	}
	exp := min(ts.Exp, PrecisionExp(tick.Ask))
	p := normValue(tick.Ask, exp)
	if exp != ts.Exp {
		ts.SetExp(exp)
	}
	if ts.Size() == 0 && ts.Time0 == 0 {
		ts.Time0 = t
	}
	i := int((t - ts.Time0) / ts.Step)
	if i < ts.Size() {
		ts.avg = nil
	}
	var last uint32
	if ts.Size() > 0 {
		last = ts.prices[ts.Size()-1]
	}
	for len(ts.prices) <= i {
		ts.prices = append(ts.prices, last)
	}
	ts.prices[i], ts.cur = p, i
}

// Append adds a price at NextTime, rounding to the existing precision.
// Invalid prices or uint32 overflow panic.
func (ts *TimeSeries) Append(p float64) {
	ts.validate()
	ts.prices = append(ts.prices, normValue(p, ts.Exp))
	ts.cur = ts.Size() - 1
}

// SeekTime moves the cursor to the interval containing t, clamped to the series.
// A negative t selects the last sample. Empty series keep cursor zero.
func (ts *TimeSeries) SeekTime(t int64) *TimeSeries {
	ts.validate()
	switch {
	case ts.Size() == 0 || t <= ts.Time0 && t >= 0:
		ts.cur = 0
	case t < 0 || t >= ts.NextTime():
		ts.cur = ts.Size() - 1
	default:
		ts.cur = int((t - ts.Time0) / ts.Step)
	}
	return ts
}

// SetExp changes precision, preserving prices to the nearest new increment.
// It allocates new storage and clears average caches. Invalid exponents or
// uint32 overflow panic without changing the series.
func (ts *TimeSeries) SetExp(exp int) *TimeSeries {
	if exp < -308 || exp > 308 {
		panic("timeseries: exponent must be between -308 and 308")
	}
	if exp == ts.Exp {
		return ts
	}
	prices := make([]uint32, ts.Size())
	for i := range prices {
		prices[i] = normValue(ts.value(i), exp)
	}
	ts.prices, ts.Exp, ts.avg = prices, exp, nil
	return ts
}

// Apply replaces each price with fn(timestamp, price), rounded to the existing
// precision. Invalid results panic without changing the series. fn must not
// mutate the receiver.
func (ts *TimeSeries) Apply(fn func(int64, float64) float64) *TimeSeries {
	ts.validate()
	prices := make([]uint32, ts.Size())
	for i := range prices {
		prices[i] = normValue(fn(ts.Time0+int64(i)*ts.Step, ts.value(i)), ts.Exp)
	}
	ts.prices, ts.avg = prices, nil
	return ts
}

// Merge appends the non-overlapping tail of other. Existing samples win in
// overlapping intervals. Steps and interval alignment must match, and a gap
// between the series panics. other is never modified.
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
	if other.Size() == 0 {
		return nil
	}
	other.validate()
	if ts.Size() == 0 {
		*ts = *other.Clone()
		ts.cur = ts.Size() - 1
		return nil
	}
	ts.validate()
	if ts.Step != other.Step || (other.Time0-ts.Time0)%ts.Step != 0 {
		panic("timeseries: merged series must have matching steps and alignment")
	}
	if other.Time0 > ts.NextTime() {
		panic("timeseries: cannot merge series separated by a gap")
	}
	if other.NextTime() <= ts.NextTime() {
		return nil
	}
	tail := other.Slice(ts.NextTime(), 0).Clone()
	exp := min(ts.Exp, tail.Exp)
	tail.SetExp(exp)
	ts.SetExp(exp)
	ts.prices = append(ts.prices, tail.prices...)
	ts.cur = ts.Size() - 1
	return nil
}

// Avg returns the simple moving average ending at the cursor. period is in
// seconds and is rounded down to a whole number of samples, with a minimum of
// one. It returns NaN until a complete window is available. Sequential calls
// reuse a running sum for each requested window size.
func (ts *TimeSeries) Avg(period int64) float64 {
	ts.validate()
	n := max(1, int(period/ts.Step))
	if ts.Size() == 0 || ts.cur < n-1 {
		return math.NaN()
	}
	a, ok := ts.avg[n]
	if !ok || ts.cur < a.index || ts.cur-a.index >= n {
		a = average{index: ts.cur}
		for _, p := range ts.prices[ts.cur-n+1 : ts.cur+1] {
			a.sum += uint64(p)
		}
	} else {
		for a.index < ts.cur {
			a.index++
			a.sum -= uint64(ts.prices[a.index-n])
			a.sum += uint64(ts.prices[a.index])
		}
	}
	if ts.avg == nil {
		ts.avg = make(map[int]average)
	}
	ts.avg[n] = a
	value := float64(a.sum) / float64(n)
	if ts.Exp < 0 {
		return value / math.Pow10(-ts.Exp)
	}
	return value * ts.Precision()
}

// Nums returns a copy of the decoded prices.
func (ts *TimeSeries) Nums() []float64 {
	values := make([]float64, ts.Size())
	for i := range values {
		values[i] = ts.value(i)
	}
	return values
}

// Iterator visits samples in time order and updates the cursor before each
// callback. Returning false stops iteration. Go 1.23 supports range over this
// method: for timestamp, price := range ts.Iterator { ... }.
// The callback must not change the series' data or metadata.
func (ts *TimeSeries) Iterator(fn func(int64, float64) bool) {
	for i := range ts.prices {
		ts.cur = i
		if !fn(ts.Time0+int64(i)*ts.Step, ts.value(i)) {
			return
		}
	}
}

// Fetch visits every sample, updating the cursor before each callback.
// The callback must not mutate the series.
func (ts *TimeSeries) Fetch(fn func(int64, float64)) {
	ts.Iterator(func(t int64, p float64) bool { fn(t, p); return true })
}

// FetchFromTo visits sample timestamps in [t1, t2). Zero bounds select the
// respective series boundary. The callback must not mutate the series.
func (ts *TimeSeries) FetchFromTo(t1, t2 int64, fn func(int64, float64)) {
	ts.validate()
	if t1 == 0 {
		t1 = ts.Time0
	}
	if t2 == 0 {
		t2 = ts.NextTime()
	}
	for i, end := ts.bound(t1), ts.bound(t2); i < end; i++ {
		ts.cur = i
		fn(ts.Time0+int64(i)*ts.Step, ts.value(i))
	}
}

// FetchTicks visits samples as ticks. The callback must not mutate the series.
func (ts *TimeSeries) FetchTicks(fn func(Tick)) {
	ts.Fetch(func(t int64, p float64) { fn(Tick{Time: t, Ask: p, Bid: p}) })
}

// FetchMergedTimeSeries visits the common time range of all series, starting
// at or after t0. Steps and alignment must match; otherwise it panics.
// Each cursor is updated before fn runs. The prices slice is reused between
// calls; copy it to retain values. Neither the input slice nor prices are mutated
// by callers while iterating.
func FetchMergedTimeSeries(series []*TimeSeries, t0 int64, fn func(int64, []float64)) {
	if len(series) == 0 {
		return
	}
	first := series[0]
	if first.Size() == 0 {
		return
	}
	first.validate()
	start, end := max(t0, first.Time0), first.NextTime()
	for _, ts := range series {
		if ts.Size() == 0 {
			return
		}
		if ts.Step != first.Step || (ts.Time0-first.Time0)%first.Step != 0 {
			panic("timeseries: merged iteration requires matching steps and alignment")
		}
		start, end = max(start, ts.Time0), min(end, ts.NextTime())
	}
	i := first.bound(start)
	prices := make([]float64, len(series))
	for t := first.Time0 + int64(i)*first.Step; t < end; t += first.Step {
		for j, ts := range series {
			ts.cur = int((t - ts.Time0) / ts.Step)
			prices[j] = ts.value(ts.cur)
		}
		fn(t, prices)
	}
}

// Diff returns price changes over shift samples, timestamped at the newer
// sample. Nonpositive shifts use one sample. Ticks preserve negative changes,
// which cannot be represented by the unsigned price storage of TimeSeries.
func (ts *TimeSeries) Diff(shift int) Ticks {
	shift = max(1, shift)
	result := make(Ticks, max(0, ts.Size()-shift))
	for i := range result {
		p := ts.value(i+shift) - ts.value(i)
		result[i] = Tick{Time: ts.Time0 + int64(i+shift)*ts.Step, Ask: p, Bid: p}
	}
	return result
}

// MovingAverage returns one sample per complete trailing window. period is
// rounded down to whole samples with a minimum window size of one. Output is
// rounded to the receiver's precision and starts at the last sample of the
// first complete window. The receiver's cursor is unchanged.
func (ts *TimeSeries) MovingAverage(period Period) *TimeSeries {
	ts.validate()
	n := max(1, int(int64(period)/ts.Step))
	result := &TimeSeries{Time0: ts.Time0 + int64(n-1)*ts.Step, Step: ts.Step, Exp: ts.Exp}
	if n > ts.Size() {
		return result
	}
	var sum uint64
	for i, p := range ts.prices {
		sum += uint64(p)
		if i >= n {
			sum -= uint64(ts.prices[i-n])
		}
		if i >= n-1 {
			result.prices = append(result.prices, uint32(math.Round(float64(sum)/float64(n))))
		}
	}
	return result
}
