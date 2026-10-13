package timeseries

import (
	"iter"
	"math"
	"slices"
	"testing"
)

const t0 int64 = 1577836800 // 2020-01-01 UTC.

func newTestTimeSeries() *TimeSeries {
	return seriesWithValues(t0, Period1s, -3, 10.123, 9.123, 10.123, 11.123, 12.123)
}

func seriesWithValues(start int64, step Period, exp int, values ...float64) *TimeSeries {
	ts := New(start, step, exp)
	for _, value := range values {
		ts.Append(value)
	}
	return ts
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want && !(got != got && want != want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.IsNaN(want) {
		if !math.IsNaN(got) {
			t.Fatalf("got %v; want NaN", got)
		}
		return
	}
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > 1e-12*max(1, math.Abs(want)) {
		t.Fatalf("got %.17g; want %.17g", got, want)
	}
}

func assertValues(t *testing.T, got, want []float64) {
	t.Helper()
	if !slices.EqualFunc(got, want, func(a, b float64) bool {
		return a == b || math.IsNaN(a) && math.IsNaN(b)
	}) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	fn()
}

func collect(seq iter.Seq2[int64, float64]) ([]int64, []float64) {
	var times []int64
	var values []float64
	for timestamp, value := range seq {
		times = append(times, timestamp)
		values = append(values, value)
	}
	return times, values
}

func assertSamples(t *testing.T, seq iter.Seq2[int64, float64], wantTimes []int64, wantValues []float64) {
	t.Helper()
	times, values := collect(seq)
	if !slices.Equal(times, wantTimes) {
		t.Fatalf("timestamps: got %v; want %v", times, wantTimes)
	}
	assertValues(t, values, wantValues)
}

func TestTimeSeries_Append(t *testing.T) {
	ts := newTestTimeSeries()
	assertEqual(t, ts.Size(), 5)
	assertEqual(t, ts.Time0, t0)
	assertEqual(t, ts.LastTime(), t0+4)
	assertEqual(t, ts.NextTime(), t0+5)
	assertEqual(t, ts.FullPeriod(), int64(5))
	assertEqual(t, ts.Period(), Period1s)
	assertEqual(t, ts.Precision(), 0.001)
	assertValues(t, ts.Nums(), []float64{10.123, 9.123, 10.123, 11.123, 12.123})
}

func TestTimeSeries_Fetch(t *testing.T) {
	ts := newTestTimeSeries()
	cursor := ts.Cursor().Seek(t0 + 2)
	var times []int64
	var values []float64
	ts.Fetch(func(timestamp int64, value float64) {
		times = append(times, timestamp)
		values = append(values, value)
	})
	if !slices.Equal(times, []int64{t0, t0 + 1, t0 + 2, t0 + 3, t0 + 4}) {
		t.Fatalf("unexpected timestamps: %v", times)
	}
	assertValues(t, values, ts.Nums())
	assertEqual(t, cursor.Index(), 2)

	times, values = nil, nil
	ts.FetchFromTo(t0+1, t0+3, func(timestamp int64, value float64) {
		times = append(times, timestamp)
		values = append(values, value)
	})
	if !slices.Equal(times, []int64{t0 + 1, t0 + 2}) {
		t.Fatalf("unexpected range timestamps: %v", times)
	}
	assertValues(t, values, []float64{9.123, 10.123})
	assertEqual(t, cursor.Index(), 2)
}

func TestTimeSeries_Iterator(t *testing.T) {
	ts := newTestTimeSeries()
	for _, seq := range []iter.Seq2[int64, float64]{ts.Iterator, ts.All()} {
		visits := 0
		for timestamp, value := range seq {
			assertEqual(t, timestamp, t0)
			assertEqual(t, value, 10.123)
			visits++
			break
		}
		assertEqual(t, visits, 1)
		_, values := collect(seq)
		assertValues(t, values, ts.Nums())
	}
}

func TestTimeSeries_Push(t *testing.T) {
	ts := newTestTimeSeries()
	ts.Push(t0, 14.9999)
	ts.Push(t0+9, 13.123)
	assertEqual(t, ts.Exp, -4)
	assertEqual(t, ts.Size(), 10)
	assertEqual(t, ts.LastTime(), t0+9)
	assertEqual(t, ts.NextTime(), t0+10)
	assertValues(t, ts.Nums(), []float64{
		14.9999, 9.123, 10.123, 11.123, 12.123,
		12.123, 12.123, 12.123, 12.123, 13.123,
	})
}

func TestTimeSeries_PushFirstSample(t *testing.T) {
	for _, start := range []int64{0, t0 + 3} {
		ts := New(t0+100, 10, -2)
		ts.Push(start, 14.25)
		assertEqual(t, ts.Time0, start)
		assertEqual(t, ts.Size(), 1)
		ts.Push(start+29, 15.50)
		assertValues(t, ts.Nums(), []float64{14.25, 14.25, 15.50})
		assertEqual(t, ts.LastTime(), start+20)
		assertEqual(t, ts.ValueOf(start+19), 14.25)
		assertEqual(t, ts.ValueOf(start+29), 15.50)
	}
}

func TestTimeSeries_ValueOf(t *testing.T) {
	ts := seriesWithValues(t0+3, 10, -2, 1.25, 2.50)
	for _, tc := range []struct {
		time int64
		want float64
	}{
		{t0 + 2, math.NaN()},
		{t0 + 3, 1.25},
		{t0 + 12, 1.25},
		{t0 + 13, 2.50},
		{t0 + 22, 2.50},
		{t0 + 23, math.NaN()},
	} {
		assertEqual(t, ts.ValueOf(tc.time), tc.want)
	}
}

func TestTimeSeries_RangeAndSlice(t *testing.T) {
	const start = t0 + 3
	ts := seriesWithValues(start, 10, 0, 10, 20, 30, 40, 50)
	cases := []struct {
		name       string
		from, to   int64
		wantTimes  []int64
		wantValues []float64
	}{
		{"aligned", start + 10, start + 30, []int64{start + 10, start + 20}, []float64{20, 30}},
		{"unaligned", start + 1, start + 30, []int64{start + 10, start + 20}, []float64{20, 30}},
		{"clamped", 0, start + 100, []int64{start, start + 10, start + 20, start + 30, start + 40}, []float64{10, 20, 30, 40, 50}},
		{"before", 0, start, nil, nil},
		{"after", start + 50, start + 100, nil, nil},
		{"empty", start + 20, start + 20, nil, nil},
		{"reversed", start + 40, start + 10, nil, nil},
		{"zero_is_literal", 0, 0, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSamples(t, ts.Range(tc.from, tc.to).All(), tc.wantTimes, tc.wantValues)
			copy := ts.Slice(tc.from, tc.to)
			assertSamples(t, copy.All(), tc.wantTimes, tc.wantValues)
		})
	}
}

