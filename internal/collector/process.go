package collector

import (
	"log-monitoring/internal/types"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

type ProcessCollector struct {
	sortBy   string
	sortDesc bool
}

func NewProcessCollector() *ProcessCollector {
	return &ProcessCollector{
		sortBy:   "cpu",
		sortDesc: true,
	}
}

func (c *ProcessCollector) Collect() ([]types.ProcessInfo, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	// Prime CPU percent for all processes first (non-blocking, uses cached value).
	// Then on second pass, each Percent(0) call returns the delta from the prime call.
	for _, p := range procs {
		_, _ = p.Percent(0)
	}

	var infos []types.ProcessInfo
	for _, p := range procs {
		name, _ := p.Name()
		cpuPercent, _ := p.Percent(0)
		memPercent, _ := p.MemoryPercent()
		memInfo, _ := p.MemoryInfo()
		statusSlice, _ := p.Status()
		createTime, _ := p.CreateTime()
		ppid, _ := p.Ppid()
		username, _ := p.Username()
		numThreads, _ := p.NumThreads()

		var memBytes uint64
		if memInfo != nil {
			memBytes = memInfo.RSS
		}

		var status string
		if len(statusSlice) > 0 {
			status = strings.Join(statusSlice, ",")
		}

		infos = append(infos, types.ProcessInfo{
			PID:        p.Pid,
			Name:       name,
			CPUPercent: cpuPercent,
			MemPercent: memPercent,
			MemBytes:   memBytes,
			Status:     status,
			CreateTime: createTime,
			PPID:       ppid,
			Username:   username,
			NumThreads: numThreads,
		})
	}

	c.sortProcesses(infos)
	return infos, nil
}

func (c *ProcessCollector) sortProcesses(infos []types.ProcessInfo) {
	sort.Slice(infos, func(i, j int) bool {
		var less bool
		switch c.sortBy {
		case "cpu":
			less = infos[i].CPUPercent < infos[j].CPUPercent
		case "mem":
			less = float64(infos[i].MemPercent) < float64(infos[j].MemPercent)
		case "pid":
			less = infos[i].PID < infos[j].PID
		case "name":
			less = infos[i].Name < infos[j].Name
		default:
			less = infos[i].CPUPercent < infos[j].CPUPercent
		}
		if c.sortDesc {
			return !less
		}
		return less
	})
}

func (c *ProcessCollector) Kill(pid int32) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
