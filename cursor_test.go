package timeseries

import (
	"bufio"
	"bytes"
	"math"
	"sync"
	"testing"
	"time"
)

func TestCursor_Avg(t *testing.T) {
	ts := newTestTimeSeries()
	cursor := ts.Cursor()
	assertEqual(t, cursor.Index(), 0)
	assertEqual(t, cursor.Time().Unix(), t0)
	assertEqual(t, cursor.Value(), 10.123)
	assertEqual(t, cursor.Avg(seconds(2)), math.NaN())

	cursor.Seek(at(ts.LastTime().Unix()))
	for range 2 {
		assertEqual(t, cursor.Avg(seconds(100)), math.NaN())
		assertEqual(t, cursor.Avg(seconds(3)), 11.123)
	}

	cursor.Seek(at(0))
	assertEqual(t, cursor.Index(), 0)
	assertEqual(t, cursor.Avg(seconds(1)), 10.123)
	assertEqual(t, cursor.Avg(seconds(2)), math.NaN())
}

func TestCursor_IterationAvg(t *testing.T) {
	ts := newTestTimeSeries()
	cursor := ts.Cursor()
	expected := [][]float64{
		{10.123, 9.123, 10.123, 11.123, 12.123},
		{math.NaN(), 9.623, 9.623, 10.623, 11.623},
		{math.NaN(), math.NaN(), 9.789666666666667, 10.123, 11.123},
	}
	for range 3 {
		visits := 0
		for timestamp, value := range cursor.AllUnix() {
			assertEqual(t, cursor.Index(), visits)
			assertEqual(t, cursor.Time().Unix(), timestamp)
			assertEqual(t, cursor.Value(), value)
			assertEqual(t, timestamp, t0+int64(visits))
			for i, values := range expected {
				assertClose(t, cursor.Avg(seconds(int64(i+1))), values[visits])
			}
			visits++
		}
		assertEqual(t, visits, 5)
		assertEqual(t, cursor.Index(), 4)
	}
}

func TestCursor_AvgAfterAppendAndPush(t *testing.T) {
	for _, appendValue := range []struct {
		name string
		fn   func(*TimeSeries)
	}{
		{"append", func(ts *TimeSeries) { ts.Append(13.123) }},
		{"push", func(ts *TimeSeries) { ts.Push(at(ts.NextTime().Unix()), 13.123) }},
	} {
		t.Run(appendValue.name, func(t *testing.T) {
			ts := newTestTimeSeries()
			cursor := ts.Cursor().Seek(at(ts.LastTime().Unix()))
			assertEqual(t, cursor.Avg(seconds(3)), 11.123)
			appendValue.fn(ts)
			assertEqual(t, cursor.Index(), 4)
			assertEqual(t, cursor.Avg(seconds(3)), 11.123)
			cursor.Seek(at(ts.LastTime().Unix()))
			assertEqual(t, cursor.Avg(seconds(3)), 12.123)
			assertEqual(t, cursor.Avg(seconds(3)), 12.123)
		})
	}
}

func TestCursor_AvgAfterStorageGrowth(t *testing.T) {
	ts := seriesWithValues(t0, 1, 0, 1, 2, 3)
	cursor := ts.Cursor().Seek(at(t0 + 2))
	assertEqual(t, cursor.Avg(seconds(3)), 2.0)
	for value := 4; value <= 1024; value++ {
		ts.Append(float64(value))
	}
	cursor.Seek(at(ts.LastTime().Unix()))
	assertEqual(t, cursor.Avg(seconds(3)), 1023.0)
}

func TestCursor_AvgInvalidation(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *TimeSeries){
		"overwrite": func(t *testing.T, ts *TimeSeries) { ts.Push(at(t0+3), 20.123) },
		"higher_precision": func(t *testing.T, ts *TimeSeries) {
			ts.Push(at(t0+4), 12.1234)
		},
		"lower_precision": func(t *testing.T, ts *TimeSeries) { ts.SetExp(-2) },
		"apply": func(t *testing.T, ts *TimeSeries) {
			ts.Apply(func(_ time.Time, value float64) float64 { return value * 2 })
		},
		"merge": func(t *testing.T, ts *TimeSeries) {
			ts.Merge(seriesWithValues(t0+3, 1, -3, 30.123, 40.123))
		},
		"decode": func(t *testing.T, ts *TimeSeries) {
			replacement := seriesWithValues(t0, 1, 0, 2, 4, 6, 8, 10)
			var data bytes.Buffer
			w := bufio.NewWriter(&data)
			if err := replacement.BinaryEncode(w); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if err := ts.BinaryDecode(bufio.NewReader(&data)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			ts := newTestTimeSeries()
			first, second := ts.Cursor().Seek(at(t0+4)), ts.Cursor().Seek(at(t0+4))
			assertEqual(t, first.Avg(seconds(3)), 11.123)
			assertEqual(t, second.Avg(seconds(3)), 11.123)
			mutate(t, ts)
			values := ts.Nums()
			want := (values[2] + values[3] + values[4]) / 3
			assertClose(t, first.Avg(seconds(3)), want)
			assertClose(t, second.Avg(seconds(3)), want)
			assertEqual(t, first.Index(), 4)
			assertEqual(t, second.Index(), 4)
		})
	}
}

