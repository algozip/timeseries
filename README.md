# timeseries

[![Go](https://github.com/ztape/timeseries/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/ztape/timeseries/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ztape/timeseries.svg)](https://pkg.go.dev/github.com/ztape/timeseries)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**Large time series, small memory footprint, fast moving averages.**

`timeseries` is a Go package for working with price histories and other
fixed-interval time series in trading, research, and backtesting. It keeps
values compact in memory and stores them in compressed `.zts` files, so you
can work through large histories without expanding them into a full
`[]float64` array.

## Why use it?

- **Keep more history in memory.** The value array uses just **4 bytes per
  sample**. Ten million samples need about **40 MB** for values, compared with
  80 MB for `[]float64`, before container overhead. Timestamps are calculated
  from the sampling interval, so there is no timestamp array.
- **Read values without expanding the whole series.** Loading a `.zts` file
  reconstructs the compact value array. Iterators convert values as you visit
  them, without allocating a second array of floating-point values.
- **Calculate moving averages efficiently.** A cursor reuses a running sum
  for each average window as it moves through the history, avoiding a full
  window scan at every sample. Multiple windows can be used in the same loop.
- **Use less disk space.** The `.zts` format is designed for compact storage
  of large time series. File size depends on the data and chosen precision;
  you can save a prepared history once and reuse it across backtests.
- **Explore ranges without copying data.** Select a date range or the latest
  period and iterate over the existing values. Independent cursors let you
  run separate analyses over the same history.

Loading reads the series into memory; it is not a lazy, disk-backed iterator.
The memory saving comes from keeping values compact throughout the analysis.
Each series holds one nonnegative value per fixed interval, with a shared
decimal precision. It is suited to sampled histories rather than irregular
raw ticks or multi-column tables.

## Install

```sh
go get github.com/ztape/timeseries
```

Requires Go 1.23 or later. No external dependencies.

[API reference](https://pkg.go.dev/github.com/ztape/timeseries) ·
[Runnable example](examples/basic/main.go) · [Contributing](CONTRIBUTING.md)

## API and examples

### Create a series and add prices

Choose the sampling interval and decimal precision. An exponent of `-2`
stores values to two decimal places.

```go
package main

import (
    "fmt"
    "time"

    "github.com/ztape/timeseries"
)

func main() {
    start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
    prices := timeseries.New(start, time.Minute, -2)

    prices.Append(100.25)
    prices.Append(101.50)
    prices.Push(start.Add(3*time.Minute), 102.75)

    for timestamp, price := range prices.All() {
        fmt.Println(timestamp, price)
    }
}
```

`Append` adds the next sample. `Push` writes to the interval containing the
specified timestamp and replaces an existing value when necessary. Missing
intervals repeat the preceding value: the example fills the third minute
with `101.50`.

On an empty series, the first `Push` sets the start to its own timestamp.
`Append` uses the start supplied to `New`. `Push` can increase decimal precision
automatically; `Append` rounds to the current precision. Use `SetExp` to change
precision explicitly. Values must fit the supported range for that precision;
invalid values and overflow panic.

### Load a history and calculate moving averages

```go
prices, err := timeseries.TimeSeriesRead("prices.zts")
if err != nil {
    log.Fatal(err)
}

cursor := prices.Cursor()
for timestamp, price := range cursor.All() {
    fast := cursor.Avg(15 * time.Minute)
    slow := cursor.Avg(time.Hour)
    if math.IsNaN(slow) {
        continue
    }
    fmt.Println(timestamp, price, fast, slow)
}
```

The cursor tracks the current sample and caches each moving-average window.
`Avg` returns `NaN` until enough history is available. Windows are measured in
samples: a duration is rounded down to whole intervals, with a minimum of one.
For best performance, reuse the cursor while traversing the series in order.

For loops that work with Unix seconds, use `AllUnix()` on the series or cursor.
It avoids constructing a time value for every sample. Use `All()` when you need
subsecond timestamps; `AllUnix()` truncates them to seconds.

### Work with a date range or recent history

```go
from := prices.StartTime().Add(24 * time.Hour)
to := from.Add(6 * time.Hour)

window := prices.Range(from, to)
for timestamp, price := range window.All() {
    fmt.Println(timestamp, price, window.Avg(time.Hour))
}

for timestamp, price := range prices.Tail(time.Hour).All() {
    fmt.Println(timestamp, price)
}
```

`Range` includes sample timestamps in `[from, to)`. `Tail` selects the final
period of the series. Both return cursors without copying values. Their bounds
are fixed when created. A moving average can include earlier samples outside
the selected range, so it has access to the preceding history.

Use `Slice(from, to)` when you need an independent copy to modify, or `Clone()`
to copy the entire series.

### Look up a value or position a cursor

```go
at := prices.StartTime().Add(30 * time.Minute)
price := prices.ValueOf(at)

cursor := prices.Cursor().Seek(at)
fmt.Println(price, cursor.Time(), cursor.Value(), cursor.Avg(15*time.Minute))
```

`ValueOf` returns the value for the interval containing the requested time,
or `NaN` outside the series. `Seek` positions the cursor within its bounds.
Calling `All` on a cursor starts traversal at the beginning of its range.

Use `Size()`, `StartTime()`, `LastTime()`, `NextTime()`, `Step()`, and `Duration()`
to inspect the series. `NextTime()` is the exclusive end; `Duration()` includes
the last sample's interval.

### Save, combine, and resample histories

```go
if err := prices.WriteToFile("prices.zts"); err != nil {
    log.Fatal(err)
}

incoming, err := timeseries.TimeSeriesRead("updates.zts")
if err != nil {
    log.Fatal(err)
}
prices.Merge(incoming)

hourly := prices.Reduce(time.Hour)
```

`Merge` applies incoming values to the series: overlapping values are replaced
and gaps repeat the preceding value. Both series must have matching intervals
and aligned timestamps; incoming data cannot start before a nonempty receiver.
`MergeFromFiles` combines updates directly from file paths.

`Reduce` keeps the first sample in each larger interval. The new interval must
be a multiple of the original. This is downsampling, not OHLC aggregation or
averaging.

### Keep memory use predictable

Iterate with `All`, `AllUnix`, or a cursor to retain compact storage. `Nums()`
creates a full `[]float64` copy and is useful when another library requires one.
`Slice`, `Clone`, and reduction to a larger interval also allocate new storage.

Use separate cursors for independent analyses. Concurrent reads are supported
when the series is immutable; mutation and sharing a cursor across goroutines
require external synchronization. Do not modify the series inside an iteration.

## License

Copyright (c) 2026 Denis Glazov.

Licensed under the [Apache License, Version 2.0](LICENSE).
