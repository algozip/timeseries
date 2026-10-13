package timeseries

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

// BinaryEncode writes the original version-zero delta-varint format without
// compression. The caller must flush w. The format contains version, reserved,
// length, exponent, start timestamp, step, and signed deltas of uint32 prices.
func (ts *TimeSeries) BinaryEncode(w *bufio.Writer) error {
	if ts == nil || ts.Step <= 0 || ts.Exp < -308 || ts.Exp > 308 {
		return errors.New("timeseries: invalid series metadata")
	}
	var buf [binary.MaxVarintLen64]byte
	write := func(v int64) error {
		n := binary.PutVarint(buf[:], v)
		_, err := w.Write(buf[:n])
		return err
	}
	for _, v := range []int64{0, 0, int64(ts.Size()), int64(ts.Exp), ts.Time0, ts.Step} {
		if err := write(v); err != nil {
			return err
		}
	}
	var previous int64
	for _, p := range ts.prices {
		if err := write(int64(p) - previous); err != nil {
			return err
		}
		previous = int64(p)
	}
	return nil
}

// BinaryDecode reads the original version-zero format. Invalid metadata,
// unsupported versions, truncated input, and price overflow return errors.
// The receiver is changed only after the entire series has been decoded.
func (ts *TimeSeries) BinaryDecode(r *bufio.Reader) error {
	var header [6]int64
	for i := range header {
		v, err := binary.ReadVarint(r)
		if err != nil {
			return fmt.Errorf("timeseries: read header: %w", err)
		}
		header[i] = v
	}
	version, reserved, n, exp, time0, step := header[0], header[1], header[2], header[3], header[4], header[5]
	if version != 0 || reserved != 0 {
		return fmt.Errorf("timeseries: unsupported format version %d or reserved field %d", version, reserved)
	}
	if n < 0 || n > int64(math.MaxInt) || step <= 0 || exp < -308 || exp > 308 {
		return errors.New("timeseries: invalid series metadata")
	}
	if n > 0 && (n > math.MaxInt64/step || time0 > math.MaxInt64-n*step) {
		return errors.New("timeseries: timestamp range overflows int64")
	}
	result := TimeSeries{Time0: time0, Step: step, Exp: int(exp), prices: make([]uint32, 0, int(min(n, 4096)))}
	var previous int64
	for i := int64(0); i < n; i++ {
		delta, err := binary.ReadVarint(r)
		if err != nil {
			return fmt.Errorf("timeseries: read price %d: %w", i, err)
		}
		if delta < -previous || delta > math.MaxUint32-previous {
			return fmt.Errorf("timeseries: price %d overflows uint32", i)
		}
		previous += delta
		if exp > 0 && math.IsInf(float64(previous)*math.Pow10(int(exp)), 0) {
			return fmt.Errorf("timeseries: price %d overflows float64", i)
		}
		result.prices = append(result.prices, uint32(previous))
	}
	result.cur = max(0, len(result.prices)-1)
	*ts = result
	return nil
}

// WriteToFile writes a gzip-compressed .zi file. Parent directories are created
// as needed. The file is replaced by renaming a completed temporary file in the
// same directory, so encoding failures do not overwrite existing data.
func (ts *TimeSeries) WriteToFile(filename string) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".timeseries-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0o644); err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	w := bufio.NewWriter(gz)
	if err := ts.BinaryEncode(w); err != nil {
		gz.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

// TimeSeriesRead reads a gzip-compressed .zi file and verifies its checksum.
// It accepts files written by the original finance.TimeSeries implementation.
func TimeSeriesRead(filename string) (*TimeSeries, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	r := bufio.NewReader(gz)
	ts := &TimeSeries{}
	if err := ts.BinaryDecode(r); err != nil {
		return nil, err
	}
	if _, err := r.ReadByte(); err != io.EOF {
		if err == nil {
			return nil, errors.New("timeseries: unexpected trailing data")
		}
		return nil, err
	}
	return ts, nil
}

// MergeFromFiles reads and merges files in order. It stops at the first error;
// successful earlier merges remain applied to the receiver.
func (ts *TimeSeries) MergeFromFiles(filenames ...string) error {
	for _, filename := range filenames {
		other, err := TimeSeriesRead(filename)
		if err != nil {
			return fmt.Errorf("timeseries: read %s: %w", filename, err)
		}
		if err := ts.merge(other); err != nil {
			return fmt.Errorf("timeseries: merge %s: %w", filename, err)
		}
	}
	return nil
}
