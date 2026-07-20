package common

import "math"

// RoundTo6 rounds a float to 6 decimal places using half-away-from-zero,
// matching the %.6f convention used across the currency display paths
// (logger.FormatQuota, controller/billing.go).
func RoundTo6(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*1e6) / 1e6
}

// ConvertAmountByRate divides a display amount by a positive exchange rate and
// rounds the result to 6 decimals. It is used to present amounts under a
// per-user settlement currency (e.g. USD) without changing the stored quota.
// A non-positive rate is treated as "no conversion" and returns the input
// unchanged, so a misconfigured rate can never fabricate a different charge.
func ConvertAmountByRate(amount, rate float64) float64 {
	if rate <= 0 {
		return amount
	}
	return RoundTo6(amount / rate)
}

// ConvertAmountToStorage is the inverse of ConvertAmountByRate: it scales an amount
// entered by a per-user settlement currency (e.g. USD, already divided by the
// display rate) back to the underlying stored quota by multiplying by rate and
// rounding to the nearest integer, so it persists in the same unit as CNY users.
// A non-positive, NaN, or Inf rate is treated as "no conversion" and returns the
// input unchanged, so a misconfigured rate can never fabricate a different amount.
func ConvertAmountToStorage(amount, rate float64) float64 {
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return amount
	}
	return math.Round(amount * rate)
}
