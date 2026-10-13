package timeseries

import (
	"math"
	"testing"
)

func Test_PrecisionExp(t *testing.T) {
	assertEqual(t, PrecisionExp(0.1+0.2), -1)
	computed := 0.1
	computed += 0.2
	assertEqual(t, PrecisionExp(computed), -1)

	for _, tc := range []struct {
		value float64
		exp   int
	}{
		{-999996.3, -1}, {999996.3, -1}, {1456.32, -2}, {1.302, -3},
		{0.9211, -4}, {0.000001, -6}, {0.000000001, -9}, {0, 0},
		{7442528, 0}, {8738518, 0}, {8738510, 1}, {8738500, 2},
		{-8738500, 2}, {-80.85, -2},
	} {
		assertEqual(t, PrecisionExp(tc.value), tc.exp)
	}
	ts := New(t0, Period1s, -1)
	ts.Push(t0, computed)
	assertEqual(t, ts.Exp, -1)
	assertEqual(t, ts.ValueOf(t0), 0.3)
}

func TestPow10(t *testing.T) {
	for _, exp := range []int{-25, -20, -3, -1, 0, 1, 3, 20, 25} {
		got, want := Pow10(exp), math.Pow10(exp)
		if math.Abs(got/want-1) > 1e-15 {
			t.Fatalf("Pow10(%d): got %.17g; want %.17g", exp, got, want)
		}
	}
}

func TestPeriod(t *testing.T) {
	for _, tc := range []struct {
		text   string
		period Period
	}{
		{"1s", Period1s}, {"15s", 15}, {"1m", Period1m}, {"15m", 15 * Period1m},
		{"1h", Period1h}, {"4h", Period4h}, {"1d", Period1D}, {"2d", 2 * Period1D},
	} {
		assertEqual(t, PeriodByString(tc.text), tc.period)
		assertEqual(t, tc.period.String(), tc.text)
	}
	for _, text := range []string{"", "s", "0s", "-1m", "1x", "1.5h", "9223372036854775807d"} {
		assertEqual(t, PeriodByString(text), Period(0))
	}
}
