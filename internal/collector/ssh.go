package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"log-monitoring/internal/config"
	"log-monitoring/internal/types"
	"log/slog"
)

var ErrServerOffline = errors.New("server offline")

var (
	procStatRx   = regexp.MustCompile(`cpu\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)`)
	procStatCore = regexp.MustCompile(`cpu(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)`)
)

// SSHCollector fetches metrics from a remote server using the system `ssh` binary.
type SSHCollector struct {
	cfg  config.ServerConfig
	name string

	mu          sync.Mutex
	prevTotal   cpuTimes
	prevPerCore []cpuTimes
	prevDisk    map[string]diskStat
	prevNet     netStat
	prevProc    map[int32]procStat
	initialized bool
}

type cpuTimes struct {
	User, Nice, System, Idle, Iowait, Irq, Softirq, Steal, Guest, GuestNice int64
}

func (t cpuTimes) total() int64 {
	return t.User + t.Nice + t.System + t.Idle + t.Iowait + t.Irq + t.Softirq + t.Steal + t.Guest + t.GuestNice
}

type diskStat struct {
	ReadBytes, WriteBytes, ReadCount, WriteCount uint64
}

type netStat struct {
	BytesRecv, BytesSent, PacketsRecv, PacketsSent uint64
}

type procStat struct {
	CpuPercent, MemPercent float64
	MemBytes               uint64
}

func NewSSHCollector(cfg config.ServerConfig) *SSHCollector {
	return &SSHCollector{
		cfg:  cfg,
		name: cfg.Name,
		prevDisk: make(map[string]diskStat),
		prevNet:  netStat{},
		prevProc: make(map[int32]procStat),
	}
}

type RemoteSnapshot struct {
	CPU   types.CPUStats
	Mem   types.MemoryStats
	Disk  types.DiskIOStats
	Net   types.NetworkIOStats
	Procs []types.ProcessInfo
	Err   error
}

const remoteCollectCmd = `cat /proc/stat 2>/dev/null
echo "---MEM---"
cat /proc/meminfo 2>/dev/null
echo "---DISK---"
cat /proc/diskstats 2>/dev/null
echo "---NET---"
cat /proc/net/dev 2>/dev/null
echo "---PROC---"
ps aux --sort=-pcpu --no-headers 2>/dev/null | head -30
echo "---END---"
`

func (c *SSHCollector) Collect() RemoteSnapshot {
	snap := RemoteSnapshot{}

	out, err := c.runSSH(remoteCollectCmd)
	if err != nil {
		slog.Warn("ssh: collect failed", "collector", c.name, "err", err)
		snap.Err = fmt.Errorf("[%s] %w", c.name, err)
		return snap
	}
	slog.Debug("ssh: collect success", "collector", c.name, "output_len", len(out))

	sections := strings.SplitN(out, "---MEM---", 2)
	cpuPart := ""
	if len(sections) >= 1 {
		cpuPart = sections[0]
	}

	if len(sections) >= 2 {
		memRest := strings.SplitN(sections[1], "---DISK---", 2)
		snap.Mem = parseMeminfo(memRest[0])

		if len(memRest) >= 2 {
			diskNet := strings.SplitN(memRest[1], "---NET---", 2)
			snap.Disk = parseDiskstats(diskNet[0], c)

			if len(diskNet) >= 2 {
				netProc := strings.SplitN(diskNet[1], "---PROC---", 2)
				snap.Net = parseNetdev(netProc[0], c)

				if len(netProc) >= 2 {
					procEnd := strings.SplitN(netProc[1], "---END---", 2)
					snap.Procs = parsePs(procEnd[0], c)
				}
			}
		}
	}

	snap.CPU = parseCPU(cpuPart, c)
	return snap
}

// runSSH executes a command on the remote server using the system `ssh` binary.
func (c *SSHCollector) runSSH(cmd string) (string, error) {
	cfg := c.cfg
	port := cfg.Port
	if port == 0 {
		port = 22
	}

	userHost := fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
	slog.Info("ssh: connecting", "target", userHost, "port", port, "collector", c.name)

	sshCmd := []string{
		"ssh",
		"-p", fmt.Sprintf("%d", port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		"-o", "BatchMode=yes",
		"-o", "LogLevel=INFO",
	}

	if cfg.AuthType == "key" && cfg.KeyPath != "" {
		keyPath := expandPath(cfg.KeyPath)
		sshCmd = append(sshCmd, "-i", keyPath)
	}

	sshCmd = append(sshCmd, fmt.Sprintf("%s@%s", cfg.User, cfg.Host), cmd)

	execCmd := exec.Command(sshCmd[0], sshCmd[1:]...)
	env := os.Environ()
	execCmd.Env = append(env, "LANG=en_US.UTF-8")

	start := time.Now()
	out, err := execCmd.CombinedOutput()
	_ = start

	if err != nil {
		slog.Error("ssh: failed", "target", userHost, "collector", c.name, "err", err, "stderr", string(out))
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "no route to host") ||
			strings.Contains(errStr, "name or service not known") {
			return "", fmt.Errorf("connection failed: %s", errStr)
		}
		errMsg := strings.TrimSpace(string(out))
		if errMsg != "" {
			return "", fmt.Errorf("ssh error: %s", errMsg)
		}
		return "", fmt.Errorf("ssh: %s", err.Error())
	}

	if strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("empty output (auth failed?)")
	}

	return string(out), nil
}

