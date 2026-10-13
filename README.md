# timeseries

Compact, fixed-interval value time series for trading and backtesting in Go.

```sh
go get github.com/denisskin/timeseries
```

Requires Go 1.23 or later. No external dependencies.

Values use `uint32` storage with a shared decimal exponent, taking **4 bytes per
sample** for the value array. Timestamps are computed from a start time and an
interval in seconds. The package includes gap filling, range iteration, copying,
downsampling, merging, independent cursors with moving averages, and
gzip-compressed delta-varint storage.

Run the included example with `go run ./examples/basic` from a checkout.

## Quick start

```go
package main

import (
    "fmt"
    "log"

    "github.com/denisskin/timeseries"
)

func main() {
    const start int64 = 1700000100
    prices := timeseries.New(0, timeseries.Period1m, -2)
    prices.Push(start, 100.25)
    prices.Append(101.50)
    prices.Push(start + 3*60, 102.75)
    // The missing third minute repeats 101.50.

    cursor := prices.Cursor()
    for timestamp, price := range cursor.All() {
        fmt.Printf("%d price=%.2f sma=%.4f\n",
            timestamp, price, cursor.Avg(3*timeseries.Period1m))
    }
    // The average is NaN until three samples are available.

    if err := prices.WriteToFile("prices.zts"); err != nil {
        log.Fatal(err)
    }
    restored, err := timeseries.TimeSeriesRead("prices.zts")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(restored.Nums()) // [100.25 101.5 101.5 102.75]
}
```

## Series and cursors

`TimeSeries` owns the data. Its `All`, `Iterator`, and `Fetch` methods leave
all cursors unchanged. Use these for stateless or nested traversal:

```go
for timestamp, price := range prices.All() {
    fmt.Println(timestamp, price)
}
```

A `Cursor` references a series without copying values and keeps its own position,
index bounds, and moving-average caches. `prices.Cursor()` creates a full-series
cursor at sample zero; its end follows the current series length, including
appended samples.

Select a bounded cursor directly from the series with `Range(from, to)` or
`Tail(period)`. Time boundaries are converted to absolute sample indices once,
when the cursor is created. `StartIndex()` is inclusive and `EndIndex()` is
exclusive. Fixed bounds do not grow when values are appended:

```go
cursor := prices.Range(from.Unix(), to.Unix())
other := prices.Cursor()

fmt.Println(cursor.StartIndex(), cursor.EndIndex())
for timestamp, price := range cursor.All() {
    fmt.Println(timestamp, price, cursor.Avg(timeseries.Period1h))
    // Reading through other or prices.All() does not move cursor.
}

other.SeekTime(prices.LastTime())
fmt.Println(other.Index(), other.Time(), other.Value())
```

`Cursor.All()` visits only that cursor's range, updating its position before each
yield. `Index()` is an absolute index into the original series. `SeekTime` and
`SeekIndex` clamp to the cursor's range. An empty cursor has no current value,
so `Value` and `Avg` return `NaN`. If the series becomes shorter, the available
bounds are clamped to its new length.

`break` leaves the cursor on the last visited sample; an empty traversal leaves
its position unchanged. Each traversal starts at `StartIndex()`. Create a
separate cursor for each backtest, concurrent reader, or nested traversal that
needs its own position. A single cursor must not be used for simultaneous or
nested traversal.

`cursor.Avg(period)` uses `max(1, period / Step)` samples, returns `NaN` before a
complete window is available, and includes earlier history outside a selected
range. Sequential calls reuse cached running sums. Changes to existing values,
precision, or decoded data invalidate cursor caches automatically. Appending
values preserves reusable caches and does not move existing cursors.

Multiple cursors can read an immutable series concurrently. Mutating a series
while iterating it, or sharing one cursor between goroutines, requires external
synchronization. Set the exported metadata fields only before adding data.

## Time boundaries

All timestamps and range bounds are **literal nonnegative Unix seconds**.
Convert `time.Time` values with `.Unix()`. Every range uses `[from, to)`: start
inclusive, end exclusive. Bounds outside the series are clamped. Unaligned
bounds select only sample timestamps inside the requested range. Empty or
reversed ranges produce no samples. Negative timestamps and indices panic.

There are no special timestamp sentinels: `0` means the Unix epoch, so
`Range(0, 0)` and `Slice(0, 0)` are empty. Use `All()` for the complete series,
`Clone()` for a full independent copy, and `Tail(period)` for recent data:

```go
for timestamp, price := range prices.Range(from.Unix(), to.Unix()).All() {
    fmt.Println(timestamp, price)
}
for timestamp, price := range prices.Tail(timeseries.Period1h).All() {
    fmt.Println(timestamp, price)
}
copy := prices.Slice(from.Unix(), to.Unix())
```

Periods are durations in seconds. `Tail(0)` is empty; negative periods panic.
`SeekTime` and `SeekIndex` clamp to the cursor's available samples, including
when seeking beyond its last sample. Use
`prices.Cursor().SeekTime(prices.LastTime())` to select the series' last sample,
without negative sentinels.

## Working with data