func TestTimeSeries_Tail(t *testing.T) {
	ts := newTestTimeSeries()
	for _, tc := range []struct {
		period Period
		times  []int64
		values []float64
	}{
		{0, nil, nil},
		{2, []int64{t0 + 3, t0 + 4}, []float64{11.123, 12.123}},
		{100, []int64{t0, t0 + 1, t0 + 2, t0 + 3, t0 + 4}, ts.Nums()},
	} {
		assertSamples(t, ts.Tail(tc.period).All(), tc.times, tc.values)
		cursor := ts.Tail(tc.period)
		assertSamples(t, cursor.All(), tc.times, tc.values)
	}
}

func TestTimeSeries_IndependentCopies(t *testing.T) {
	for _, copySeries := range []struct {
		name string
		fn   func(*TimeSeries) *TimeSeries
	}{
		{"clone", func(ts *TimeSeries) *TimeSeries { return ts.Clone() }},
		{"slice", func(ts *TimeSeries) *TimeSeries { return ts.Slice(t0, t0+3) }},
	} {
		t.Run(copySeries.name, func(t *testing.T) {
			original := newTestTimeSeries()
			copy := copySeries.fn(original)
			copy.Push(t0, 20.123)
			assertEqual(t, original.ValueOf(t0), 10.123)
			original.Push(t0+1, 30.123)
			assertEqual(t, copy.ValueOf(t0+1), 9.123)
			values := copy.Nums()
			values[0] = 0
			assertEqual(t, copy.ValueOf(t0), 20.123)
		})
	}
}

