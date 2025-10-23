package utils

import "fmt"

func ShortenLog(hash string) string {
	index_cut := 8
	if len(hash) <= 8 {
		return hash
	} else if len(hash) <= 16 {
		index_cut = 4
	}
	return fmt.Sprintf("%s...%s", hash[:index_cut], hash[len(hash)-index_cut:])
}
