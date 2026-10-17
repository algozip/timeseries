// Package timeseries is optimized for large, fixed-interval time series in
// trading and backtesting, with efficient memory use, fast moving averages,
// and compact .zts files.
//
// Values occupy four bytes per sample using uint32 storage with a shared
// decimal exponent. Timestamps are derived from the start and interval.
// Cursors reuse running sums for moving averages during sequential traversal.
// The .zts format combines implicit timestamps, delta encoding, variable-length
// integers, and gzip compression for compact storage of large time series.
//
// Series traversal is stateless. A Cursor holds independent position, bounds,
// and average caches over shared data. Separate cursors may read immutable
// data concurrently; mutation requires synchronization.
package timeseries