func TestTimeSeries_SliceExcludesLaterAppends(t *testing.T) {
	ts := newTestTimeSeries()
	end := ts.NextTime()
	ts.Push(end, 666)
	copy := ts.Slice(ts.Time0, end)
	assertValues(t, copy.Nums(), newTestTimeSeries().Nums())
	assertEqual(t, copy.NextTime(), end)
}

func TestTimeSeries_Reduce(t *testing.T) {
	const start = t0 + 3
	ts := seriesWithValues(start, 10, 0, 10, 20, 30, 40, 50)
	cursor := ts.Cursor().Seek(ts.LastTime())
	reduced := ts.Reduce(20)
	assertEqual(t, reduced.Time0, start)
	assertEqual(t, reduced.Step, int64(20))
	assertValues(t, reduced.Nums(), []float64{10, 30, 50})
	assertEqual(t, reduced.Cursor().Index(), 0)
	assertEqual(t, cursor.Index(), 4)
	reduced.Push(reduced.NextTime(), 70)
	assertSamples(t, reduced.All(), []int64{start, start + 20, start + 40, start + 60}, []float64{10, 30, 50, 70})
	reduced.Push(start, 99)
	assertEqual(t, ts.ValueOf(start), 10.0)
	assertEqual(t, ts.Reduce(10), ts)
}

func TestTimeSeries_SetExp(t *testing.T) {
	cases := []struct {
		name         string
		from, to     int
		values, want []float64
	}{
		{"increase", -2, -4, []float64{1.23, 12.34}, []float64{1.23, 12.34}},
		{"unchanged", -3, -3, []float64{1.005}, []float64{1.005}},
		{"decrease", -3, -2, []float64{1.004, 1.005, 9.999}, []float64{1, 1.01, 10}},
		{"round_large_value", 0, 1, []float64{math.MaxUint32}, []float64{4294967300}},
		{"large_exponent_change", -3, 7, []float64{4294967.295}, []float64{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := seriesWithValues(t0, Period1s, tc.from, tc.values...)
			assertEqual(t, ts.SetExp(tc.to), ts)
			assertEqual(t, ts.Exp, tc.to)
			assertValues(t, ts.Nums(), tc.want)
		})
	}
	ts := seriesWithValues(t0, Period1s, 0, math.MaxUint32)
	assertPanics(t, func() { ts.SetExp(-1) })
	assertEqual(t, ts.Exp, 0)
	assertValues(t, ts.Nums(), []float64{math.MaxUint32})
}

func TestTimeSeries_Apply(t *testing.T) {
	ts := newTestTimeSeries()
	var times []int64
	ts.Apply(func(timestamp int64, value float64) float64 {
		times = append(times, timestamp)
		return value + 1
	})
	assertValues(t, ts.Nums(), []float64{11.123, 10.123, 11.123, 12.123, 13.123})
	if !slices.Equal(times, []int64{t0, t0 + 1, t0 + 2, t0 + 3, t0 + 4}) {
		t.Fatalf("unexpected callback timestamps: %v", times)
	}
	before := ts.Nums()
	assertPanics(t, func() {
		ts.Apply(func(timestamp int64, value float64) float64 {
			if timestamp == t0+2 {
				return -1
			}
			return value * 2
		})
	})
	assertValues(t, ts.Nums(), before)
}

func TestTimeSeries_Merge(t *testing.T) {
	ts := seriesWithValues(t0, Period1s, -2, 1.25, 2.50, 3.75)
	other := seriesWithValues(t0+1, Period1s, -3, 2.555, 3.125, 4.875)
	before := other.Clone()
	ts.Merge(other)
	assertValues(t, ts.Nums(), []float64{1.25, 2.555, 3.125, 4.875})
	assertEqual(t, ts.Exp, -3)
	assertValues(t, other.Nums(), before.Nums())
	assertEqual(t, other.Exp, before.Exp)

	ts.Merge(seriesWithValues(t0+6, Period1s, -3, 5.125))
	assertValues(t, ts.Nums(), []float64{1.25, 2.555, 3.125, 4.875, 4.875, 4.875, 5.125})
	ts.Merge(ts)
	assertEqual(t, ts.Size(), 7)

	var empty TimeSeries
	empty.Merge(other)
	assertEqual(t, empty.Time0, other.Time0)
	assertEqual(t, empty.Step, other.Step)
	assertEqual(t, empty.Exp, other.Exp)
	assertValues(t, empty.Nums(), other.Nums())
	empty.Push(other.Time0, 9.125)
	assertEqual(t, other.ValueOf(other.Time0), 2.555)
}

