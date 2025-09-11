package utils

import "time"

// SecondsBetween returns num of seconds between two timestamps
func SecondsBetween(from time.Time, to time.Time) float64 {
	return to.Sub(from).Seconds()
}