func expandPath(path string) string {
	if path == "" {
		return ""
	}
	path = os.ExpandEnv(path)
	if len(path) >= 2 && path[0] == '~' {
		usr, err := user.Current()
		if err == nil {
			path = filepath.Join(usr.HomeDir, path[1:])
		}
	}
	return path
}

// --- Parsing functions ---

func parseCPU(raw string, c *SSHCollector) types.CPUStats {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var totalLine string
	var coreLines []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "cpu ") && !strings.HasPrefix(line, "cpu0") {
			totalLine = line
		} else if strings.HasPrefix(line, "cpu") {
			coreLines = append(coreLines, line)
		}
	}

	stats := types.CPUStats{}

	if totalLine == "" {
		return stats
	}

	m := procStatRx.FindStringSubmatch(totalLine)
	if m == nil {
		return stats
	}

	user, _ := strconv.ParseInt(m[1], 10, 64)
	nice, _ := strconv.ParseInt(m[2], 10, 64)
	system, _ := strconv.ParseInt(m[3], 10, 64)
	idle, _ := strconv.ParseInt(m[4], 10, 64)
	iowait, _ := strconv.ParseInt(m[5], 10, 64)
	irq, _ := strconv.ParseInt(m[6], 10, 64)
	softirq, _ := strconv.ParseInt(m[7], 10, 64)
	steal, _ := strconv.ParseInt(m[8], 10, 64)

	curr := cpuTimes{
		User: user, Nice: nice, System: system, Idle: idle,
		Iowait: iowait, Irq: irq, Softirq: softirq, Steal: steal,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.initialized {
		c.prevTotal = curr
		c.prevPerCore = make([]cpuTimes, len(coreLines))
		c.initialized = true
		stats.PerCore = make([]float64, len(coreLines))
		return stats
	}

	totalDelta := curr.total() - c.prevTotal.total()
	idleDelta := curr.Idle + curr.Iowait - c.prevTotal.Idle - c.prevTotal.Iowait

	usage := 0.0
	if totalDelta > 0 {
		usage = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	}
	if usage < 0 {
		usage = 0
	}

	stats.Total = usage
	stats.User = usage

	c.prevTotal = curr

	stats.PerCore = make([]float64, len(coreLines))
	sum := 0.0
	for i, line := range coreLines {
		if i >= len(c.prevPerCore) {
			break
		}
		sm := procStatCore.FindStringSubmatch(line)
		if sm == nil {
			continue
		}
		u, _ := strconv.ParseInt(sm[1], 10, 64)
		n, _ := strconv.ParseInt(sm[2], 10, 64)
		sy, _ := strconv.ParseInt(sm[3], 10, 64)
		id, _ := strconv.ParseInt(sm[4], 10, 64)
		io, _ := strconv.ParseInt(sm[5], 10, 64)

		coreCurr := cpuTimes{User: u, Nice: n, System: sy, Idle: id, Iowait: io}
		prev := c.prevPerCore[i]

		td := coreCurr.total() - prev.total()
		idD := coreCurr.Idle + coreCurr.Iowait - prev.Idle - prev.Iowait
		coreUsage := 0.0
		if td > 0 {
			coreUsage = float64(td-idD) / float64(td) * 100
		}
		if coreUsage < 0 {
			coreUsage = 0
		}
		stats.PerCore[i] = coreUsage
		sum += coreUsage
		c.prevPerCore[i] = coreCurr
	}

	if len(coreLines) > 0 {
		stats.System = sum / float64(len(coreLines))
		stats.Iowait = sum / float64(len(coreLines))
	}

	return stats
}

