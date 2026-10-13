package timeseries

import (
	"math"
	"time"
)

// unixNano validates before converting: time.Time.UnixNano is undefined for
// dates outside its int64 range. The package supports the Unix epoch onwards.
func unixNano(t time.Time) int64 {
	seconds, nanos := t.Unix(), int64(t.Nanosecond())
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	const maxNanos = math.MaxInt64 % int64(time.Second)
	if seconds < 0 || seconds > maxSeconds || seconds == maxSeconds && nanos > maxNanos {
		panic("timeseries: timestamp is outside the supported Unix nanosecond range")
	}
	return seconds*int64(time.Second) + nanos
}

func nanoTime(nanos int64) time.Time { return time.Unix(0, nanos).UTC() }
