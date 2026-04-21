package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"log-monitoring/internal/collector"
)

type tickMsg time.Time
type statsLoadedMsg struct {
	snapshot statsSnapshot
}
type remoteStatsMsg struct {
	serverIdx int
	snapshot  collector.RemoteSnapshot
}

func (m *Model) Init() tea.Cmd {
	if m.mode == "remote" {
		return tea.Batch(m.tick(), m.collectRemoteStats())
	}
	return tea.Batch(m.tick(), m.collectStatsCmd(true))
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(m.updateInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) collectStatsCmd(includeProcesses bool) tea.Cmd {
	return func() tea.Msg {
		return statsLoadedMsg{snapshot: m.CollectSnapshotLocal(includeProcesses)}
	}
}

func (m *Model) collectRemoteStats() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.servers))
	for i := range m.servers {
		idx := i
		srv := m.servers[idx]
		if srv.sshCollector == nil {
			continue
		}
		cmd := func() tea.Msg {
			snap := srv.sshCollector.Collect()
			return remoteStatsMsg{serverIdx: idx, snapshot: snap}
		}
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(v.Width, v.Height)
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
		return m, nil

	case tickMsg:
		if m.paused {
			return m, m.tick()
		}
		if m.collecting {
			return m, m.tick()
		}
		m.tickCount++
		m.collecting = true
		if m.mode == "remote" {
			return m, tea.Batch(m.tick(), m.collectRemoteStats())
		}
		return m, tea.Batch(m.tick(), m.collectStatsCmd(m.shouldRefreshProcesses()))

	case statsLoadedMsg:
		m.collecting = false
		if v.snapshot.err == nil || !isZeroSnapshot(v.snapshot) {
			m.ApplySnapshot(v.snapshot)
		}
		return m, nil

	case remoteStatsMsg:
		m.collecting = false
		m.ApplyRemoteSnapshot(v.snapshot, v.serverIdx)
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(v)
	}
	return m, nil
}

func (m *Model) handleKey(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch v.String() {
	case "ctrl+c", "q":
		m.Cancel()
		return m, tea.Quit

	case "escape":
		handled := false
		if m.searchMode {
			m.searchMode = false
			m.searchText = ""
			handled = true
		}
		if m.showKillConfirm {
			m.showKillConfirm = false
			handled = true
		}
		if m.processDetailMode {
			m.processDetailMode = false
			m.lockedPID = 0
			handled = true
		}
		if handled {
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}
		return m, nil

	case "/":
		if !m.processDetailMode && !m.showKillConfirm {
			m.searchMode = !m.searchMode
			if !m.searchMode {
				m.searchText = ""
			}
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	case "enter":
		if m.searchMode {
			m.searchMode = false
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
			return m, nil
		}
		procs := m.GetFilteredProcesses()
		if len(procs) > 0 && m.selectedProcess < len(procs) {
			m.processDetailMode = !m.processDetailMode
			if m.processDetailMode {
				m.lockedPID = procs[m.selectedProcess].PID
				m.lockedProc = procs[m.selectedProcess]
			} else {
				m.lockedPID = 0
				m.showKillConfirm = false
			}
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}
		return m, nil

	case "up", "k":
		if !m.processDetailMode && !m.showKillConfirm {
			if m.selectedProcess > 0 {
				m.selectedProcess--
			}
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	case "down", "j":
		if !m.processDetailMode && !m.showKillConfirm {
			maxProc := len(m.GetFilteredProcesses()) - 1
			if m.selectedProcess < maxProc {
				m.selectedProcess++
			}
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	case "tab":
		m.nextPanel()
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()

	case "shift+tab":
		m.prevPanel()
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()

	case "left", "h":
		if m.mode == "remote" && len(m.servers) > 0 {
			m.selectedServer = (m.selectedServer - 1 + len(m.servers)) % len(m.servers)
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
			return m, nil
		}
		m.selectedPanel = panelProcesses
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()

	case "right", "l":
		if m.mode == "remote" && len(m.servers) > 0 {
			m.selectedServer = (m.selectedServer + 1) % len(m.servers)
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
			return m, nil
		}
		m.selectedPanel = panelProcesses
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()

	case " ":
		m.paused = !m.paused

	case "c":
		m.SetSort(sortCPU)
	case "m":
		m.SetSort(sortMem)
	case "p":
		m.SetSort(sortPID)
	case "a":
		m.SetSort(sortName)

	case "1":
		m.selectedPanel = panelCPU
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
	case "2":
		m.selectedPanel = panelMemory
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
	case "3":
		m.selectedPanel = panelDisk
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
	case "4":
		m.selectedPanel = panelNetwork
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()
	case "0":
		m.selectedPanel = panelProcesses
		m.mu.Lock()
		m.uiCacheDirty = true
		m.mu.Unlock()

	case "K":
		if m.processDetailMode && !m.showKillConfirm && m.mode != "remote" {
			m.showKillConfirm = true
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	case "y":
		if m.showKillConfirm {
			m.showKillConfirm = false
			m.processDetailMode = false
			_, msg := m.KillSelectedProcess()
			m.killMessage = msg
			m.killMessageAt = time.Now()
			m.selectedProcess = 0
			m.procsDirty = true
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
			return m, tea.Batch(m.tick(), m.collectStatsCmd(true))
		}

	case "n":
		if m.showKillConfirm {
			m.showKillConfirm = false
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	case "backspace":
		if m.searchMode && len(m.searchText) > 0 {
			m.searchText = m.searchText[:len(m.searchText)-1]
			m.mu.Lock()
			m.uiCacheDirty = true
			m.mu.Unlock()
		}

	default:
		if m.searchMode && !m.processDetailMode && !m.showKillConfirm {
			key := v.String()
			if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
				m.searchText += key
				m.mu.Lock()
				m.uiCacheDirty = true
				m.mu.Unlock()
			}
		}
	}
	return m, nil
}

func isZeroSnapshot(snapshot statsSnapshot) bool {
	return len(snapshot.cpu.PerCore) == 0 &&
		snapshot.cpu.Total == 0 &&
		snapshot.mem.Total == 0 &&
		snapshot.disk.ReadBytes == 0 &&
		snapshot.net.BytesRecv == 0 &&
		!snapshot.hasProc
}

func (m *Model) nextPanel() {
	switch m.selectedPanel {
	case panelProcesses:
		m.selectedPanel = panelCPU
	case panelCPU:
		m.selectedPanel = panelMemory
	case panelMemory:
		m.selectedPanel = panelDisk
	case panelDisk:
		m.selectedPanel = panelNetwork
	case panelNetwork:
		m.selectedPanel = panelProcesses
	}
}

func (m *Model) prevPanel() {
	switch m.selectedPanel {
	case panelProcesses:
		m.selectedPanel = panelNetwork
	case panelCPU:
		m.selectedPanel = panelProcesses
	case panelMemory:
		m.selectedPanel = panelCPU
	case panelDisk:
		m.selectedPanel = panelMemory
	case panelNetwork:
		m.selectedPanel = panelDisk
	}
}