func parseMeminfo(raw string) types.MemoryStats {
	s := types.MemoryStats{}
	scan := bufio.NewScanner(strings.NewReader(raw))
	for scan.Scan() {
		parts := strings.Fields(scan.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		val, _ := strconv.ParseUint(parts[1], 10, 64)
		val *= 1024 // kernel reports in KB

		switch key {
		case "MemTotal":
			s.Total = val
		case "MemFree":
			s.Free = val
		case "MemAvailable":
			s.Available = val
		case "Buffers":
			s.Buffers = val
		case "Cached":
			s.Cached = val
		case "SwapTotal":
			s.SwapTotal = val
		case "SwapFree":
			s.SwapFree = val
		}
	}
	if s.SwapTotal > 0 && s.SwapUsed == 0 {
		s.SwapUsed = s.SwapTotal - s.SwapFree
	}
	if s.Total > 0 {
		s.Used = s.Total - s.Free - s.Buffers - s.Cached
	}
	return s
}

func parseDiskstats(raw string, c *SSHCollector) types.DiskIOStats {
	scan := bufio.NewScanner(strings.NewReader(raw))
	var totalRdBytes, totalWrBytes, totalRdCount, totalWrCount uint64

	c.mu.Lock()
	defer c.mu.Unlock()

	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 14 {
			continue
		}
		devName := fields[2]

		// Skip partitions (names with digits like sda1, vda2)
		if strings.ContainsAny(devName, "0123456789") {
			continue
		}

		readsComp, _ := strconv.ParseUint(fields[3], 10, 64)
		readsSector, _ := strconv.ParseUint(fields[5], 10, 64)
		wrComp, _ := strconv.ParseUint(fields[7], 10, 64)
		wrSector, _ := strconv.ParseUint(fields[9], 10, 64)

		rdBytes := readsSector * 512
		wrBytes := wrSector * 512

		prev, ok := c.prevDisk[devName]
		if ok {
			totalRdBytes += rdBytes - prev.ReadBytes
			totalWrBytes += wrBytes - prev.WriteBytes
			totalRdCount += readsComp - prev.ReadCount
			totalWrCount += wrComp - prev.WriteCount
		}
		c.prevDisk[devName] = diskStat{ReadBytes: rdBytes, WriteBytes: wrBytes, ReadCount: readsComp, WriteCount: wrComp}
	}

	return types.DiskIOStats{
		ReadBytes:  totalRdBytes >> 10,
		WriteBytes: totalWrBytes >> 10,
		ReadCount:  totalRdCount,
		WriteCount: totalWrCount,
	}
}

func parseNetdev(raw string, c *SSHCollector) types.NetworkIOStats {
	scan := bufio.NewScanner(strings.NewReader(raw))
	var totalRecv, totalSent, totalPktRecv, totalPktSent uint64

	c.mu.Lock()
	defer c.mu.Unlock()

	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		dev := strings.TrimSpace(line[:colonIdx])
		if dev == "lo" {
			continue
		}

		fields := strings.Fields(strings.TrimSpace(line[colonIdx+1:]))
		if len(fields) < 10 {
			continue
		}
		recv, _ := strconv.ParseUint(fields[0], 10, 64)
		pktRecv, _ := strconv.ParseUint(fields[1], 10, 64)
		sent, _ := strconv.ParseUint(fields[8], 10, 64)
		pktSent, _ := strconv.ParseUint(fields[9], 10, 64)

		totalRecv += recv
		totalSent += sent
		totalPktRecv += pktRecv
		totalPktSent += pktSent
	}

	prev := c.prevNet
	stats := types.NetworkIOStats{
		BytesRecv:   totalRecv - prev.BytesRecv,
		BytesSent:   totalSent - prev.BytesSent,
		PacketsRecv: totalPktRecv - prev.PacketsRecv,
		PacketsSent: totalPktSent - prev.PacketsSent,
	}
	c.prevNet = netStat{BytesRecv: totalRecv, BytesSent: totalSent, PacketsRecv: totalPktRecv, PacketsSent: totalPktSent}
	return stats
}

func parsePs(raw string, c *SSHCollector) []types.ProcessInfo {
	var procs []types.ProcessInfo
	scan := bufio.NewScanner(strings.NewReader(raw))

	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}

		pid, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}

		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		rssKB, _ := strconv.ParseUint(fields[5], 10, 64)
		rssBytes := rssKB * 1024

		username := fields[0]
		status := fields[7]
		numThreads, _ := strconv.ParseInt(fields[6], 10, 64)

		// Command can have spaces -> join fields[10:] back together
		command := strings.Join(fields[10:], " ")

		// Extract just the executable name for the process name
		parts := strings.Fields(command)
		procName := command
		if len(parts) > 0 {
			procName = parts[0]
		}

		c.mu.Lock()
		prev := c.prevProc[int32(pid)]
		cpuPct := cpu
		if cpu == 0 {
			cpuPct = prev.CpuPercent
		}
		c.prevProc[int32(pid)] = procStat{CpuPercent: cpuPct, MemPercent: mem, MemBytes: rssBytes}
		c.mu.Unlock()

		procs = append(procs, types.ProcessInfo{
			PID:        int32(pid),
			Name:       procName,
			CPUPercent: cpuPct,
			MemPercent: float32(mem),
			MemBytes:   rssBytes,
			Status:     status,
			CreateTime: 0,
			PPID:       0,
			Username:   username,
			NumThreads: int32(numThreads),
		})
	}
	return procs
}
