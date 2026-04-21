package types

type CPUStats struct {
	PerCore []float64
	Total   float64
	User    float64
	System  float64
	Idle    float64
	Iowait  float64
	Steal   float64
}

type MemoryStats struct {
	Total     uint64
	Used      uint64
	Free      uint64
	Available uint64
	Buffers   uint64
	Cached    uint64
	SwapTotal uint64
	SwapUsed  uint64
	SwapFree  uint64
}

type DiskIOStats struct {
	ReadBytes  uint64
	WriteBytes uint64
	ReadCount  uint64
	WriteCount uint64
}

type NetworkIOStats struct {
	BytesSent   uint64
	BytesRecv   uint64
	PacketsSent uint64
	PacketsRecv uint64
}

type ProcessInfo struct {
	PID        int32
	Name       string
	CPUPercent float64
	MemPercent float32
	MemBytes   uint64
	Status     string
	CreateTime int64
	PPID       int32
	Username   string
	NumThreads int32
}
