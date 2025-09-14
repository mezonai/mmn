package performance

import (
	"fmt"
	"time"
)

// OptimizationCalculator calculates expected TPS improvements
type OptimizationCalculator struct {
	// Current configuration
	CurrentConfig *CurrentConfig

	// Optimized configuration
	OptimizedConfig *OptimizedConfig
}

// CurrentConfig represents current system configuration
type CurrentConfig struct {
	HashesPerTick       int
	TicksPerSlot        int
	TickIntervalMs      int
	BatchSize           int
	LeaderTimeout       int
	LeaderTimeoutLoopMs int
}

// OptimizedConfig represents optimized system configuration
type OptimizedConfig struct {
	HashesPerTick       int
	TicksPerSlot        int
	TickIntervalMs      int
	BatchSize           int
	LeaderTimeout       int
	LeaderTimeoutLoopMs int
	WorkerCount         int
	BufferSize          int
}

// TPSAnalysis represents TPS analysis results
type TPSAnalysis struct {
	CurrentTPS      float64
	OptimizedTPS    float64
	Improvement     float64
	Bottlenecks     []string
	Recommendations []string
}

// NewOptimizationCalculator creates a new optimization calculator
func NewOptimizationCalculator() *OptimizationCalculator {
	return &OptimizationCalculator{
		CurrentConfig: &CurrentConfig{
			HashesPerTick:       10,
			TicksPerSlot:        4,
			TickIntervalMs:      100,
			BatchSize:           750,
			LeaderTimeout:       50,
			LeaderTimeoutLoopMs: 5,
		},
		OptimizedConfig: &OptimizedConfig{
			HashesPerTick:       20,
			TicksPerSlot:        2,
			TickIntervalMs:      50,
			BatchSize:           2000,
			LeaderTimeout:       25,
			LeaderTimeoutLoopMs: 2,
			WorkerCount:         8,
			BufferSize:          50000,
		},
	}
}

// CalculateTPSImprovement calculates expected TPS improvement
func (oc *OptimizationCalculator) CalculateTPSImprovement() *TPSAnalysis {
	// Calculate current TPS
	currentSlotTime := time.Duration(oc.CurrentConfig.TicksPerSlot*oc.CurrentConfig.TickIntervalMs) * time.Millisecond
	currentTPS := float64(oc.CurrentConfig.BatchSize) / currentSlotTime.Seconds()

	// Calculate optimized TPS
	optimizedSlotTime := time.Duration(oc.OptimizedConfig.TicksPerSlot*oc.OptimizedConfig.TickIntervalMs) * time.Millisecond
	optimizedTPS := float64(oc.OptimizedConfig.BatchSize) / optimizedSlotTime.Seconds()

	// Calculate improvement
	improvement := optimizedTPS / currentTPS

	// Identify bottlenecks
	bottlenecks := oc.identifyBottlenecks()

	// Generate recommendations
	recommendations := oc.generateRecommendations()

	return &TPSAnalysis{
		CurrentTPS:      currentTPS,
		OptimizedTPS:    optimizedTPS,
		Improvement:     improvement,
		Bottlenecks:     bottlenecks,
		Recommendations: recommendations,
	}
}

// identifyBottlenecks identifies current bottlenecks
func (oc *OptimizationCalculator) identifyBottlenecks() []string {
	bottlenecks := []string{}

	// Check slot time
	slotTime := oc.CurrentConfig.TicksPerSlot * oc.CurrentConfig.TickIntervalMs
	if slotTime > 200 {
		bottlenecks = append(bottlenecks, "Slow slot time (too many ticks or long tick interval)")
	}

	// Check batch size
	if oc.CurrentConfig.BatchSize < 1000 {
		bottlenecks = append(bottlenecks, "Small batch size limiting throughput")
	}

	// Check leader timeout
	if oc.CurrentConfig.LeaderTimeout > 30 {
		bottlenecks = append(bottlenecks, "Long leader timeout causing delays")
	}

	// Check PoH configuration
	if oc.CurrentConfig.HashesPerTick < 15 {
		bottlenecks = append(bottlenecks, "Low PoH hashes per tick")
	}

	return bottlenecks
}

// generateRecommendations generates optimization recommendations
func (oc *OptimizationCalculator) generateRecommendations() []string {
	recommendations := []string{}

	// PoH optimizations
	recommendations = append(recommendations, "Increase PoH hashes per tick from 10 to 20")
	recommendations = append(recommendations, "Reduce ticks per slot from 4 to 2")
	recommendations = append(recommendations, "Reduce tick interval from 100ms to 50ms")

	// Batch optimizations
	recommendations = append(recommendations, "Increase batch size from 750 to 2000")
	recommendations = append(recommendations, "Reduce leader timeout from 50ms to 25ms")

	// Block assembly optimizations
	recommendations = append(recommendations, "Add object pooling for block assembly")
	recommendations = append(recommendations, "Implement parallel block assembly")

	// Memory optimizations
	recommendations = append(recommendations, "Implement entry buffering with 1,000 capacity")

	return recommendations
}

// PrintAnalysis prints the TPS analysis
func (oc *OptimizationCalculator) PrintAnalysis() {
	analysis := oc.CalculateTPSImprovement()

	fmt.Println("=== TPS OPTIMIZATION ANALYSIS ===")
	fmt.Printf("Current TPS: %.2f\n", analysis.CurrentTPS)
	fmt.Printf("Optimized TPS: %.2f\n", analysis.OptimizedTPS)
	fmt.Printf("Improvement: %.2fx\n", analysis.Improvement)
	fmt.Println()

	fmt.Println("=== IDENTIFIED BOTTLENECKS ===")
	for i, bottleneck := range analysis.Bottlenecks {
		fmt.Printf("%d. %s\n", i+1, bottleneck)
	}
	fmt.Println()

	fmt.Println("=== OPTIMIZATION RECOMMENDATIONS ===")
	for i, rec := range analysis.Recommendations {
		fmt.Printf("%d. %s\n", i+1, rec)
	}
	fmt.Println()

	// Calculate expected results
	fmt.Println("=== EXPECTED RESULTS ===")
	fmt.Printf("Slot time: %dms → %dms (%.2fx faster)\n",
		oc.CurrentConfig.TicksPerSlot*oc.CurrentConfig.TickIntervalMs,
		oc.OptimizedConfig.TicksPerSlot*oc.OptimizedConfig.TickIntervalMs,
		float64(oc.CurrentConfig.TicksPerSlot*oc.CurrentConfig.TickIntervalMs)/float64(oc.OptimizedConfig.TicksPerSlot*oc.OptimizedConfig.TickIntervalMs))

	fmt.Printf("Batch size: %d → %d (%.2fx larger)\n",
		oc.CurrentConfig.BatchSize,
		oc.OptimizedConfig.BatchSize,
		float64(oc.OptimizedConfig.BatchSize)/float64(oc.CurrentConfig.BatchSize))

	fmt.Printf("Overall TPS improvement: %.2fx\n", analysis.Improvement)
}

// CalculateExpectedTPS calculates expected TPS after optimizations
func (oc *OptimizationCalculator) CalculateExpectedTPS() float64 {
	analysis := oc.CalculateTPSImprovement()
	return analysis.OptimizedTPS
}
