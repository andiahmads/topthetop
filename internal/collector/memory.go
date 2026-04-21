package collector

import (
	"github.com/shirou/gopsutil/v4/mem"
	"log-monitoring/internal/types"
)

type MemoryCollector struct{}

func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{}
}

func (c *MemoryCollector) Collect() (types.MemoryStats, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return types.MemoryStats{}, err
	}

	swap, _ := mem.SwapMemory()

	return types.MemoryStats{
		Total:     v.Total,
		Used:      v.Used,
		Free:      v.Free,
		Available: v.Available,
		Buffers:   v.Buffers,
		Cached:    v.Cached,
		SwapTotal: swap.Total,
		SwapUsed:  swap.Used,
		SwapFree:  swap.Free,
	}, nil
}
