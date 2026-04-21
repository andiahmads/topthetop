package collector

import (
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
	"log-monitoring/internal/types"
)

type CPUCollector struct {
	mu          sync.Mutex
	prevTotal   cpu.TimesStat
	prevPerCore []cpu.TimesStat
	initialized bool
}

func NewCPUCollector() *CPUCollector {
	return &CPUCollector{}
}

func (c *CPUCollector) Collect() (types.CPUStats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	totalTimes, err := cpu.Times(false)
	if err != nil {
		return types.CPUStats{}, err
	}
	perCoreTimes, err := cpu.Times(true)
	if err != nil {
		return types.CPUStats{}, err
	}
	if len(totalTimes) == 0 {
		return types.CPUStats{}, nil
	}

	if !c.initialized || len(c.prevPerCore) != len(perCoreTimes) {
		c.prevTotal = totalTimes[0]
		c.prevPerCore = append([]cpu.TimesStat(nil), perCoreTimes...)
		c.initialized = true
		return types.CPUStats{
			PerCore: make([]float64, len(perCoreTimes)),
		}, nil
	}

	total := usageBetween(c.prevTotal, totalTimes[0])
	perCore := make([]float64, len(perCoreTimes))
	for i := range perCoreTimes {
		perCore[i] = usageBetween(c.prevPerCore[i], perCoreTimes[i])
	}

	c.prevTotal = totalTimes[0]
	c.prevPerCore = append(c.prevPerCore[:0], perCoreTimes...)

	var sum float64
	for _, pct := range perCore {
		sum += pct
	}
	avgCore := 0.0
	if len(perCore) > 0 {
		avgCore = sum / float64(len(perCore))
	}

	// Find min/max per-core
	minCore, maxCore := 100.0, 0.0
	for _, pct := range perCore {
		if pct < minCore {
			minCore = pct
		}
		if pct > maxCore {
			maxCore = pct
		}
	}

	return types.CPUStats{
		PerCore: perCore,
		Total:   total,
		User:    total,   // Total CPU usage percentage
		System:  avgCore, // Average per-core
		Idle:    minCore, // Min core (most idle)
		Iowait:  maxCore, // Max core (most busy)
		Steal:   0,
	}, nil
}

func usageBetween(prev, curr cpu.TimesStat) float64 {
	prevTotal := totalCPUTime(prev)
	currTotal := totalCPUTime(curr)
	totalDelta := currTotal - prevTotal
	if totalDelta <= 0 {
		return 0
	}

	idleDelta := (curr.Idle + curr.Iowait) - (prev.Idle + prev.Iowait)
	usedDelta := totalDelta - idleDelta
	if usedDelta < 0 {
		usedDelta = 0
	}

	return (usedDelta / totalDelta) * 100
}

func totalCPUTime(t cpu.TimesStat) float64 {
	return t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal + t.Guest + t.GuestNice
}
