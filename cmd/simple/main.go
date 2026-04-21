package main

import (
	"fmt"

	"log-monitoring/internal/collector"
)

func main() {
	cpuCol := collector.NewCPUCollector()
	if _, err := cpuCol.Collect(); err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	cpuStats, err := cpuCol.Collect()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("=== CPU Monitor ===")
	fmt.Printf("Total CPU: %.1f%%\n", cpuStats.Total)
	fmt.Printf("User: %.1f%%\n", cpuStats.User)
	fmt.Printf("System: %.1f%%\n", cpuStats.System)
	fmt.Printf("Idle: %.1f%%\n", cpuStats.Idle)

	if len(cpuStats.PerCore) > 0 {
		fmt.Println("\nPer Core:")
		for i, pct := range cpuStats.PerCore {
			fmt.Printf("  Core %d: %.1f%%\n", i, pct)
		}
	}
}