func TestCursor_IndependentTraversal(t *testing.T) {
	ts := newTestTimeSeries()
	outer, inner := ts.Cursor(), ts.Cursor()
	for timestamp, value := range outer.AllUnix() {
		index := outer.Index()
		for range inner.AllUnix() {
		}
		for range ts.AllUnix() {
		}
		assertEqual(t, outer.Index(), index)
		assertEqual(t, outer.Time().Unix(), timestamp)
		assertEqual(t, outer.Value(), value)
		assertEqual(t, outer.Avg(seconds(1)), value)
		assertEqual(t, inner.Index(), 4)
	}
}

func TestCursor_Range(t *testing.T) {
	ts := newTestTimeSeries()
	cursor := ts.Range(at(t0+2), at(t0+4))
	assertEqual(t, cursor.StartIndex(), 2)
	assertEqual(t, cursor.EndIndex(), 4)
	assertEqual(t, cursor.Index(), 2)
	assertEqual(t, cursor.Value(), 10.123)
	seq := cursor.AllUnix()
	for range 2 {
		visits := 0
		for timestamp, _ := range seq {
			assertEqual(t, cursor.Index(), visits+2)
			assertEqual(t, cursor.Time().Unix(), timestamp)
			assertClose(t, cursor.Avg(seconds(3)), []float64{9.789666666666667, 10.123}[visits])
			visits++
		}
		assertEqual(t, visits, 2)
		assertEqual(t, cursor.Index(), 3)
	}
	for range cursor.AllUnix() {
		break
	}
	assertEqual(t, cursor.Index(), 2)
	assertEqual(t, cursor.Seek(at(0)).Index(), 2)
	assertEqual(t, cursor.Seek(at(ts.NextTime().Unix())).Index(), 3)
}

func TestCursor_EmptyRange(t *testing.T) {
	ts := newTestTimeSeries()
	for _, tc := range []struct {
		from, to int64
		index    int
	}{
		{0, 0, 0},
		{t0 + 2, t0 + 2, 2},
		{t0 + 4, t0 + 1, 4},
		{t0 + 10, t0 + 20, 5},
	} {
		cursor := ts.Range(at(tc.from), at(tc.to))
		assertEqual(t, cursor.StartIndex(), tc.index)
		assertEqual(t, cursor.EndIndex(), tc.index)
		assertEqual(t, cursor.Index(), tc.index)
		assertEqual(t, cursor.Time().Unix(), t0+int64(tc.index))
		assertEqual(t, cursor.Value(), math.NaN())
		assertEqual(t, cursor.Avg(seconds(1)), math.NaN())
		assertSamples(t, cursor.AllUnix(), nil, nil)
		assertEqual(t, cursor.Seek(at(0)).Index(), tc.index)
		assertEqual(t, cursor.Seek(at(ts.NextTime().Unix())).Index(), tc.index)
	}
}

func TestCursor_FixedAndFullBounds(t *testing.T) {
	ts := seriesWithValues(t0, 1, 0, 1, 2, 3)
	bounded, tail, full := ts.Range(at(t0), at(t0+10)), ts.Tail(seconds(2)), ts.Cursor()
	assertEqual(t, bounded.EndIndex(), 3)
	assertEqual(t, tail.StartIndex(), 1)
	assertEqual(t, tail.EndIndex(), 3)
	assertEqual(t, full.EndIndex(), 3)
	ts.Append(4)
	assertEqual(t, bounded.EndIndex(), 3)
	assertEqual(t, tail.EndIndex(), 3)
	assertEqual(t, full.EndIndex(), 4)
	assertSamples(t, bounded.AllUnix(), []int64{t0, t0 + 1, t0 + 2}, []float64{1, 2, 3})
	assertSamples(t, tail.AllUnix(), []int64{t0 + 1, t0 + 2}, []float64{2, 3})
	assertSamples(t, full.AllUnix(), []int64{t0, t0 + 1, t0 + 2, t0 + 3}, []float64{1, 2, 3, 4})
	assertEqual(t, bounded.Seek(at(ts.LastTime().Unix())).Value(), 3.0)
	assertEqual(t, full.Seek(at(ts.LastTime().Unix())).Value(), 4.0)

	empty := New(at(t0), seconds(1), 0)
	emptyRange := empty.Range(at(t0), at(t0+10))
	empty.Append(1)
	assertSamples(t, emptyRange.AllUnix(), nil, nil)
	assertEqual(t, emptyRange.Value(), math.NaN())
}

