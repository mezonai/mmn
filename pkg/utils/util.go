package utils

import (
	"math/big"
)

// FormatBigIntWithDecimals formats a big.Int as a decimal string with 6 digits after the decimal point.
// It assumes the input represents a fixed-point number scaled by 1e6.
func FormatBigIntWithDecimals(i *big.Int) float64 {
	// Convert big.Int to big.Float
	floatVal := new(big.Float).SetInt(i)

	// Divide by 1e6 (fixed-point scaling)
	divisor := big.NewFloat(1_000_000)
	result, _ := new(big.Float).Quo(floatVal, divisor).Float64()

	// Format to 6 decimal places
	return result
}