func TestTimeSeries_FetchMerged(t *testing.T) {
	const start = t0 + 3
	first := seriesWithValues(start, 10, 0, 1, 2, 3, 4, 5)
	second := seriesWithValues(start+10, 10, 1, 20, 30, 40)
	c1, c2 := first.Cursor().Seek(start+10), second.Cursor()
	series := []*TimeSeries{first, second}
	var times []int64
	var values [][]float64
	FetchMergedTimeSeries(series, start+15, func(timestamp int64, p []float64) {
		times = append(times, timestamp)
		values = append(values, slices.Clone(p))
	})
	if !slices.Equal(times, []int64{start + 20, start + 30}) {
		t.Fatalf("unexpected merged timestamps: %v", times)
	}
	assertEqual(t, len(values), 2)
	assertValues(t, values[0], []float64{3, 30})
	assertValues(t, values[1], []float64{4, 40})
	assertEqual(t, series[0], first)
	assertEqual(t, series[1], second)
	assertEqual(t, c1.Index(), 1)
	assertEqual(t, c2.Index(), 0)
	FetchMergedTimeSeries(series, start+100, func(int64, []float64) { t.Fatal("unexpected visit outside common range") })
}

func TestTimeSeries_Empty(t *testing.T) {
	ts := New(t0, Period1s, -3)
	assertEqual(t, ts.Size(), 0)
	assertEqual(t, ts.NextTime(), t0)
	assertEqual(t, ts.LastTime(), t0)
	assertEqual(t, ts.ValueOf(t0), math.NaN())
	assertSamples(t, ts.All(), nil, nil)
	assertSamples(t, ts.Tail(10).All(), nil, nil)
	assertEqual(t, ts.Slice(0, t0+10).Size(), 0)
	assertEqual(t, ts.Reduce(2).Size(), 0)
	var nilSeries *TimeSeries
	assertEqual(t, nilSeries.Size(), 0)
	FetchMergedTimeSeries(nil, 0, func(int64, []float64) { t.Fatal("unexpected visit for empty input") })
}

func TestTimeSeries_InvalidInput(t *testing.T) {
	for name, fn := range map[string]func(){
		"negative_start": func() { New(-1, Period1s, 0) },
		"zero_step":      func() { New(t0, 0, 0) },
		"invalid_exp":    func() { New(t0, Period1s, 309) },
		"negative_value": func() { newTestTimeSeries().Append(-1) },
		"nan_value":      func() { newTestTimeSeries().Append(math.NaN()) },
		"infinite_value": func() { newTestTimeSeries().Push(t0, math.Inf(1)) },
		"overflow":       func() { New(t0, Period1s, 0).Append(float64(math.MaxUint32) + 1) },
		"negative_time":  func() { newTestTimeSeries().Push(-1, 1) },
		"earlier_time":   func() { newTestTimeSeries().Push(t0-1, 1) },
		"negative_lookup": func() {
			newTestTimeSeries().ValueOf(-1)
		},
		"negative_range": func() { newTestTimeSeries().Range(-1, t0) },
		"negative_slice": func() { newTestTimeSeries().Slice(t0, -1) },
		"negative_tail":  func() { newTestTimeSeries().Tail(-1) },
		"invalid_reduce": func() { seriesWithValues(t0, 2, 0, 1).Reduce(3) },
		"merge_steps": func() {
			newTestTimeSeries().Merge(seriesWithValues(t0, 2, 0, 1))
		},
		"merge_alignment": func() {
			seriesWithValues(t0, 2, 0, 1).Merge(seriesWithValues(t0+1, 2, 0, 2))
		},
		"merge_earlier": func() {
			newTestTimeSeries().Merge(seriesWithValues(t0-1, Period1s, 0, 1))
		},
	} {
		t.Run(name, func(t *testing.T) { assertPanics(t, fn) })
	}
}
