package main

import (
	"fmt"
	"log-monitoring/internal/collector"
)

func main() {
	cpu := collector.NewCPUCollector()
	mem := collector.NewMemoryCollector()
	disk := collector.NewDiskCollector()
	net := collector.NewNetworkCollector()
	proc := collector.NewProcessCollector()

	fmt.Println("=== Testing Collectors ===")

	fmt.Println("\nCPU:")
	if _, err := cpu.Collect(); err != nil {
		fmt.Printf("  Warmup error: %v\n", err)
	}
	cpuStats, err := cpu.Collect()
	fmt.Printf("  Stats: %+v\n", cpuStats)
	fmt.Printf("  Error: %v\n", err)

	fmt.Println("\nMemory:")
	memStats, err := mem.Collect()
	fmt.Printf("  Stats: %+v\n", memStats)
	fmt.Printf("  Error: %v\n", err)

	fmt.Println("\nDisk:")
	diskStats, err := disk.Collect()
	fmt.Printf("  Stats: %+v\n", diskStats)
	fmt.Printf("  Error: %v\n", err)

	fmt.Println("\nNetwork:")
	netStats, err := net.Collect()
	fmt.Printf("  Stats: %+v\n", netStats)
	fmt.Printf("  Error: %v\n", err)

	fmt.Println("\nProcesses:")
	procs, err := proc.Collect()
	fmt.Printf("  Count: %d\n", len(procs))
	fmt.Printf("  Error: %v\n", err)
	if len(procs) > 0 {
		fmt.Printf("  First: PID=%d Name=%s CPU=%.1f MEM=%.1f\n",
			procs[0].PID, procs[0].Name, procs[0].CPUPercent, procs[0].MemPercent)
	}
}
