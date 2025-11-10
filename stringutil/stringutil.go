package stringutil

import "fmt"

const ShortenLogLength = 16

// ShortenLog shortens a hash string for logging purposes
func ShortenLog(hash string) string {
	indexCut := ShortenLogLength / 2
	if len(hash) <= ShortenLogLength {
		return hash
	}
	return fmt.Sprintf("%s...%s", hash[:indexCut], hash[len(hash)-indexCut:])
}
