// File: pkg/blockchain/difficulty.go
package blockchain

import (
	"fmt"
	"time"
)

// AdjustDifficulty recalculates difficulty based on the time taken to mine the last 'adjustmentInterval' blocks.
func AdjustDifficulty(chain []*Block, targetTimePerBlock time.Duration, adjustmentInterval int) int {
	n := len(chain)
	if n < adjustmentInterval {
		return chain[n-1].Difficulty
	}
	start := chain[n-adjustmentInterval]
	end := chain[n-1]
	actualTime := time.Duration(end.Timestamp-start.Timestamp) * time.Second
	expectedTime := targetTimePerBlock * time.Duration(adjustmentInterval)
	currentDifficulty := chain[n-1].Difficulty

	if actualTime < expectedTime/2 {
		fmt.Printf("Increasing difficulty: actual %v < expected/2 %v\n", actualTime, expectedTime/2)
		return currentDifficulty + 1
	} else if actualTime > expectedTime*2 {
		if currentDifficulty > 1 {
			fmt.Printf("Decreasing difficulty: actual %v > expected*2 %v\n", actualTime, expectedTime*2)
			return currentDifficulty - 1
		}
	}
	return currentDifficulty
}
