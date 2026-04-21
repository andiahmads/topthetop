package app

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"log-monitoring/internal/collector"
	"log-monitoring/internal/config"
	"log-monitoring/internal/types"
)

type activePanel int

const (
	panelProcesses activePanel = iota
	panelCPU
	panelMemory
	panelDisk
	panelNetwork
)

type sortField string

const (
	sortCPU  sortField = "cpu"
	sortMem  sortField = "mem"
	sortPID  sortField = "pid"
	sortName sortField = "name"
)

// ServerState holds all metrics and state for a single remote server.
type ServerState struct {
	Name string

	// Stats
	cpuStats  types.CPUStats
	memStats  types.MemoryStats
	diskStats types.DiskIOStats
	netStats  types.NetworkIOStats
	processes []types.ProcessInfo

	// SSH collector (nil when local)
	sshCollector *collector.SSHCollector

	// History
	cpuHistory   []float64
	memHistory   []float64
	swapHistory  []float64
	diskRHistory []float64
	diskWHistory []float64
	netRHistory  []float64
	netWHistory  []float64

	// Status
	connected  bool
	errMsg     string
	lastErrAt  time.Time
	procsDirty bool

	mu sync.Mutex
}

func newServerState(name string, historySize int) *ServerState {
	s := &ServerState{
		Name:      name,
		connected: true,
	}
	for i := 0; i < historySize; i++ {
		s.cpuHistory = append(s.cpuHistory, 0)
		s.memHistory = append(s.memHistory, 0)
		s.swapHistory = append(s.swapHistory, 0)
		s.diskRHistory = append(s.diskRHistory, 0)
		s.diskWHistory = append(s.diskWHistory, 0)
		s.netRHistory = append(s.netRHistory, 0)
		s.netWHistory = append(s.netWHistory, 0)
	}
	return s
}

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc

	// Mode: "local" or "remote"
	mode string

	// Remote mode
	servers       []*ServerState
	selectedServer int
	serverCfg     []config.ServerConfig

	// --- Local mode fields (used when mode == "local") ---
	cpuStats  types.CPUStats
	memStats  types.MemoryStats
	diskStats types.DiskIOStats
	netStats  types.NetworkIOStats
	processes []types.ProcessInfo

	cpuHistory   []float64
	memHistory   []float64
	swapHistory  []float64
	diskRHistory []float64
	diskWHistory []float64
	netRHistory  []float64
	netWHistory  []float64

	// Local collectors (only used in local mode)
	cpuCollector  *collector.CPUCollector
	memCollector  *collector.MemoryCollector
	diskCollector *collector.DiskCollector
	netCollector  *collector.NetworkCollector
	procCollector *collector.ProcessCollector

	// Config
	graphWidth     int
	historySize    int
	updateInterval time.Duration

	// UI State
	selectedPanel   activePanel
	selectedProcess int
	sortField       sortField
	sortDesc        bool
	paused          bool
	collecting      bool
	tickCount       int
	width           int
	height          int
	startedAt       time.Time

	// Rendering cache
	uiCacheDirty bool
	uiCache      string

	// Process detail modal
	processDetailMode bool
	lockedPID         int32
	lockedProc        types.ProcessInfo

	// Kill confirmation dialog
	showKillConfirm bool

	// Search mode
	searchMode bool
	searchText string

	// Kill feedback
	killMessage   string
	killMessageAt time.Time

	// procsDirty flag for local mode
	procsDirty bool

	mu sync.Mutex
}

func New() *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		mode:             "local",
		cpuCollector:     collector.NewCPUCollector(),
		memCollector:     collector.NewMemoryCollector(),
		diskCollector:    collector.NewDiskCollector(),
		netCollector:     collector.NewNetworkCollector(),
		procCollector:    collector.NewProcessCollector(),
		graphWidth:       45,
		historySize:      60,
		updateInterval:   1 * time.Second,
		selectedPanel:    panelProcesses,
		selectedProcess: 0,
		sortField:        sortCPU,
		sortDesc:         true,
		startedAt:        time.Now(),
	}

	for i := 0; i < m.historySize; i++ {
		m.cpuHistory = append(m.cpuHistory, 0)
		m.memHistory = append(m.memHistory, 0)
		m.swapHistory = append(m.swapHistory, 0)
		m.diskRHistory = append(m.diskRHistory, 0)
		m.diskWHistory = append(m.diskWHistory, 0)
		m.netRHistory = append(m.netRHistory, 0)
		m.netWHistory = append(m.netWHistory, 0)
	}

	m.ctx = ctx
	m.cancel = cancel
	return m
}

