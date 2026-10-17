# timeseries

[![Go](https://github.com/ztape/timeseries/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/ztape/timeseries/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ztape/timeseries.svg)](https://pkg.go.dev/github.com/ztape/timeseries)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A Go package optimized for large, fixed-interval time series in trading and
backtesting, with efficient memory use, fast moving averages, and compact
`.zts` files.

- **Memory-efficient:** values occupy just **4 bytes per sample**, using `uint32`
  storage with a shared decimal exponent. Timestamps are derived from the start
  and interval instead of being stored for each sample.
- **Fast moving averages:** cursors reuse a running sum for each window during
  sequential traversal, avoiding repeated scans of the entire window.
- **Compact files:** the **`.zts` format** is optimized for storing large time
  series. It combines implicit timestamps, delta encoding, variable-length
  integers, and gzip compression to reduce file size.

```sh
go get github.com/ztape/timeseries
```

Requires Go 1.23 or later. No external dependencies.

[API reference](https://pkg.go.dev/github.com/ztape/timeseries) ·
[Example](examples/basic/main.go) · [Contributing](CONTRIBUTING.md)

Run the included example with `go run ./examples/basic`.

## Quick start

```go
package main

import (
    "fmt"
    "time"

    "github.com/ztape/timeseries"
)

func main() {
    start := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
    prices := timeseries.New(start, time.Minute, -2)
    prices.Push(start, 100.25)
    prices.Append(101.50)
    prices.Push(start.Add(3*time.Minute), 102.75)
    // The missing third minute repeats 101.50.

    cursor := prices.Range(start, start.Add(4*time.Minute))
    for timestamp, price := range cursor.All() {
        fmt.Println(timestamp, price, cursor.Avg(3*time.Minute))
    }
    // The average is NaN until three samples are available.
}
```

## Time and duration

| Operation | Time API |
| --- | --- |
| `New(start, step, exp)` | `start time.Time`, `step time.Duration` |
| `StartTime()`, `LastTime()`, `NextTime()` | Return `time.Time` in UTC |
| `Step()`, `Duration()` | Return `time.Duration` |
| `Push(timestamp, value)`, `ValueOf(timestamp)` | Accept `time.Time` |
| `Range(from, to)`, `Slice(from, to)` | Accept two `time.Time` bounds |
| `Tail(duration)`, `Reduce(step)`, `cursor.Avg(duration)` | Accept `time.Duration` |
| `cursor.Seek(timestamp)`, `cursor.Time()` | Accept or return `time.Time` |
| `All()` | Return `iter.Seq2[time.Time, float64]` |
| `AllUnix()` | Return `iter.Seq2[int64, float64]` with Unix **seconds** |

`Duration()` covers all sample intervals, including the final interval.
`NextTime()` is the exclusive end. On an empty series, `LastTime()` and
`NextTime()` return `StartTime()`. `Step()` must be positive; nanosecond steps
are supported. Use standard durations such as `time.Minute`, `4*time.Hour`, or
`250*time.Microsecond`.

Input locations are accepted, and returned timestamps are normalized to UTC.
Monotonic clock readings and the input location are not stored. Supported times
range from the Unix epoch through `2262-04-11T23:47:16.854775807Z`, the largest
nonnegative int64 Unix nanosecond timestamp. Invalid dates or a series whose
exclusive end would overflow this range are rejected. `time.Time{}` is outside
this range and is not a special marker.

## Iteration and speed

`All()` preserves full timestamp precision. `AllUnix()` returns seconds exactly
like `time.Time.Unix()` and avoids constructing a `time.Time` per sample. When
the step is a whole number of seconds, time conversion occurs once before the
loop and the loop increments an integer timestamp. For subsecond steps,
`AllUnix()` truncates timestamps to seconds, so multiple samples may have the
same timestamp; use `All()` when full precision is needed.

```go
for timestamp, price := range prices.AllUnix() {
    fmt.Println(timestamp, price)
}
```

Both iterators support `break` and repeat traversal from the beginning. Series
iteration leaves existing cursors unchanged. Loop bodies must not mutate the
series.

`Iterator`, `Fetch`, `FetchFromTo`, and `Apply` callbacks receive `time.Time`.
`FetchMergedTimeSeries` also uses `time.Time` for its starting bound and callback.

## Series and cursors

`TimeSeries` owns the data. A `Cursor` references it without copying values and
keeps independent position, index bounds, and moving-average caches.

`series.Cursor()` creates a full-series cursor that sees appended samples.
`series.Range(from, to)` and `series.Tail(duration)` create bounded cursors with
fixed indices; their ends do not grow after insertion. `StartIndex()` is
inclusive, `EndIndex()` is exclusive, and `Index()` is absolute in the source
series. `Seek` stays within the cursor's bounds.

```go
cursor := prices.Range(from, to)
for timestamp, price := range cursor.AllUnix() {
    fmt.Println(timestamp, price, cursor.Avg(time.Hour))
}
```

`cursor.All()` and `cursor.AllUnix()` update only that cursor before yielding.
`break` leaves its position on the last visited sample. Empty traversal leaves
the position unchanged. Each traversal begins at `StartIndex()`. If the series
becomes shorter, available bounds are clamped to its new length. Empty cursors
return `NaN` from `Value()` and `Avg()`.

`Avg(duration)` uses `max(1, duration / step)` samples and returns `NaN` until a
complete window is available. Its window includes earlier history outside the
selected range. Sequential calls reuse cached running sums. Overwriting values,
changing precision, or decoding replacement data invalidates caches; appending
values preserves them and does not move existing cursors.

Separate cursors can read an immutable series concurrently. Use separate cursors
for nested traversal. A shared cursor or series mutation needs external
synchronization. `Exp` must only be assigned before adding data; use `SetExp`
afterwards. Start and step are private and exposed through read-only getters.

## Ranges and copies

All ranges use `[from, to)`: start inclusive, end exclusive. Bounds preserve
nanosecond precision, and only sample timestamps inside the range are selected.
Bounds outside the series are clamped. Empty or reversed ranges are empty.
There are no special date sentinels.

`Slice(from, to)` copies the selected values into independent storage. `Clone()`
copies the complete series. `Tail(duration)` selects recent data without copying;
`Tail(0)` is empty, and negative durations panic.

`Reduce(step)` keeps the first sample of each larger interval, including a final
partial interval. The new step must be a positive multiple of the original.
A larger step creates independent data and preserves the start; an unchanged
step returns the receiver. New cursors begin at the first sample.

## Insertion and merging

`Append(value)` adds at `NextTime()`, rounding to the current precision.
`Push(timestamp, value)` increases precision when necessary and stores the value
in its interval. The first `Push` on an empty series sets its exact start to the
incoming timestamp, overriding the constructor's start without leading zeros.
Intervals are relative to that start. Gaps repeat the preceding value, and
existing intervals are overwritten.

`Merge(other)` feeds all incoming values through `Push`. Steps and time-grid
alignment must match. Incoming values replace overlaps, gaps are filled, and
samples before a nonempty receiver's start are rejected. An empty zero-value
receiver adopts the source step and exponent. The source is unchanged. If a
later push fails, earlier pushes remain applied.

`FetchMergedTimeSeries` visits the common time range of all input series, using
matching steps and alignment. It leaves the input slice and existing cursors
unchanged. Its callback value slice is reused; copy it to retain values.

## Precision

A stored integer `p` represents `p * 10^Exp`. For example, `Exp=-2` stores cents
and represents values from `0` through `42,949,672.95`. The original numeric
`PrecisionExp` ignores absolute floating-point differences up to `1e-15`.
`SetExp` increases precision using the existing float conversion; decreasing
precision uses integer arithmetic with halfway values rounded up. A value of
`1.005` at exponent `-3` becomes `1.01` at exponent `-2`.

Invalid or non-finite values and overflow panic rather than wrap. Exponents
range from `-308` through `308`, subject to `uint32` and `float64` capacity.
The package stores one nonnegative value per interval, without bid/ask or volume.

## Storage

`WriteToFile` writes a gzip-compressed `.zts` file through a completed temporary
file before replacing the destination. `TimeSeriesRead` verifies metadata and
the gzip checksum. `BinaryEncode` and `BinaryDecode` expose the uncompressed
format; callers must flush the buffered writer.

The header consists of six signed varints: version, reserved zero, sample count,
decimal exponent, start timestamp, and step. Signed varint deltas of the `uint32`
values follow the header.

- Version `0` stores start and step in seconds. Whole-second series continue to
  use this format and remain byte-compatible with previous releases.
- Version `1` stores start and step in nanoseconds, preserving subsecond data.
- The reader supports both versions and validates conversion and timestamp
  overflow. Older readers cannot read version `1` files.

File extensions are not enforced. Cursor state is never serialized.

## Validation and benchmarks

GitHub Actions builds, vets, and runs tests with the race detector on Linux,
macOS, and Windows. Linux also checks Go 1.23 compatibility, the previous
stable release, formatting, and module metadata.

```sh
go test -race ./...
go vet ./...
go test -run '^$' -bench . -benchmem .
```

## Migration from v0.6

- Pass `time.Time` directly to constructors, insertion, queries, and seeking.
- Use `time.Duration` for sampling steps, reductions, tails, and average windows.
- Replace time metadata fields with `StartTime()` and `Step()`.
- Use `Duration()` for the total covered duration.
- `All()` now yields `time.Time`; use `AllUnix()` for Unix seconds.
- Callback-based traversal and transformations now receive `time.Time`.
- Sampling interval types, constants, and parsing helpers have been removed;
  use the standard `time` package.

## Origin and license

Extracted from `finance/finance/timeseries.go` into a standalone Go module.
Exchange integration and application-specific database paths stay outside the
package.

Copyright (c) 2026 Denis Glazov.

Licensed under the [Apache License, Version 2.0](LICENSE)
(SPDX: `Apache-2.0`).
