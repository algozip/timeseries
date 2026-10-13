// Package timeseries provides compact, regularly spaced price series for
// trading and backtesting. Prices are stored as uint32 values with a shared
// decimal exponent; timestamps are derived from a start time and a fixed step.
//
// Times and periods use Unix seconds. A series stores one nonnegative price per
// interval, not OHLCV bars or bid/ask spreads. Iteration updates the cursor used
// by Avg. Series are not safe for concurrent mutation or cursor-based iteration.
package timeseries
