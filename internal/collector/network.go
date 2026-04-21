package collector

import (
	"log-monitoring/internal/types"

	"github.com/shirou/gopsutil/v4/net"
)

type NetworkCollector struct {
	prevStats map[string]net.IOCountersStat
}

func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{
		prevStats: make(map[string]net.IOCountersStat),
	}
}

func (c *NetworkCollector) Collect() (types.NetworkIOStats, error) {
	stats, err := net.IOCounters(false)
	if err != nil {
		return types.NetworkIOStats{}, err
	}

	if len(stats) == 0 {
		return types.NetworkIOStats{}, nil
	}

	prev, ok := c.prevStats[""]
	stat := stats[0]

	var sent, recv, packetsSent, packetsRecv uint64
	if ok {
		sent = stat.BytesSent - prev.BytesSent
		recv = stat.BytesRecv - prev.BytesRecv
		packetsSent = stat.PacketsSent - prev.PacketsSent
		packetsRecv = stat.PacketsRecv - prev.PacketsRecv
	}

	c.prevStats[""] = stat

	return types.NetworkIOStats{
		BytesSent:   sent,
		BytesRecv:   recv,
		PacketsSent: packetsSent,
		PacketsRecv: packetsRecv,
	}, nil
}
