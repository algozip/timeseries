# timeseries

Compact, fixed-interval price time series for trading and backtesting in Go.

```sh
go get github.com/denisskin/timeseries
```

Requires Go 1.23 or later. No external dependencies.

Run the included example with `go run ./examples/basic` from a checkout.

Prices use `uint32` storage with a shared decimal exponent, taking **4 bytes per
sample** for the price array. Timestamps are computed from a start time and an
interval in seconds. The package includes gap filling, time slicing, downsampling,
merging, cursor-based moving averages, and gzip-compressed delta-varint storage.

## Quick start

```go
package main

import (
    "fmt"
    "log"

    "github.com/denisskin/timeseries"
)

func main() {
    const start int64 = 1700000040
    prices := timeseries.New(start, timeseries.Period1m, -2)
    prices.Append(100.25)
    prices.Append(101.50)
    prices.Push(timeseries.Tick{Time: start + 3*60, Ask: 102.75})
    // The missing third minute repeats 101.50.

    for timestamp, price := range prices.All() {
        fmt.Printf("%d price=%.2f sma=%.4f\n", timestamp, price, prices.Avg(3*60))
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

## Working with series

| Operation | Behavior |
| --- | --- |
| `New(time0, period, exp)` | Create an empty series; align the start down to the period boundary. |
| `Append(price)` | Add at `NextTime()`, rounding to the current precision. |
| `Push(tick)` | Store `tick.Ask` in its interval; fill gaps with the preceding price and increase precision when needed. |
| `All()` | Return an `iter.Seq2[int64, float64]` for ranging over timestamp-price pairs. |
| `ValueOf(timestamp)` | Return the interval's price, or `math.NaN()` outside the series. |
| `SeekTime(timestamp).Avg(seconds)` | Calculate a trailing simple moving average at the selected cursor. |
| `Slice(from, to)` | Select sample timestamps in `[from, to)` as a shared, read-only view. |
| `Clone()` | Copy prices into independent storage. |
| `CleanCopy()` | Share prices with an independent cursor; treat both copies as read-only. |
| `Reduce(period)` | Keep the first sample in each larger interval, including a final partial interval. |
| `MovingAverage(period)` | Produce a series of complete trailing averages, rounded to the current precision. |
| `Diff(shift)` | Return timestamped signed price differences as `Ticks`. |
| `Apply(fn)` | Replace prices with transformed values at the current precision. |
| `Merge(other)` | Append the non-overlapping tail; existing samples win and gaps are rejected. |
| `FetchMergedTimeSeries(series, start, fn)` | Iterate matching timestamps in the common range, updating all cursors. |

`Slice(0, 0)` selects the whole series. Negative slice bounds are offsets in
seconds from `NextTime()`; `Slice(-3600, 0)` selects the last hour. Unaligned slice
bounds select only sample timestamps inside the requested range. Clone a view
before modifying it.

`SeekTime(-1)` selects the last sample. `Avg` uses `max(1, seconds / Step)` samples
and returns `NaN` before a complete window is available. Sequential cursor movement
reuses cached running sums. Iteration and `Fetch` update the cursor before calling
your function, so `Avg` is available inside a backtest loop.

`All()` returns a standard `iter.Seq2[int64, float64]`. Each traversal starts at
the first sample; `break` stops iteration and leaves the cursor on the last
visited sample. The existing `Iterator` callback method also supports ranging
directly with `for timestamp, price := range prices.Iterator`.

`FetchMergedTimeSeries` requires matching steps and interval alignment. It leaves
the input slice intact and reuses its callback's price slice; copy that slice if
you need to retain it. Callbacks must not mutate series data or metadata.

## Precision and limits

A stored integer `p` represents `p * 10^Exp`. For example, `Exp = -2` stores
cents and represents prices from `0` through `42,949,672.95`. `Append` rounds to
the selected increment; `Push` increases precision to accommodate the incoming
price. Increasing precision can exceed the storage limit. Invalid prices,
non-finite values, and overflow panic instead of silently wrapping.

Use `SetExp` to change the precision of an existing series. Set the exported
`Time0`, `Step`, and `Exp` fields only before adding data. Exponents range from
`-308` through `308`; the usable price range also depends on `float64` limits.

The package stores one nonnegative price per interval. It does not preserve
spreads, volume, or OHLCV bars: `Push` reads `Ask`, and returned quotes set both
`Ask` and `Bid` to the stored price. `Diff` returns floating-point `Ticks` so that
falling prices produce negative differences. Series are not safe for concurrent
mutation or cursor-based iteration.

## Storage

`WriteToFile` creates parent directories and writes a temporary file before
replacing the destination. `TimeSeriesRead` validates the format and gzip
checksum. `BinaryEncode` and `BinaryDecode` expose the uncompressed format through
buffered readers and writers; callers must flush the writer.

The `.zts` format remains compatible with the original `finance.TimeSeries`:
six signed varints (version `0`, reserved `0`, sample count, decimal exponent,
start timestamp, and step), followed by signed varint deltas of the `uint32`
prices, all wrapped in gzip for file storage.

Existing `.zi` files can be read directly or renamed to `.zts`; the binary format
is unchanged, and file extensions are not enforced by the reader or writer.

## Origin

Extracted from `finance/finance/timeseries.go` into a standalone Go module.
Exchange integration, symbol lookup, download logic, and application-specific
database paths stay outside this package. The extraction corrects precision
rescaling, transformation, time alignment, moving-average boundaries, and
unsigned subtraction in the original implementation.
`Seek` is named `SeekTime` to avoid a signature conflict with Go's `io.Seeker`.

## License

MIT
