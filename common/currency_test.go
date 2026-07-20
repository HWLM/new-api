package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTo6(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"no fraction", 12, 12},
		{"round down", 1.0 / 3.0, 0.333333},
		{"round up half away", 0.0000005, 0.000001},
		{"negative half away", -0.0000005, -0.000001},
		{"already 6dp", 1.234567, 1.234567},
		{"trailing beyond 6dp", 2.98529411764, 2.985294},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, RoundTo6(tc.in), 1e-9)
		})
	}

	require.True(t, math.IsNaN(RoundTo6(math.NaN())))
	require.True(t, math.IsInf(RoundTo6(math.Inf(1)), 1))
}

func TestConvertAmountByRate(t *testing.T) {
	cases := []struct {
		name   string
		amount float64
		rate   float64
		want   float64
	}{
		{"usd rate 6.8", 6.8, 6.8, 1},
		{"quota by 6.8", 1000000, 6.8, 147058.823529},
		{"zero amount", 0, 6.8, 0},
		{"rate one identity", 42.5, 1, 42.5},
		// Non-positive rate is a no-op: never fabricate a different amount.
		{"zero rate no-op", 500, 0, 500},
		{"negative rate no-op", 500, -6.8, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, ConvertAmountByRate(tc.amount, tc.rate), 1e-9)
		})
	}
}

func TestConvertAmountToStorage(t *testing.T) {
	cases := []struct {
		name   string
		amount float64
		rate   float64
		want   float64
	}{
		{"usd rate 6.8", 1, 6.8, 7},                // math.Round(6.8) = 7
		{"round-trip whole", 147059, 6.8, 1000001}, // math.Round(1000001.2)
		{"zero amount", 0, 6.8, 0},
		{"rate one identity", 42, 1, 42},
		// Non-positive rate is a no-op: never fabricate a different amount.
		{"zero rate no-op", 500, 0, 500},
		{"negative rate no-op", 500, -6.8, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, ConvertAmountToStorage(tc.amount, tc.rate), 1e-9)
		})
	}

	// A NaN / Inf rate must not corrupt the stored amount.
	require.Equal(t, 500.0, ConvertAmountToStorage(500, math.NaN()))
	require.Equal(t, 500.0, ConvertAmountToStorage(500, math.Inf(1)))
}