| Operation | Behavior |
| --- | --- |
| `New(time0, period, exp)` | Create an empty series. `Append` uses the supplied start; the first `Push` sets its own start. |
| `Push(timestamp, value)` | Store a value in its interval, automatically increasing precision and filling gaps with the previous value. |
| `Append(value)` | Add at `NextTime()`, rounding to the current precision. |
| `ValueOf(timestamp)` | Return the interval's value, or `math.NaN()` outside the series. |
| `Cursor()` | Create a full-series cursor with an independent position and average caches; appended samples are visible. |
| `All()` | Return a stateless `iter.Seq2[int64, float64]` over the complete series. |
| `Range(from, to)`, `Tail(period)` | Create independent cursors with fixed start and end indices, without copying values. |
| `Slice(from, to)` | Copy sample timestamps in `[from, to)` into independent storage. |
| `Clone()` | Copy all metadata and values into independent storage. |
| `Reduce(period)` | Copy the first sample of each larger interval, including a final partial interval. |
| `Apply(fn)` | Replace values with transformed values at the current precision. |
| `Merge(other)` | Feed all incoming values through `Push`; incoming values replace overlaps and gaps are filled. |
| `FetchMergedTimeSeries(series, start, fn)` | Visit matching timestamps in the common range without changing cursors. |

The first `Push` always sets `Time0` to its exact timestamp. It does not create
leading zeros, even if a different start was supplied to `New`. All subsequent
intervals are relative to `Time0`, rather than aligned to the Unix epoch. This
also allows appending to a reduced series without shifting its time grid.

`Reduce` preserves `Time0` and requires a positive multiple of the original step.
A larger period creates independent data; an unchanged period returns the
receiver. New cursors on a reduced or copied series start at the first sample;
existing cursors remain attached to the original.

`Merge` requires matching steps and, for nonempty receivers, matching time-grid
alignment. It accepts gaps, overwrites overlapping intervals with incoming
values, and rejects incoming samples before the receiver's start. Empty
zero-value receivers adopt the incoming step and exponent. The source remains
unchanged. If a later push fails, earlier pushes remain applied.

`FetchMergedTimeSeries` requires matching steps and alignment. It leaves the
input slice and all cursors unchanged, and reuses its callback's value slice;
copy that slice if you need to retain it. Callbacks must not mutate series data.

## Precision and limits

A stored integer `p` represents `p * 10^Exp`. For example, `Exp = -2` stores
cents and represents values from `0` through `42,949,672.95`. `Append` rounds to
the selected increment; `Push` increases precision using the original finance
`PrecisionExp`, which ignores absolute floating-point differences up to `1e-15`.
It uses numeric operations and the original `Pow10` table, without formatting
values as strings.

`SetExp` increases precision using the existing float conversion. Decreasing
precision uses integer division with halfway values rounded up: `1.005` stored
at `Exp=-3` becomes `1.01` at `Exp=-2`. It allocates new storage and leaves the
series unchanged if conversion fails.

Increasing precision can exceed the `uint32` storage limit. Invalid values,
non-finite values, and overflow panic instead of wrapping. Exponents range from
`-308` through `308`; the usable value range also depends on `float64` limits.
The package stores one nonnegative value per interval, without bid/ask or volume.

## Storage

`WriteToFile` creates parent directories and writes a temporary file before
replacing the destination. `TimeSeriesRead` validates the format and gzip
checksum. `BinaryEncode` and `BinaryDecode` expose the uncompressed format through
buffered readers and writers; callers must flush the writer.

The `.zts` format remains compatible with nonnegative-timestamp files from the
original `finance.TimeSeries`: six signed varints (version `0`, reserved `0`,
sample count, decimal exponent, start timestamp, and step), followed by signed
varint deltas of the `uint32` values, wrapped in gzip for file storage.

Existing `.zi` files can be read directly or renamed to `.zts`; file extensions
are not enforced by the reader or writer. Cursor state is never serialized.

## Migration from v0.4

`Range` and `Tail` now belong only to `TimeSeries` and return a bounded `*Cursor`.
Replace `cursor.Range(from, to)` with `cursor := series.Range(from, to)` followed
by a loop over `cursor.All()`. For a one-off loop, use
`for timestamp, price := range series.Range(from, to).All()`.
Likewise, use `series.Tail(period).All()` instead of ranging over `Tail` directly.
`SeekTime` and `SeekIndex` stay inside a bounded cursor's range; `Avg` still
includes earlier series history. `series.Cursor()` retains full-series behavior
and sees newly appended samples.

## Migration from v0.3

- Replace `Push(Tick{Time: t, Ask: p})` with `Push(t, p)`.
- Create `cursor := series.Cursor()` and move `Avg` and `SeekTime` calls to it.
- Iterate `cursor.All()`; select bounded cursors with `series.Range(...)`.
- Replace negative timestamps with explicit bounds or `Tail(period)`.
- Replace zero end-bound sentinels with `NextTime()`, or use `All()`.
- `Slice` now copies values; `CleanCopy` has been removed in favor of cursors.
- `Tick`, `Ticks`, tick-based methods, `Diff`, and `MovingAverage` are removed.
- `New` preserves its exact start, and the first `Push` overrides it.
- `Merge` now overwrites overlaps and fills gaps through `Push`.

## Origin

Extracted from `finance/finance/timeseries.go` into a standalone Go module.
Exchange integration, symbol lookup, download logic, and application-specific
database paths stay outside this package.

## License

MIT