func NewRemote(cfg []config.ServerConfig, timeout time.Duration) (*Model, error) {
	ctx, cancel := context.WithCancel(context.Background())

	servers := make([]*ServerState, len(cfg))
	for i, sc := range cfg {
		servers[i] = newServerState(sc.Name, 60)
		servers[i].sshCollector = collector.NewSSHCollector(sc)
		servers[i].connected = true
	}

	m := &Model{
		ctx:             ctx,
		cancel:          cancel,
		mode:            "remote",
		servers:         servers,
		selectedServer:  0,
		serverCfg:       cfg,
		graphWidth:      45,
		historySize:     60,
		updateInterval:  2 * time.Second,
		selectedPanel:   panelCPU,
		selectedProcess: 0,
		sortField:       sortCPU,
		sortDesc:        true,
		startedAt:       time.Now(),
	}

	return m, nil
}

func (m *Model) Context() context.Context {
	return m.ctx
}

func (m *Model) Cancel() {
	m.cancel()
}

func (m *Model) IsRemote() bool {
	return m.mode == "remote"
}

func (m *Model) NumServers() int {
	return len(m.servers)
}

func (m *Model) CurrentServer() *ServerState {
	if !m.IsRemote() || m.selectedServer >= len(m.servers) {
		return nil
	}
	return m.servers[m.selectedServer]
}

type statsSnapshot struct {
	cpu       types.CPUStats
	mem       types.MemoryStats
	disk      types.DiskIOStats
	net       types.NetworkIOStats
	processes []types.ProcessInfo
	hasProc   bool
	err       error
}

func (m *Model) CollectSnapshotLocal(includeProcesses bool) statsSnapshot {
	snapshot := statsSnapshot{}

	cpuStats, cpuErr := m.cpuCollector.Collect()
	if cpuErr == nil {
		snapshot.cpu = cpuStats
	} else {
		snapshot.err = cpuErr
	}

	memStats, memErr := m.memCollector.Collect()
	if memErr == nil {
		snapshot.mem = memStats
	} else if snapshot.err == nil {
		snapshot.err = memErr
	}

	diskStats, diskErr := m.diskCollector.Collect()
	if diskErr == nil {
		snapshot.disk = diskStats
	} else if snapshot.err == nil {
		snapshot.err = diskErr
	}

	netStats, netErr := m.netCollector.Collect()
	if netErr == nil {
		snapshot.net = netStats
	} else if snapshot.err == nil {
		snapshot.err = netErr
	}

	if includeProcesses {
		procs, procErr := m.procCollector.Collect()
		if procErr == nil {
			snapshot.processes = procs
			snapshot.hasProc = true
		} else if snapshot.err == nil {
			snapshot.err = procErr
		}
	}

	return snapshot
}

func (m *Model) CollectSnapshot(includeProcesses bool) statsSnapshot {
	return m.CollectSnapshotLocal(includeProcesses)
}

func (m *Model) ApplySnapshot(snapshot statsSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cpuStats = snapshot.cpu
	m.cpuHistory = append(m.cpuHistory[1:], snapshot.cpu.Total)

	m.memStats = snapshot.mem
	usedPct := 0.0
	if snapshot.mem.Total > 0 {
		usedPct = float64(snapshot.mem.Used) / float64(snapshot.mem.Total) * 100
	}
	m.memHistory = append(m.memHistory[1:], usedPct)

	swapPct := 0.0
	if snapshot.mem.SwapTotal > 0 {
		swapPct = float64(snapshot.mem.SwapUsed) / float64(snapshot.mem.SwapTotal) * 100
	}
	m.swapHistory = append(m.swapHistory[1:], swapPct)

	m.diskStats = snapshot.disk
	m.diskRHistory = append(m.diskRHistory[1:], float64(snapshot.disk.ReadBytes))
	m.diskWHistory = append(m.diskWHistory[1:], float64(snapshot.disk.WriteBytes))

	m.netStats = snapshot.net
	m.netRHistory = append(m.netRHistory[1:], float64(snapshot.net.BytesRecv))
	m.netWHistory = append(m.netWHistory[1:], float64(snapshot.net.BytesSent))

	if snapshot.hasProc {
		m.processes = snapshot.processes
		m.procsDirty = true
	}

	if m.procsDirty {
		m.sortProcesses()
		m.procsDirty = false
	}

	if m.killMessage != "" && time.Since(m.killMessageAt) > 3*time.Second {
		m.killMessage = ""
	}

	m.uiCacheDirty = true
}

