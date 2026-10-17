// Package timeseries provides memory-efficient time series, fast moving
// averages, and compact .zts files for trading and backtesting.
//
// Large histories stay compact throughout analysis: the value array uses four
// bytes per sample, and timestamps are derived from a shared start and interval.
// Iteration converts individual values without expanding the series into a
// full []float64. Loading a .zts file reconstructs the compact array in memory;
// it does not stream samples from disk.
//
// # Creating a series
//
// Use [New] to choose a start, sampling interval, and decimal precision. An
// exponent of -2 represents values to two decimal places. [TimeSeries.Append]
// adds the next sample; [TimeSeries.Push] writes at a timestamp and fills gaps
// with the preceding value. Each series holds one nonnegative value per interval.
//
// # Iteration and moving averages
//
// Use [TimeSeries.All] to iterate timestamp-value pairs, or [TimeSeries.AllUnix]
// for Unix seconds. Both avoid allocating a full floating-point array.
// [TimeSeries.Cursor] creates an independent position for analysis. While
// traversing [Cursor.All], call [Cursor.Avg] with one or more window durations.
// Each window reuses a running sum as the cursor advances. An average is NaN
// until the full window is available.
//
// [TimeSeries.Range] and [TimeSeries.Tail] select ranges without copying values.
// A cursor's averages can use history preceding its selected range. Use
// [TimeSeries.Slice] or [TimeSeries.Clone] when an independent copy is needed.
// [TimeSeries.Nums] explicitly allocates a []float64 for interoperability.
//
// # Saving and loading
//
// [TimeSeries.WriteToFile] saves a compact .zts history for reuse across analyses.
// [TimeSeriesRead] loads it into compact in-memory storage. [TimeSeries.Merge]
// combines aligned histories; [TimeSeries.Reduce] downsamples by retaining the
// first value in each larger interval.
//
// # Concurrent use
//
// Independent cursors can read an immutable series concurrently. Mutation and
// sharing a cursor across goroutines require external synchronization. Do not
// mutate a series while iterating over it.
package timeseries
