package stringutil

import "fmt"

const SHORTEN_LOG_LENGTH = 16

// ShortenLog shortens a hash string for logging purposes
func ShortenLog(hash string) string {
	index_cut := SHORTEN_LOG_LENGTH / 2
	if len(hash) <= SHORTEN_LOG_LENGTH {
		return hash
	}
	return fmt.Sprintf("%s...%s", hash[:index_cut], hash[len(hash)-index_cut:])
}
