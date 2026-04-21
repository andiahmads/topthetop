package collector

import (
	"log-monitoring/internal/types"

	"github.com/shirou/gopsutil/v4/disk"
)

type DiskCollector struct {
	prevStats map[string]disk.IOCountersStat
}

func NewDiskCollector() *DiskCollector {
	return &DiskCollector{
		prevStats: make(map[string]disk.IOCountersStat),
	}
}

func (c *DiskCollector) Collect() (types.DiskIOStats, error) {
	stats, err := disk.IOCounters()
	if err != nil {
		return types.DiskIOStats{}, err
	}

	var readBytes, writeBytes, readCount, writeCount uint64
	for _, stat := range stats {
		prev, ok := c.prevStats[stat.Name]
		if ok {
			readBytes += stat.ReadBytes - prev.ReadBytes
			writeBytes += stat.WriteBytes - prev.WriteBytes
			readCount += stat.ReadCount - prev.ReadCount
			writeCount += stat.WriteCount - prev.WriteCount
		}
		c.prevStats[stat.Name] = stat
	}

	readBytes >>= 10
	writeBytes >>= 10

	return types.DiskIOStats{
		ReadBytes:  readBytes,
		WriteBytes: writeBytes,
		ReadCount:  readCount,
		WriteCount: writeCount,
	}, nil
}