func (m *Model) ApplyRemoteSnapshot(snap collector.RemoteSnapshot, serverIdx int) {
	if serverIdx < 0 || serverIdx >= len(m.servers) {
		return
	}
	s := m.servers[serverIdx]
	s.mu.Lock()
	defer s.mu.Unlock()

	if snap.Err != nil {
		s.errMsg = snap.Err.Error()
		s.lastErrAt = time.Now()
		s.connected = false
		return
	}

	s.connected = true
	s.errMsg = ""
	s.cpuStats = snap.CPU
	s.cpuHistory = append(s.cpuHistory[1:], snap.CPU.Total)

	s.memStats = snap.Mem
	memPct := 0.0
	if snap.Mem.Total > 0 {
		memPct = float64(snap.Mem.Used) / float64(snap.Mem.Total) * 100
	}
	s.memHistory = append(s.memHistory[1:], memPct)

	swapPct := 0.0
	if snap.Mem.SwapTotal > 0 {
		swapPct = float64(snap.Mem.SwapUsed) / float64(snap.Mem.SwapTotal) * 100
	}
	s.swapHistory = append(s.swapHistory[1:], swapPct)

	s.diskStats = snap.Disk
	s.diskRHistory = append(s.diskRHistory[1:], float64(snap.Disk.ReadBytes))
	s.diskWHistory = append(s.diskWHistory[1:], float64(snap.Disk.WriteBytes))

	s.netStats = snap.Net
	s.netRHistory = append(s.netRHistory[1:], float64(snap.Net.BytesRecv))
	s.netWHistory = append(s.netWHistory[1:], float64(snap.Net.BytesSent))

	if len(snap.Procs) > 0 {
		s.processes = snap.Procs
		s.procsDirty = true
	}

	if s.procsDirty {
		sortSliceProcesses(s.processes, m.sortField, m.sortDesc)
		s.procsDirty = false
	}
}

func (m *Model) shouldRefreshProcesses() bool {
	if len(m.processes) == 0 {
		return true
	}
	return m.tickCount%3 == 0
}

func (m *Model) GetFilteredProcesses() []types.ProcessInfo {
	if m.searchText == "" {
		return m.processes
	}
	var filtered []types.ProcessInfo
	for _, p := range m.processes {
		if containsIgnoreCase(p.Name, m.searchText) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (m *Model) SetSort(field sortField) {
	m.sortField = field
	m.sortDesc = true
	m.sortProcesses()
	m.uiCacheDirty = true
}

func (m *Model) GetWidth() int {
	return m.width
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func (m *Model) KillSelectedProcess() (bool, string) {
	procs := m.GetFilteredProcesses()
	if m.selectedProcess < 0 || m.selectedProcess >= len(procs) {
		return false, "no process selected"
	}
	pid := procs[m.selectedProcess].PID
	name := procs[m.selectedProcess].Name
	err := m.procCollector.Kill(pid)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "process not found") ||
			strings.Contains(errMsg, "no such process") ||
			strings.Contains(errMsg, "not found") ||
			strings.Contains(errMsg, "esrch") {
			return true, "process already gone: " + name
		}
		return false, "kill failed: " + err.Error()
	}
	return true, "killed: " + name
}

func (m *Model) sortProcesses() {
	sortSliceProcesses(m.processes, m.sortField, m.sortDesc)
}

func sortSliceProcesses(procs []types.ProcessInfo, field sortField, desc bool) {
	sort.Slice(procs, func(i, j int) bool {
		var less bool
		switch field {
		case sortMem:
			less = procs[i].MemPercent < procs[j].MemPercent
		case sortPID:
			less = procs[i].PID < procs[j].PID
		case sortName:
			less = procs[i].Name < procs[j].Name
		case sortCPU:
			fallthrough
		default:
			less = procs[i].CPUPercent < procs[j].CPUPercent
		}
		if desc {
			return !less
		}
		return less
	})
}