func TestCursor_BoundsAfterShorterReplacement(t *testing.T) {
	ts := newTestTimeSeries()
	cursor := ts.Range(at(t0+2), at(ts.NextTime().Unix())).Seek(at(t0 + 4))
	assertEqual(t, cursor.Avg(seconds(3)), 11.123)
	data := encodeSeries(t, seriesWithValues(t0, 1, 0, 9))
	if err := ts.BinaryDecode(bufio.NewReader(bytes.NewReader(data))); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, cursor.StartIndex(), 1)
	assertEqual(t, cursor.EndIndex(), 1)
	assertEqual(t, cursor.Value(), math.NaN())
	assertEqual(t, cursor.Avg(seconds(1)), math.NaN())
	assertSamples(t, cursor.AllUnix(), nil, nil)
	assertEqual(t, cursor.Seek(at(0)).Index(), 1)
}

func TestCursor_Seek(t *testing.T) {
	const start = t0 + 3
	ts := seriesWithValues(start, 10, 0, 10, 20, 30)
	cursor := ts.Cursor()
	for _, tc := range []struct {
		time  int64
		index int
	}{
		{0, 0}, {start, 0}, {start + 9, 0}, {start + 10, 1},
		{start + 29, 2}, {start + 30, 2}, {start + 100, 2},
	} {
		assertEqual(t, cursor.Seek(at(tc.time)), cursor)
		assertEqual(t, cursor.Index(), tc.index)
		assertEqual(t, cursor.Time().Unix(), start+int64(tc.index)*10)
	}
	assertEqual(t, cursor.Seek(at(start+10)).Value(), 20.0)
	assertEqual(t, cursor.Seek(at(start+100)).Index(), 2)
	assertEqual(t, cursor.Avg(seconds(19)), 30.0)
	assertEqual(t, cursor.Avg(seconds(0)), 30.0)
}

func TestCursor_EmptyAndShorterReplacement(t *testing.T) {
	ts := New(at(t0), seconds(1), -3)
	cursor := ts.Cursor()
	assertEqual(t, cursor.Index(), 0)
	assertEqual(t, cursor.Time().Unix(), t0)
	assertEqual(t, cursor.Value(), math.NaN())
	assertEqual(t, cursor.Avg(seconds(1)), math.NaN())
	assertSamples(t, cursor.AllUnix(), nil, nil)
	assertEqual(t, cursor.Seek(at(t0+100)).Index(), 0)

	ts.Append(1)
	ts.Append(2)
	cursor.Seek(at(t0 + 1))
	assertEqual(t, cursor.Avg(seconds(2)), 1.5)
	var data bytes.Buffer
	w := bufio.NewWriter(&data)
	replacement := seriesWithValues(t0, 1, 0, 9)
	if err := replacement.BinaryEncode(w); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := ts.BinaryDecode(bufio.NewReader(&data)); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, cursor.Value(), math.NaN())
	assertEqual(t, cursor.Avg(seconds(1)), math.NaN())
	assertEqual(t, cursor.Seek(at(0)).Value(), 9.0)
	assertEqual(t, cursor.Avg(seconds(1)), 9.0)
}

func TestCursor_ConcurrentReaders(t *testing.T) {
	ts := newTestTimeSeries()
	want := ts.Nums()
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			cursor := ts.Cursor()
			for range 20 {
				for timestamp, value := range cursor.AllUnix() {
					index := int(timestamp - t0)
					if cursor.Index() != index || value != want[index] || cursor.Avg(seconds(1)) != value {
						t.Errorf("independent cursor mismatch at timestamp %d", timestamp)
						return
					}
				}
			}
		}()
	}
	workers.Wait()
}

func TestCursor_InvalidInput(t *testing.T) {
	cursor := newTestTimeSeries().Cursor()
	for name, fn := range map[string]func(){
		"negative_time": func() { cursor.Seek(at(-1)) },
		"negative_avg":  func() { cursor.Avg(seconds(-1)) },
	} {
		t.Run(name, func(t *testing.T) { assertPanics(t, fn) })
	}
}
