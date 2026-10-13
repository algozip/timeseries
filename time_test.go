package timeseries

import (
	"bufio"
	"bytes"
	"iter"
	"math"
	"testing"
	"time"
)

func TestTimeSeries_NativeTimeAndUnixIteration(t *testing.T) {
	for _, tc := range []struct {
		name string
		step time.Duration
		unix []int64
	}{
		{"whole_second_step", time.Second, []int64{t0, t0 + 1, t0 + 2}},
		{"subsecond_step", 200 * time.Millisecond, []int64{t0, t0 + 1, t0 + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := at(t0).Add(900 * time.Millisecond)
			ts := New(start, tc.step, -2)
			ts.Append(1.25)
			ts.Append(2.50)
			ts.Append(3.75)
			assertEqual(t, ts.StartTime(), start)
			assertEqual(t, ts.Step(), tc.step)
			assertEqual(t, ts.Duration(), 3*tc.step)
			assertEqual(t, ts.LastTime(), start.Add(2*tc.step))
			assertEqual(t, ts.NextTime(), start.Add(3*tc.step))
			assertSamples(t, ts.AllUnix(), tc.unix, []float64{1.25, 2.50, 3.75})
			cursor := ts.Cursor()
			assertSamples(t, cursor.AllUnix(), tc.unix, []float64{1.25, 2.50, 3.75})
			for _, seq := range []iter.Seq2[time.Time, float64]{ts.All(), ts.Iterator, cursor.All()} {
				visits := 0
				for timestamp, value := range seq {
					assertEqual(t, timestamp, start.Add(time.Duration(visits)*tc.step))
					assertEqual(t, timestamp.Location(), time.UTC)
					assertEqual(t, value, []float64{1.25, 2.50, 3.75}[visits])
					visits++
				}
				assertEqual(t, visits, 3)
				visits = 0
				for range seq {
					visits++
					break
				}
				assertEqual(t, visits, 1)
			}
			for timestamp, _ := range cursor.All() {
				assertEqual(t, cursor.Time(), timestamp)
			}
		})
	}
}

func TestTimeSeries_NanosecondRange(t *testing.T) {
	start := at(t0).Add(123 * time.Nanosecond)
	ts := New(start, 250*time.Millisecond, 0)
	for value := 1; value <= 5; value++ {
		ts.Append(float64(value))
	}
	from, to := start.Add(time.Nanosecond), start.Add(500*time.Millisecond+time.Nanosecond)
	cursor := ts.Range(from, to)
	assertEqual(t, cursor.StartIndex(), 1)
	assertEqual(t, cursor.EndIndex(), 3)
	assertEqual(t, cursor.Seek(from).Index(), 1)
	assertEqual(t, cursor.Seek(to).Index(), 2)
	visits := 0
	for timestamp, value := range cursor.All() {
		assertEqual(t, timestamp, start.Add(time.Duration(visits+1)*250*time.Millisecond))
		assertEqual(t, value, float64(visits+2))
		assertEqual(t, cursor.Avg(500*time.Millisecond), float64(visits)+1.5)
		visits++
	}
	assertEqual(t, visits, 2)
	assertEqual(t, ts.ValueOf(start.Add(250*time.Millisecond-time.Nanosecond)), 1.0)
	assertEqual(t, ts.ValueOf(start.Add(250*time.Millisecond)), 2.0)
	copy := ts.Slice(from, to)
	assertEqual(t, copy.StartTime(), start.Add(250*time.Millisecond))
	assertValues(t, copy.Nums(), []float64{2, 3})
	assertValues(t, ts.Slice(start, start.Add(time.Nanosecond)).Nums(), []float64{1})
	assertEqual(t, ts.Cursor().Seek(start.Add(250*time.Millisecond-time.Nanosecond)).Index(), 0)
	assertEqual(t, ts.Tail(500*time.Millisecond).StartIndex(), 3)

	reduced := ts.Reduce(500 * time.Millisecond)
	assertEqual(t, reduced.StartTime(), start)
	assertValues(t, reduced.Nums(), []float64{1, 3, 5})
	reduced.Push(reduced.NextTime(), 7)
	assertValues(t, reduced.Nums(), []float64{1, 3, 5, 7})
}

func TestTimeSeries_TimezoneAndSubsecondPush(t *testing.T) {
	start := time.Date(2026, time.October, 13, 12, 0, 0, 321, time.FixedZone("UTC+3", 3*3600))
	ts := New(at(0), 100*time.Microsecond, -2)
	ts.Push(start, 1.25)
	ts.Push(start.Add(250*time.Microsecond), 2.50)
	assertEqual(t, ts.StartTime(), start.UTC())
	assertEqual(t, ts.LastTime(), start.UTC().Add(200*time.Microsecond))
	assertValues(t, ts.Nums(), []float64{1.25, 1.25, 2.50})
	assertEqual(t, ts.ValueOf(start.Add(150*time.Microsecond)), 1.25)
	cursor := ts.Range(start, start.Add(300*time.Microsecond))
	assertEqual(t, cursor.EndIndex(), 3)
	var times []time.Time
	ts.Apply(func(timestamp time.Time, value float64) float64 {
		times = append(times, timestamp)
		return value + 1
	})
	for i, timestamp := range times {
		assertEqual(t, timestamp, start.UTC().Add(time.Duration(i)*100*time.Microsecond))
	}
}

func TestTimeSeries_NanosecondStorage(t *testing.T) {
	start := at(t0).Add(123 * time.Nanosecond)
	step := 250 * time.Microsecond
	ts := New(start, step, -2)
	for _, value := range []float64{123.45, 123, 123.50} {
		ts.Append(value)
	}
	data := encodeSeries(t, ts)
	want := binaryFixture(1, 0, 3, -2, start.UnixNano(), int64(step), 12345, -45, 50)
	if !bytes.Equal(data, want) {
		t.Fatalf("nanosecond wire format: got %x; want %x", data, want)
	}
	var decoded TimeSeries
	if err := decoded.BinaryDecode(bufio.NewReader(bytes.NewReader(data))); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, decoded.StartTime(), start)
	assertEqual(t, decoded.Step(), step)
	assertEqual(t, decoded.NextTime(), start.Add(3*step))
	assertValues(t, decoded.Nums(), ts.Nums())
}

func TestTimeSeries_TimeLimits(t *testing.T) {
	for _, timestamp := range []time.Time{time.Time{}, time.Unix(-1, 0), time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)} {
		assertPanics(t, func() { New(timestamp, time.Second, 0) })
	}
	start := time.Unix(0, math.MaxInt64-2).UTC()
	ts := New(start, 2*time.Nanosecond, 0)
	ts.Append(1)
	assertEqual(t, ts.NextTime(), time.Unix(0, math.MaxInt64).UTC())
	assertPanics(t, func() { ts.Append(2) })
	assertEqual(t, ts.Size(), 1)
	assertPanics(t, func() { ts.Push(time.Unix(0, math.MaxInt64), 2) })
	assertPanics(t, func() { ts.Reduce(3 * time.Nanosecond) })
	for _, data := range [][]byte{
		binaryFixture(0, 0, 0, 0, math.MaxInt64/int64(time.Second)+1, 1),
		binaryFixture(0, 0, 0, 0, 0, math.MaxInt64/int64(time.Second)+1),
		binaryFixture(1, 0, 2, 0, math.MaxInt64-1, 1, 1, 1),
	} {
		var decoded TimeSeries
		if err := decoded.BinaryDecode(bufio.NewReader(bytes.NewReader(data))); err == nil {
			t.Fatal("expected an overflowing timestamp error")
		}
	}
}
