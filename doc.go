// Package timeseries provides compact, regularly spaced price series for
// trading and backtesting. Prices are stored as uint32 values with a shared
// decimal exponent; timestamps are derived from a start time and a fixed step.
//
// Times are literal nonnegative Unix seconds; periods are durations in seconds.
// A series stores one nonnegative value per interval. Series iteration is
// stateless. A Cursor holds an independent position and moving-average caches
// while sharing the series' data. Separate cursors may read immutable data
// concurrently. Series mutation and sharing a cursor require synchronization.
package timeseries
