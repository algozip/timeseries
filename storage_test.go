package timeseries

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func binaryFixture(values ...int64) []byte {
	var data []byte
	for _, value := range values {
		data = binary.AppendVarint(data, value)
	}
	return data
}

func encodeSeries(t *testing.T, ts *TimeSeries) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := bufio.NewWriter(&data)
	if err := ts.BinaryEncode(writer); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestTimeSeries_BinaryFormatCompatibility(t *testing.T) {
	// Original finance format: version, reserved, count, exponent, start, step,
	// then signed deltas for the integer values 12345, 12300, and 12350.
	fixture := binaryFixture(0, 0, 3, -2, t0, 60, 12345, -45, 50)
	ts := seriesWithValues(t0, 60, -2, 123.45, 123, 123.50)
	if got := encodeSeries(t, ts); !bytes.Equal(got, fixture) {
		t.Fatalf("wire format: got %x; want %x", got, fixture)
	}
	var decoded TimeSeries
	if err := decoded.BinaryDecode(bufio.NewReader(bytes.NewReader(fixture))); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, decoded.StartTime().Unix(), t0)
	assertEqual(t, int64(decoded.Step()/time.Second), int64(60))
	assertEqual(t, decoded.Exp, -2)
	assertValues(t, decoded.Nums(), []float64{123.45, 123, 123.50})
	assertEqual(t, decoded.Cursor().Index(), 0)

	filename := filepath.Join(t.TempDir(), "original.zts")
	if err := os.WriteFile(filename, gzipBytes(t, fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := TimeSeriesRead(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertValues(t, loaded.Nums(), decoded.Nums())
}

func TestTimeSeries_FileRoundTrip(t *testing.T) {
	subsecond := New(at(t0).Add(123*time.Nanosecond), 250*time.Microsecond, -2)
	subsecond.Append(1.25)
	subsecond.Append(2.50)
	for _, tc := range []struct {
		name string
		ts   *TimeSeries
	}{
		{"empty", New(at(t0), seconds(1), -3)},
		{"fractional", newTestTimeSeries()},
		{"subsecond", subsecond},
		{"positive_exponent", seriesWithValues(t0+3, 10, 2, 100, 200, 0, 300)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "nested", "prices.zts")
			if err := tc.ts.WriteToFile(filename); err != nil {
				t.Fatal(err)
			}
			loaded, err := TimeSeriesRead(filename)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, loaded.StartTime(), tc.ts.StartTime())
			assertEqual(t, loaded.Step(), tc.ts.Step())
			assertEqual(t, loaded.Exp, tc.ts.Exp)
			assertValues(t, loaded.Nums(), tc.ts.Nums())
		})
	}
}

func TestTimeSeries_BinaryDecodeRejectsInvalidInput(t *testing.T) {
	cases := map[string][]byte{
		"empty_header":       nil,
		"truncated_header":   binaryFixture(0, 0),
		"version":            binaryFixture(2, 0, 0, 0, t0, 1),
		"reserved":           binaryFixture(0, 1, 0, 0, t0, 1),
		"negative_count":     binaryFixture(0, 0, -1, 0, t0, 1),
		"negative_time":      binaryFixture(0, 0, 0, 0, -1, 1),
		"zero_step":          binaryFixture(0, 0, 0, 0, t0, 0),
		"invalid_exponent":   binaryFixture(0, 0, 0, 309, t0, 1),
		"timestamp_overflow": binaryFixture(0, 0, 2, 0, math.MaxInt64-1, 1, 0, 0),
		"truncated_values":   binaryFixture(0, 0, 2, 0, t0, 1, 10),
		"negative_value":     binaryFixture(0, 0, 1, 0, t0, 1, -1),
		"uint32_overflow":    binaryFixture(0, 0, 1, 0, t0, 1, int64(math.MaxUint32)+1),
		"float64_overflow":   binaryFixture(0, 0, 1, 308, t0, 1, 2),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			ts := newTestTimeSeries()
			before := ts.Clone()
			cursor := ts.Cursor().Seek(at(t0 + 4))
			assertEqual(t, cursor.Avg(seconds(3)), 11.123)
			if err := ts.BinaryDecode(bufio.NewReader(bytes.NewReader(data))); err == nil {
				t.Fatal("expected a decoding error")
			}
			assertEqual(t, ts.StartTime().Unix(), before.StartTime().Unix())
			assertEqual(t, int64(ts.Step()/time.Second), int64(before.Step()/time.Second))
			assertEqual(t, ts.Exp, before.Exp)
			assertValues(t, ts.Nums(), before.Nums())
			assertEqual(t, cursor.Avg(seconds(3)), 11.123)
		})
	}
}

func TestTimeSeries_ReadRejectsDamagedFiles(t *testing.T) {
	payload := encodeSeries(t, newTestTimeSeries())
	compressed := gzipBytes(t, payload)
	badChecksum := bytes.Clone(compressed)
	badChecksum[len(badChecksum)-8] ^= 1
	for name, data := range map[string][]byte{
		"not_gzip":      []byte("not a time series"),
		"truncated":     compressed[:len(compressed)-1],
		"bad_checksum":  badChecksum,
		"trailing_data": gzipBytes(t, append(bytes.Clone(payload), 0)),
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "damaged.zts")
			if err := os.WriteFile(filename, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := TimeSeriesRead(filename); err == nil {
				t.Fatal("expected a file decoding error")
			}
		})
	}
}

func TestTimeSeries_WriteFailurePreservesDestination(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "prices.zts")
	original := newTestTimeSeries()
	if err := original.WriteToFile(filename); err != nil {
		t.Fatal(err)
	}
	invalid := &TimeSeries{}
	if err := invalid.WriteToFile(filename); err == nil {
		t.Fatal("expected an encoding error")
	}
	loaded, err := TimeSeriesRead(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertValues(t, loaded.Nums(), original.Nums())
}

func TestTimeSeries_MergeFromFiles(t *testing.T) {
	dir := t.TempDir()
	first := seriesWithValues(t0+4, 1, -3, 20.123, 21.123)
	second := seriesWithValues(t0+6, 1, -3, 22.123)
	filenames := []string{filepath.Join(dir, "first.zts"), filepath.Join(dir, "second.zts")}
	for i, ts := range []*TimeSeries{first, second} {
		if err := ts.WriteToFile(filenames[i]); err != nil {
			t.Fatal(err)
		}
	}
	ts := newTestTimeSeries()
	if err := ts.MergeFromFiles(filenames...); err != nil {
		t.Fatal(err)
	}
	assertValues(t, ts.Nums(), []float64{10.123, 9.123, 10.123, 11.123, 20.123, 21.123, 22.123})

	ts = newTestTimeSeries()
	if err := ts.MergeFromFiles(filenames[0], filepath.Join(dir, "missing.zts")); err == nil {
		t.Fatal("expected a missing-file error")
	}
	assertValues(t, ts.Nums(), []float64{10.123, 9.123, 10.123, 11.123, 20.123, 21.123})
}
