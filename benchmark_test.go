package timeseries

import (
	"testing"
	"time"
)

var benchmarkSink float64

func benchmarkSeries() *TimeSeries {
	ts := New(time.Unix(1577836800, 0), time.Second, -3)
	ts.prices = make([]uint32, 1<<16)
	for i := range ts.prices {
		ts.prices[i] = uint32(10000 + i%1000)
	}
	return ts
}

func BenchmarkSeriesAllUnix(b *testing.B) {
	ts := benchmarkSeries()
	b.ReportAllocs()
	b.SetBytes(int64(ts.Size() * 4))
	b.ResetTimer()
	var sum float64
	for i := 0; i < b.N; i++ {
		for timestamp, value := range ts.AllUnix() {
			sum += value + float64(timestamp&1)
		}
	}
	benchmarkSink = sum
}

func BenchmarkCursorAllUnix(b *testing.B) {
	ts := benchmarkSeries()
	cursor := ts.Cursor()
	b.ReportAllocs()
	b.SetBytes(int64(ts.Size() * 4))
	b.ResetTimer()
	var sum float64
	for i := 0; i < b.N; i++ {
		for timestamp, value := range cursor.AllUnix() {
			sum += value + float64(timestamp&1)
		}
	}
	benchmarkSink = sum
}

func BenchmarkCursorAvg(b *testing.B) {
	ts := benchmarkSeries()
	cursor := ts.Cursor().Seek(ts.LastTime())
	period := time.Minute
	cursor.Avg(period)
	b.ReportAllocs()
	b.ResetTimer()
	var sum float64
	for i := 0; i < b.N; i++ {
		sum += cursor.Avg(period)
	}
	benchmarkSink = sum
}

func BenchmarkCursorAllUnixAvg(b *testing.B) {
	ts := benchmarkSeries()
	cursor := ts.Cursor()
	period := time.Minute
	b.ReportAllocs()
	b.SetBytes(int64(ts.Size() * 4))
	b.ResetTimer()
	var sum float64
	for i := 0; i < b.N; i++ {
		for _, value := range cursor.AllUnix() {
			average := cursor.Avg(period)
			if cursor.Index() >= 59 {
				sum += value + average
			}
		}
	}
	benchmarkSink = sum
}

func BenchmarkSeriesAll(b *testing.B) {
	ts := benchmarkSeries()
	b.ReportAllocs()
	b.SetBytes(int64(ts.Size() * 4))
	b.ResetTimer()
	var sum float64
	for i := 0; i < b.N; i++ {
		for timestamp, value := range ts.All() {
			sum += value + float64(timestamp.Unix()&1)
		}
	}
	benchmarkSink = sum
}
