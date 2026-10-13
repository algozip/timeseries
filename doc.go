// Package timeseries provides compact, regularly spaced value series for
// trading and backtesting. Values use uint32 storage with a shared decimal
// exponent; timestamps are derived from an integer nanosecond start and step.
//
// Public time APIs use time.Time and time.Duration. All yields exact UTC
// timestamps; AllUnix yields Unix seconds and offers an integer-only fast path
// for whole-second steps. Series traversal is stateless. A Cursor holds
// independent position, bounds, and average caches over shared data. Separate
// cursors may read immutable data concurrently; mutation requires synchronization.
package timeseries
