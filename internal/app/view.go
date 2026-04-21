package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"log-monitoring/internal/types"
)

var (
	bgDark       = lipgloss.Color("#0F1017")
	bgPanel      = lipgloss.Color("#090A10")
	border       = lipgloss.Color("#4A4D62")
	borderActive = lipgloss.Color("#7E839C")
	titleMuted   = lipgloss.Color("#8C90A9")
	titleBlue    = lipgloss.Color("#BABFD6")
	green        = lipgloss.Color("#8FB676")
	yellow       = lipgloss.Color("#D3B66F")
	red          = lipgloss.Color("#D97A6D")
	cyan         = lipgloss.Color("#8BC9D9")
	orange       = lipgloss.Color("#D8A873")
	white        = lipgloss.Color("#CFD3E6")
	selected     = lipgloss.Color("#151821")
	dim          = lipgloss.Color("#676C84")
)

var (
	boxBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "┌",
		TopRight:    "┐",
		BottomLeft:  "└",
		BottomRight: "┘",
	}

	baseBoxStyle = lipgloss.NewStyle().
			Border(boxBorder).
			Background(bgPanel)
)

func strip(s string) string {
	runes := []rune{}
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape && (r == 'm' || r == ';' || (r >= '0' && r <= '9') || r == '[') {
			continue
		}
		inEscape = false
		runes = append(runes, r)
	}
	return string(runes)
}

func col(s string, c lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(c).Render(s)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	m.mu.Lock()
	dirty := m.uiCacheDirty
	m.mu.Unlock()

	if !dirty {
		m.mu.Lock()
		cached := m.uiCache
		m.mu.Unlock()
		if cached != "" {
			return cached
		}
	}

	if m.mode == "remote" {
		return m.renderRemoteUI()
	}
	return m.renderUI()
}

func (m *Model) renderUI() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.width < 80 || m.height < 24 {
		m.uiCache = col("Terminal too small. Resize to at least 80x24.", yellow)
		m.uiCacheDirty = false
		return m.uiCache
	}

	header := m.renderHeader()
	contentHeight := max(12, m.height-2)
	topHeight := clamp(contentHeight/3, 9, 12)
	bottomHeight := max(10, contentHeight-topHeight)
	leftWidth := clamp(m.width/3, 28, 38)
	rightWidth := max(30, m.width-leftWidth)

	cpuPanel := m.renderCPUBox(m.width, topHeight)
	leftColumn := m.renderLeftColumn(leftWidth, bottomHeight)
	procPanel := m.renderProcBox(rightWidth, bottomHeight)

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, procPanel)

	m.uiCache = lipgloss.JoinVertical(lipgloss.Left, header, cpuPanel, bottomRow)
	m.uiCacheDirty = false
	return m.uiCache
}

func (m *Model) renderHeader() string {
	now := time.Now().Format("15:04:05")
	uptime := formatDur(time.Since(m.startedAt))
	status := "RUN"
	statusColor := green
	if m.paused {
		status = "PAUSE"
		statusColor = yellow
	}

	left := col("log-monitoring", white) + " " + col("system monitor", dim)
	right := col(status, statusColor) + "  " + col("up", titleMuted) + " " + col(uptime, cyan) + "  " + col(now, white)

	// Kill message notification
	if m.killMessage != "" {
		msgColor := green
		if strings.Contains(m.killMessage, "failed") || strings.Contains(m.killMessage, "error") || strings.Contains(m.killMessage, "permission") {
			msgColor = red
		}
		killTag := " " + col("[kill]", msgColor) + " " + col(m.killMessage, msgColor)
		right += killTag
	}

	gap := strings.Repeat(" ", max(1, m.width-stripLen(left)-stripLen(right)))
	return left + gap + right
}

func (m *Model) renderCPUBox(w, h int) string {
	style := m.panelStyle(panelCPU).Width(w).Height(h)
	innerW := max(20, w-4)
	innerH := max(5, h-2)
	graphH := max(5, innerH-3)
	sideW := min(34, max(24, innerW/3))
	graphW := max(30, innerW-sideW-4)

	headerLine := panelCaption("cpu", fmt.Sprintf("total %.1f%%", m.cpuStats.Total), m.selectedPanel == panelCPU)
	summaryLine := strings.Join([]string{
		renderInlineStat("user", fmt.Sprintf("%.1f%%", m.cpuStats.User), m.cpuStats.User),
		renderInlineStat("temp", fmt.Sprintf("%dC", pseudoTemp(m.cpuStats.Total)), m.cpuStats.Total),
		renderInlineStat("load", fmt.Sprintf("%.2f", m.cpuStats.Total/100*float64(max(1, len(m.cpuStats.PerCore)))), m.cpuStats.Total),
	}, "   ")
	graphLines := renderBtopGraph(m.cpuHistory, graphW, graphH, cyan)
	graphBlock := headerLine + "\n" + summaryLine + "\n" + strings.Join(graphLines, "\n")

	sideLines := []string{col("cores", titleMuted)}
	for i, core := range m.cpuStats.PerCore {
		if len(sideLines) >= graphH+2 {
			break
		}
		sideLines = append(sideLines, fmt.Sprintf("c%-2d  %s", i, compactMeter(core, sideW-8)))
	}
	for len(sideLines) < graphH+2 {
		sideLines = append(sideLines, "")
	}

	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(graphW).PaddingLeft(1).PaddingRight(1).Render(graphBlock),
		"  ",
		lipgloss.NewStyle().Width(sideW).Render(strings.Join(sideLines, "\n")),
	)
	return style.Render(content)
}

func (m *Model) renderLeftColumn(w, h int) string {
	memH := max(9, h/2)
	diskH := max(6, h/3)
	netH := max(6, h-memH-diskH)

	mem := m.renderMemBox(w, memH)
	disk := m.renderDiskBox(w, diskH)
	net := m.renderNetBox(w, netH)

	return lipgloss.JoinVertical(lipgloss.Left, mem, disk, net)
}

func (m *Model) renderMemBox(w, h int) string {
	style := m.panelStyle(panelMemory).Width(w).Height(h)

	total := bytesHuman(m.memStats.Total)
	used := bytesHuman(m.memStats.Used)
	avail := bytesHuman(m.memStats.Available)
	free := bytesHuman(m.memStats.Free)
	cached := bytesHuman(m.memStats.Cached)

	usedPct := percentOf(m.memStats.Used, m.memStats.Total)
	swapPct := percentOf(m.memStats.SwapUsed, m.memStats.SwapTotal)

	barW := max(10, w-14)
	lines := []string{
		panelCaption("mem", fmt.Sprintf("%s / %s", used, total), m.selectedPanel == panelMemory),
		renderTightMetric("used", meterLine(usedPct, barW, false)),
		renderTightMetric("total", colorizeValue(total, 0)),
		renderTightMetric("free", colorizeValue(free, 0)),
		renderTightMetric("avail", colorizeValue(avail, 0)),
		renderTightMetric("cache", colorizeValue(cached, 0)),
		renderTightMetric("swap", meterLine(swapPct, barW, false)),
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderDiskBox(w, h int) string {
	style := m.panelStyle(panelDisk).Width(w).Height(h)

	lines := []string{
		panelCaption("disks", fmt.Sprintf("%s in / %s out", formatRate(m.diskStats.ReadBytes), formatRate(m.diskStats.WriteBytes)), m.selectedPanel == panelDisk),
		renderTightMetric("hist", tinyHistory(combineHistories(m.diskRHistory, m.diskWHistory), max(6, w-10))),
		renderTightMetric("read", meterLine(scaleRate(m.diskStats.ReadBytes, 2*1024*1024), max(6, w-14), true)),
		renderTightMetric("write", meterLine(scaleRate(m.diskStats.WriteBytes, 2*1024*1024), max(6, w-14), true)),
		renderTightMetric("ops", colorizeValue(fmt.Sprintf("R %d", m.diskStats.ReadCount), 0)+" "+col("W", titleMuted)+" "+col(fmt.Sprintf("%d", m.diskStats.WriteCount), white)),
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderNetBox(w, h int) string {
	style := m.panelStyle(panelNetwork).Width(w).Height(h)
	graphW := max(10, w-4)
	graphH := max(3, h-5)
	graphLines := renderDualBtopGraph(m.netRHistory, m.netWHistory, graphW, graphH)

	lines := []string{
		panelCaption("net", fmt.Sprintf("%s down / %s up", formatRate(m.netStats.BytesRecv), formatRate(m.netStats.BytesSent)), m.selectedPanel == panelNetwork),
		renderTightMetric("down", colorizeValue(formatRate(m.netStats.BytesRecv), float64(scaleRate(m.netStats.BytesRecv, 20*1024*1024)))),
		renderTightMetric("up", colorizeValue(formatRate(m.netStats.BytesSent), float64(scaleRate(m.netStats.BytesSent, 20*1024*1024)))),
		strings.Join(graphLines, "\n"),
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderProcBox(w, h int) string {
	style := m.panelStyle(panelProcesses).Width(w).Height(h)
	innerW := max(20, w-4)
	innerH := max(4, h-2)
	procs := m.GetFilteredProcesses()
	if m.selectedProcess >= len(procs) && len(procs) > 0 {
		m.selectedProcess = len(procs) - 1
	}
	selectedProc, hasSelected := m.selectedProcessData(procs)

	// Expanded detail view (when Enter is pressed)
	if m.processDetailMode {
		// Use locked process data if available, otherwise fallback
		detailProc := m.lockedProc
		hasDetail := m.lockedPID != 0
		detailLines := m.renderProcessDetailExpanded(innerW, innerH, detailProc, hasDetail)
		content := strings.Join(detailLines, "\n")
		return style.Render(content)
	}

	detailW := min(30, max(24, innerW/4))
	listW := max(40, innerW-detailW-1)
	searchOffset := ternary(m.searchMode, 1, 0)
	visibleRows := max(3, innerH-4-searchOffset)

	start := 0
	if m.selectedProcess >= visibleRows {
		start = m.selectedProcess - visibleRows + 1
	}
	end := min(len(procs), start+visibleRows)

	sortInfo := fmt.Sprintf("sort %s %s", m.sortField, ternary(m.sortDesc, "desc", "asc"))
	title := panelCaption("proc", sortInfo, m.selectedPanel == panelProcesses)
	header := fitProcRow(listW, false,
		col("pid", titleMuted),
		col("program", titleMuted),
		col("cpu", titleMuted),
		col("em", titleMuted),
		col("mem", titleMuted),
		col("time", titleMuted),
		col("user", titleMuted),
		col("", titleMuted),
	)
	subHeader := col("status", dim)

	listLines := []string{title, header, subHeader}

	// Search mode input
	if m.searchMode {
		searchLine := col("search", cyan) + " " + m.searchText + col("_", white)
		listLines = append(listLines, searchLine)
	}

	for idx := start; idx < end; idx++ {
		p := procs[idx]
		memBytes := bytesHuman(p.MemBytes)
		elapsed := formatProcElapsed(time.Since(time.UnixMilli(p.CreateTime)))
		row := fitProcRow(listW, idx == m.selectedProcess,
			fmt.Sprintf("%d", p.PID),
			p.Name,
			colorizeValue(fmt.Sprintf("%.1f", p.CPUPercent), p.CPUPercent),
			colorizeValue(fmt.Sprintf("%.1f", p.MemPercent), float64(p.MemPercent)),
			colorizeValue(memBytes, float64(p.MemPercent)),
			col(elapsed, titleMuted),
			shorten(p.Username, 10),
			col(string([]rune(shortStatus(p.Status))[0]), statusColor(shortStatus(p.Status))),
		)
		listLines = append(listLines, row)
	}

	for len(listLines) < innerH-1 {
		listLines = append(listLines, "")
	}

	footer := col("shown", dim) + " " + col(fmt.Sprintf("%d", len(procs)), white) + "  " + col("selected", dim) + " " + col(fmt.Sprintf("%d", m.selectedProcess+1), white) + "  " + col("[/]search [enter]detail [k]kill [tab][0-4][q]", dim)
	listLines = append(listLines[:min(len(listLines), innerH-1)], footer)

	for i, line := range listLines {
		listLines[i] = lipgloss.NewStyle().Width(listW).Render(line)
	}

	detailLines := m.renderProcDetails(detailW, innerH, selectedProc, hasSelected)
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(listLines, "\n"),
		" ",
		strings.Join(detailLines, "\n"),
	)

	return style.Render(content)
}

func (m *Model) selectedProcessData(procs []types.ProcessInfo) (types.ProcessInfo, bool) {
	if len(procs) == 0 || m.selectedProcess < 0 || m.selectedProcess >= len(procs) {
		return types.ProcessInfo{}, false
	}
	return procs[m.selectedProcess], true
}

func (m *Model) renderProcDetails(width, height int, p types.ProcessInfo, ok bool) []string {
	lines := []string{panelCaption("detail", "", false)}
	if !ok {
		lines = append(lines, col("no process selected", dim))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return fixedWidthLines(lines, width)
	}

	lines = append(lines,
		renderTightMetric("name", shorten(p.Name, width-7)),
		renderTightMetric("pid", fmt.Sprintf("%d", p.PID)),
		renderTightMetric("user", shorten(p.Username, width-7)),
		renderTightMetric("state", col(shortStatus(p.Status), statusColor(shortStatus(p.Status)))),
		renderTightMetric("cpu", colorizeValue(fmt.Sprintf("%.1f%%", p.CPUPercent), p.CPUPercent)),
		renderTightMetric("mem", colorizeValue(fmt.Sprintf("%.1f%%", p.MemPercent), float64(p.MemPercent))),
		renderTightMetric("rss", bytesHuman(p.MemBytes)),
		renderTightMetric("thr", fmt.Sprintf("%d", p.NumThreads)),
		renderTightMetric("ppid", fmt.Sprintf("%d", p.PPID)),
		renderTightMetric("time", formatProcElapsed(time.Since(time.UnixMilli(p.CreateTime)))),
		"",
		col("cpu history", titleMuted),
	)

	hist := make([]float64, len(m.cpuHistory))
	for i := range hist {
		hist[i] = clampFloat(m.cpuHistory[i]*0.7+p.CPUPercent*0.3, 0, 100)
	}
	lines = append(lines, renderBtopGraph(hist, max(8, width), max(3, height-len(lines)-1), green)...)

	for len(lines) < height {
		lines = append(lines, "")
	}
	return fixedWidthLines(lines, width)
}

func fixedWidthLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = lipgloss.NewStyle().Width(width).Render(line)
	}
	return out
}

func (m *Model) renderProcessDetailExpanded(w, h int, p types.ProcessInfo, ok bool) []string {
	title := panelCaption("process detail", "", m.selectedPanel == panelProcesses)
	lines := []string{title}

	if !ok {
		lines = append(lines, col("no process selected", dim))
		for len(lines) < h {
			lines = append(lines, "")
		}
		return fixedWidthLines(lines, w)
	}

	appendLine := func(label, value string) {
		lines = append(lines, fmt.Sprintf("%-8s %s", col(label, titleMuted), value))
	}

	appendLine("name", p.Name)
	appendLine("pid", fmt.Sprintf("%d", p.PID))
	lines = append(lines, "")

	appendLine("user", p.Username)
	appendLine("state", col(shortStatus(p.Status), statusColor(shortStatus(p.Status))))
	appendLine("status", p.Status)
	lines = append(lines, "")

	appendLine("cpu", colorizeValue(fmt.Sprintf("%.1f%%", p.CPUPercent), p.CPUPercent))
	appendLine("mem", colorizeValue(fmt.Sprintf("%.1f%%", p.MemPercent), float64(p.MemPercent)))
	appendLine("rss", bytesHuman(p.MemBytes))
	lines = append(lines, "")

	appendLine("threads", fmt.Sprintf("%d", p.NumThreads))
	appendLine("ppid", fmt.Sprintf("%d", p.PPID))
	lines = append(lines, "")

	appendLine("started", time.UnixMilli(p.CreateTime).Format("2006-01-02 15:04:05"))
	appendLine("elapsed", formatProcElapsed(time.Since(time.UnixMilli(p.CreateTime))))
	lines = append(lines, "")

	lines = append(lines, col("cpu history", titleMuted))
	hist := make([]float64, len(m.cpuHistory))
	for i := range hist {
		hist[i] = clampFloat(m.cpuHistory[i]*0.7+p.CPUPercent*0.3, 0, 100)
	}
	lines = append(lines, renderBtopGraph(hist, max(10, w-4), max(3, h-len(lines)-2), green)...)

	// Keyboard hints
	if m.showKillConfirm {
		lines = append(lines, "")
		lines = append(lines, col("kill process?", red))
		procs := m.GetFilteredProcesses()
		if m.selectedProcess < len(procs) {
			p := procs[m.selectedProcess]
			lines = append(lines, fmt.Sprintf("  %s (pid %d)", p.Name, p.PID))
		}
		lines = append(lines, "")
		lines = append(lines, col("[y] yes, kill it    [n/esc] cancel", dim))
	} else {
		lines = append(lines, "")
		hintLine := col("[K]", red) + " kill  " + col("[esc/enter]", titleMuted) + " close"
		lines = append(lines, hintLine)
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return fixedWidthLines(lines, w)
}

func fitProcRow(width int, selectedRow bool, cols ...string) string {
	pidW := 6
	nameW := max(18, width-49)
	cpuW := 4
	memW := 4
	memBytesW := 7
	timeW := 7
	userW := 9
	statusW := 1

	values := []string{
		lipgloss.NewStyle().Width(pidW).Align(lipgloss.Left).Render(shorten(strip(cols[0]), pidW)),
		lipgloss.NewStyle().Width(nameW).Align(lipgloss.Left).Render(shorten(strip(cols[1]), nameW)),
		lipgloss.NewStyle().Width(cpuW).Align(lipgloss.Right).Render(shorten(strip(cols[2]), cpuW)),
		lipgloss.NewStyle().Width(memW).Align(lipgloss.Right).Render(shorten(strip(cols[3]), memW)),
		lipgloss.NewStyle().Width(memBytesW).Align(lipgloss.Right).Render(shorten(strip(cols[4]), memBytesW)),
		lipgloss.NewStyle().Width(timeW).Align(lipgloss.Right).Render(shorten(strip(cols[5]), timeW)),
		lipgloss.NewStyle().Width(userW).Align(lipgloss.Left).Render(shorten(strip(cols[6]), userW)),
		lipgloss.NewStyle().Width(statusW).Align(lipgloss.Left).Render(shorten(strip(cols[7]), statusW)),
	}

	row := strings.Join(values, " ")
	style := lipgloss.NewStyle().Foreground(white)
	if selectedRow {
		style = style.Background(selected).Foreground(lipgloss.Color("#F8FAFC"))
	}
	return style.Render(row)
}

func windowPill(label string, active bool, suffix string) string {
	bg := lipgloss.Color("#353845")
	fg := lipgloss.Color("#A8ADBE")
	if active {
		bg = lipgloss.Color("#4B4F5A")
		fg = white
	}
	pill := lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Padding(0, 1).
		Render(" " + label + " ")
	return pill + lipgloss.NewStyle().Foreground(dim).Render(" "+suffix)
}

func rightAlignBlock(lines []string, width int) string {
	out := make([]string, len(lines))
	for i, line := range lines {
		pad := max(0, width-stripLen(line))
		out[i] = strings.Repeat(" ", pad) + line
	}
	return strings.Join(out, "\n")
}

func (m *Model) panelStyle(panel activePanel) lipgloss.Style {
	active := m.selectedPanel == panel
	panelBorder := border
	if active {
		panelBorder = borderActive
	}
	return baseBoxStyle.BorderForeground(panelBorder)
}

func renderAreaGraph(history []float64, width, height int) []string {
	width = max(1, width)
	height = max(1, height)
	trimmed := resampleHistory(history, width)

	lines := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		for _, value := range trimmed {
			filled := int((clampFloat(value, 0, 100) / 100) * float64(height))
			cell := " "
			if height-row <= filled {
				cell = "█"
			} else if height-row == filled+1 && filled > 0 {
				cell = "▄"
			}
			b.WriteString(colorGraphCell(cell, value))
		}
		lines[row] = b.String()
	}
	return lines
}

func renderHistogram(history []float64, width, height int) []string {
	width = max(1, width)
	height = max(1, height)
	trimmed := resampleHistory(history, width)
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		for _, value := range trimmed {
			level := int((clampFloat(value, 0, 100) / 100) * float64(height))
			cell := " "
			if height-row <= level {
				cell = "█"
			}
			b.WriteString(colorGraphCell(cell, value))
		}
		lines[row] = b.String()
	}
	return lines
}

func resampleHistory(history []float64, width int) []float64 {
	if len(history) == 0 {
		return make([]float64, width)
	}
	if len(history) == width {
		return history
	}
	out := make([]float64, width)
	for i := 0; i < width; i++ {
		start := int(float64(i) * float64(len(history)) / float64(width))
		end := int(float64(i+1) * float64(len(history)) / float64(width))
		if end <= start {
			end = start + 1
		}
		if end > len(history) {
			end = len(history)
		}
		sum := 0.0
		for _, v := range history[start:end] {
			sum += v
		}
		out[i] = sum / float64(end-start)
	}
	return out
}

func combineHistories(a, b []float64) []float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	maxVal := 1.0
	for i := 0; i < n; i++ {
		out[i] = a[i] + b[i]
		if out[i] > maxVal {
			maxVal = out[i]
		}
	}
	for i := range out {
		out[i] = (out[i] / maxVal) * 100
	}
	return out
}

func meterLine(value float64, width int, hot bool) string {
	width = max(4, width)
	pct := clampFloat(value, 0, 100)
	filled := int((pct / 100) * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	fillColor := green
	if pct >= 75 || hot {
		fillColor = yellow
	}
	if pct >= 90 {
		fillColor = red
	}

	bar := lipgloss.NewStyle().Foreground(fillColor).Render(strings.Repeat("■", filled))
	gap := lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("·", empty))
	return fmt.Sprintf("%s%s %s", bar, gap, colorizeValue(fmt.Sprintf("%5.1f%%", pct), pct))
}

func compactMeter(value float64, width int) string {
	width = max(4, width)
	pct := clampFloat(value, 0, 100)
	filled := int((pct / 100) * float64(width))
	if filled > width {
		filled = width
	}
	bar := lipgloss.NewStyle().Foreground(colorForPercent(pct)).Render(strings.Repeat("■", filled))
	gap := lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("·", width-filled))
	return fmt.Sprintf("%s%s %s", bar, gap, colorizeValue(fmt.Sprintf("%4.1f", pct), pct))
}

func tinyHistory(history []float64, width int) string {
	width = max(4, width)
	history = resampleHistory(history, width)
	levels := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	var b strings.Builder
	for _, v := range history {
		idx := int((clampFloat(v, 0, 100) / 100) * float64(len(levels)-1))
		b.WriteString(colorGraphCell(levels[idx], v))
	}
	return b.String()
}

func renderBtopGraph(history []float64, width, height int, mainColor lipgloss.Color) []string {
	width = max(1, width)
	height = max(1, height)
	values := resampleHistory(history, width)
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		threshold := float64(height-row) / float64(height) * 100
		for _, v := range values {
			switch {
			case v >= threshold:
				b.WriteString(col("█", blendGraphColor(v, mainColor)))
			case v >= threshold-10:
				b.WriteString(col("▄", blendGraphColor(v, mainColor)))
			default:
				b.WriteString(col("·", border))
			}
		}
		lines[row] = b.String()
	}
	return lines
}

func renderDualBtopGraph(a, b []float64, width, height int) []string {
	width = max(1, width)
	height = max(1, height)
	down := normalizeRateHistory(a, width)
	up := normalizeRateHistory(b, width)
	lines := make([]string, height)
	for row := 0; row < height; row++ {
		var sb strings.Builder
		threshold := float64(height-row) / float64(height) * 100
		for i := 0; i < width; i++ {
			cell := col("·", border)
			switch {
			case up[i] >= threshold:
				cell = col("█", orange)
			case down[i] >= threshold:
				cell = col("█", cyan)
			case up[i] >= threshold-10:
				cell = col("▄", orange)
			case down[i] >= threshold-10:
				cell = col("▄", cyan)
			}
			sb.WriteString(cell)
		}
		lines[row] = sb.String()
	}
	return lines
}

func normalizeRateHistory(history []float64, width int) []float64 {
	values := resampleHistory(history, width)
	maxVal := 1.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	for i := range values {
		values[i] = values[i] / maxVal * 100
	}
	return values
}

// renderMemAreaGraph draws a btop-style filled area + line graph for memory.
// Area below the line is filled with block chars; the line itself is brighter.
func renderMemAreaGraph(history []float64, width, height int) []string {
	width = max(1, width)
	height = max(1, height)

	// Use raw data points, one char per data point (like btop)
	values := resampleHistory(history, width)
	if len(values) == 0 {
		values = make([]float64, width)
	}

	lines := make([]string, height)

	for row := 0; row < height; row++ {
		var b strings.Builder
		// Threshold for this row: 0% at bottom, 100% at top
		threshold := float64(height-row) / float64(height) * 100

		for i := 0; i < width; i++ {
			v := values[i]
			above := v >= threshold
			// One cell below line top?
			oneBelow := i > 0 && values[i-1] >= threshold-1

			switch {
			case above:
				// This cell is on or below the line — filled area
				b.WriteString(col("█", memGraphColor(v)))
			case oneBelow:
				// Line peak above — draw half block at boundary
				b.WriteString(col("▄", memGraphColor(v)))
			default:
				// Empty space
				b.WriteString(col(" ", lipgloss.Color("#0F1017")))
			}
		}
		lines[row] = b.String()
	}

	return lines
}

// memGraphColor returns a gradient color for memory usage value.
func memGraphColor(v float64) lipgloss.Color {
	switch {
	case v >= 90:
		return red
	case v >= 75:
		return yellow
	case v >= 50:
		return green
	case v >= 25:
		return lipgloss.Color("#6DAF8E") // muted green
	default:
		return lipgloss.Color("#6BA3BE") // muted cyan
	}
}

// renderSwapMiniGraph draws a compact bar-style mini graph for swap usage.
// Normalized to its own max (swap is usually small), placed beside main graph.
func renderSwapMiniGraph(history []float64, width, height int) []string {
	width = max(1, width)
	height = max(1, height)
	values := resampleHistory(history, width)

	maxVal := 1.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}

	lines := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		threshold := float64(height-row) / float64(height) * 100
		for _, v := range values {
			normV := (v / maxVal) * 100
			switch {
			case normV >= threshold:
				b.WriteString(col("█", swapColor(normV)))
			case normV >= threshold-12:
				b.WriteString(col("▄", swapColor(normV)))
			default:
				b.WriteString(col("·", lipgloss.Color("#1F2230")))
			}
		}
		lines[row] = b.String()
	}
	return lines
}

// swapColor returns color for swap graph based on usage percentage.
func swapColor(v float64) lipgloss.Color {
	switch {
	case v >= 80:
		return red
	case v >= 50:
		return yellow
	case v >= 20:
		return lipgloss.Color("#B07CCC") // purple — matches btop alt theme
	default:
		return lipgloss.Color("#9B8FB8")
	}
}

func blendGraphColor(value float64, base lipgloss.Color) lipgloss.Color {
	switch {
	case value >= 85:
		return yellow
	case value >= 65:
		return green
	default:
		return base
	}
}

func panelCaption(name, meta string, active bool) string {
	labelColor := titleBlue
	metaColor := titleMuted
	if active {
		metaColor = borderActive
	}
	base := col(name, labelColor)
	if meta == "" {
		return base
	}
	return base + " " + col(meta, metaColor)
}

func renderKeyValueLine(key, value string) string {
	return fmt.Sprintf("%-8s %s", col(key, titleMuted), value)
}

func renderTightMetric(key, value string) string {
	return fmt.Sprintf("%-5s %s", col(key, titleMuted), value)
}

func renderInlineStat(key, value string, pct float64) string {
	return col(key, titleMuted) + " " + colorizeValue(value, pct)
}

func colorizeValue(value string, pct float64) string {
	return col(value, colorForPercent(pct))
}

func colorForPercent(pct float64) lipgloss.Color {
	switch {
	case pct >= 85:
		return red
	case pct >= 60:
		return yellow
	case pct >= 25:
		return green
	case pct > 0:
		return cyan
	default:
		return white
	}
}

func colorGraphCell(cell string, value float64) string {
	switch {
	case value >= 80:
		return col(cell, red)
	case value >= 60:
		return col(cell, yellow)
	case value >= 35:
		return col(cell, green)
	default:
		return col(cell, cyan)
	}
}

func shortStatus(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "running"):
		return "run"
	case strings.Contains(s, "sleep"):
		return "slp"
	case strings.Contains(s, "stop"):
		return "stp"
	case strings.Contains(s, "zombie"):
		return "zom"
	case strings.Contains(s, "idle"):
		return "idl"
	default:
		if len(s) > 3 {
			return s[:3]
		}
		return s
	}
}

func statusColor(s string) lipgloss.Color {
	s = strings.ToLower(s)
	switch s {
	case "run":
		return green
	case "slp", "idl":
		return dim
	case "stp":
		return yellow
	case "zom":
		return red
	default:
		return white
	}
}

func formatDur(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatProcElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 99 {
		return fmt.Sprintf("%dh", h)
	}
	if h > 0 {
		return fmt.Sprintf("%02d:%02d", h, m)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatRate(v uint64) string {
	return bytesHuman(v) + "/s"
}

func bytesHuman(v uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case v >= TB:
		return fmt.Sprintf("%.1fT", float64(v)/TB)
	case v >= GB:
		return fmt.Sprintf("%.1fG", float64(v)/GB)
	case v >= MB:
		return fmt.Sprintf("%.1fM", float64(v)/MB)
	case v >= KB:
		return fmt.Sprintf("%.1fK", float64(v)/KB)
	default:
		return fmt.Sprintf("%dB", v)
	}
}

func percentOf(v, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(v) / float64(total) * 100
}

func scaleRate(value uint64, maxValue float64) float64 {
	if maxValue <= 0 {
		return 0
	}
	return (float64(value) / maxValue) * 100
}

func approxFreq(load float64) string {
	base := 2.0 + (clampFloat(load, 0, 100) / 100 * 1.2)
	return fmt.Sprintf("%.2fGHz", base)
}

func pseudoTemp(load float64) int {
	return 42 + int((clampFloat(load, 0, 100)/100)*35)
}

func stripLen(s string) int {
	return len(strip(s))
}

func shorten(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 2 {
		return string(r[:width])
	}
	return string(r[:width-2]) + ".."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func clampFloat(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func minFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	minimum := values[0]
	for _, v := range values[1:] {
		if v < minimum {
			minimum = v
		}
	}
	return minimum
}

func maxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	maximum := values[0]
	for _, v := range values[1:] {
		if v > maximum {
			maximum = v
		}
	}
	return maximum
}

func ternary[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

// --- Remote / multi-server view rendering ---

var serverStatusColors = []lipgloss.Color{
	lipgloss.Color("#8FB676"), // green
	lipgloss.Color("#8BC9D9"), // cyan
	lipgloss.Color("#D8A873"), // orange
	lipgloss.Color("#D3B66F"), // yellow
	lipgloss.Color("#D97A6D"), // red
	lipgloss.Color("#BABFD6"), // blue
}

func (m *Model) renderRemoteUI() string {
	if m.width < 80 || m.height < 24 {
		return col("Terminal too small. Resize to at least 80x24.", yellow)
	}

	header := m.renderRemoteHeader()
	contentHeight := max(10, m.height-2)

	// Top section: server selector tabs + CPU overview per server
	cpuRows := max(6, contentHeight/3)
	cpuSection := m.renderRemoteCPUGrid(m.width, cpuRows)

	// Bottom: 2-column layout — Mem/Disk left, Net/Proc right
	bottomH := max(8, contentHeight-cpuRows)
	leftW := clamp(m.width/3, 28, 40)
	rightW := max(30, m.width-leftW)
	detailSection := m.renderRemoteDetailGrid(leftW, rightW, bottomH)

	body := lipgloss.JoinVertical(lipgloss.Left, cpuSection, detailSection)
	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func (m *Model) renderRemoteHeader() string {
	now := time.Now().Format("15:04:05")
	uptime := formatDur(time.Since(m.startedAt))

	var serverTabs []string
	for i, s := range m.servers {
		idx := i
		ss := m.servers[idx]
		ss.mu.Lock()
		connected := ss.connected
		ss.mu.Unlock()

		prefix := " "
		suffix := " "
		tab := ""
		if i == m.selectedServer {
			prefix = "["
			suffix = "]"
			tab = col(prefix+s.Name+suffix, white)
		} else {
			statusCol := red
			if connected {
				statusCol = green
			}
			tab = col(prefix+s.Name+suffix, statusCol)
		}
		serverTabs = append(serverTabs, tab)
	}

	left := col("log-monitoring", white) + " " + col("remote monitor", dim) + "  " + strings.Join(serverTabs, " ")
	right := col("up", titleMuted) + " " + col(uptime, cyan) + "  " + col(now, white)

	// Connection error indicator
	if m.selectedServer < len(m.servers) {
		ss := m.servers[m.selectedServer]
		ss.mu.Lock()
		errMsg := ss.errMsg
		ss.mu.Unlock()
		if errMsg != "" {
			right += "  " + col("[!]", red) + " " + col(errMsg, red)
		}
	}

	gap := strings.Repeat(" ", max(1, m.width-stripLen(left)-stripLen(right)))
	return left + gap + right
}

func (m *Model) renderRemoteCPUGrid(w, h int) string {
	style := m.panelStyle(panelCPU).Width(w).Height(h)
	_ = max(20, w-4) // inner width reserved

	var lines []string
	numServers := len(m.servers)
	if numServers == 0 {
		lines = []string{col("no servers configured", dim)}
	} else {
		// Render one row per server in the grid
		cellW := max(8, (max(20, w-4))/numServers)
		var rowLines []string

		// Header row with server names
		var headerCells []string
		for i, s := range m.servers {
			color := serverStatusColors[i%len(serverStatusColors)]
			sel := ""
			if i == m.selectedServer {
				sel = "*"
			}
			headerCells = append(headerCells, col(sel+s.Name, color))
		}
		rowLines = append(rowLines, strings.Join(headerCells, "  "))

		// CPU usage bar per server
		var cpuLine []string
		for _, s := range m.servers {
			s.mu.Lock()
			cpu := s.cpuStats.Total
			s.mu.Unlock()
			bar := compactMeter(cpu, cellW-6)
			cpuLine = append(cpuLine, bar)
		}
		rowLines = append(rowLines, strings.Join(cpuLine, "  "))

		// CPU % text
		var pctLine []string
		for _, s := range m.servers {
			s.mu.Lock()
			cpu := s.cpuStats.Total
			s.mu.Unlock()
			pctLine = append(pctLine, col(fmt.Sprintf("%.1f%%", cpu), cyan))
		}
		rowLines = append(rowLines, strings.Join(pctLine, "  "))

		// Memory usage bar
		var memLine []string
		for _, s := range m.servers {
			s.mu.Lock()
			memPct := 0.0
			if s.memStats.Total > 0 {
				memPct = float64(s.memStats.Used) / float64(s.memStats.Total) * 100
			}
			s.mu.Unlock()
			memLine = append(memLine, compactMeter(memPct, cellW-6))
		}
		rowLines = append(rowLines, strings.Join(memLine, "  "))

		// Memory % text
		var memPctLine []string
		for _, s := range m.servers {
			s.mu.Lock()
			memPct := 0.0
			if s.memStats.Total > 0 {
				memPct = float64(s.memStats.Used) / float64(s.memStats.Total) * 100
			}
			s.mu.Unlock()
			memPctLine = append(memPctLine, col(fmt.Sprintf("%.1f%%", memPct), green))
		}
		rowLines = append(rowLines, strings.Join(memPctLine, "  "))

		lines = rowLines
	}

	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderRemoteDetailGrid(leftW, rightW, h int) string {
	ss := m.CurrentServer()
	if ss == nil {
		style := baseBoxStyle.Width(rightW).Height(h)
		return style.Render(col("no server selected", dim))
	}

	ss.mu.Lock()
	_ = ss.cpuStats // reserved for detail view
	memStats := ss.memStats
	diskStats := ss.diskStats
	netStats := ss.netStats
	procs := ss.processes
	ss.mu.Unlock()

	// Left column: memory mini stats
	memH := max(6, h/2)
	diskH := max(5, h/3)
	netH := max(4, h-memH-diskH)

	memStyle := m.panelStyle(panelMemory).Width(leftW).Height(memH)
	total := bytesHuman(memStats.Total)
	used := bytesHuman(memStats.Used)
	avail := bytesHuman(memStats.Available)
	usedPct := 0.0
	if memStats.Total > 0 {
		usedPct = float64(memStats.Used) / float64(memStats.Total) * 100
	}
	barW := max(10, leftW-14)
	memLines := []string{
		panelCaption("mem", fmt.Sprintf("%s / %s", used, total), m.selectedPanel == panelMemory),
		renderTightMetric("used", meterLine(usedPct, barW, false)),
		renderTightMetric("avail", colorizeValue(avail, 0)),
	}
	memBlock := memStyle.Render(strings.Join(memLines, "\n"))

	diskStyle := m.panelStyle(panelDisk).Width(leftW).Height(diskH)
	diskLines := []string{
		panelCaption("disk", fmt.Sprintf("%s in / %s out", formatRate(diskStats.ReadBytes), formatRate(diskStats.WriteBytes)), m.selectedPanel == panelDisk),
		renderTightMetric("read", meterLine(scaleRate(diskStats.ReadBytes, 2*1024*1024), max(6, leftW-14), true)),
		renderTightMetric("write", meterLine(scaleRate(diskStats.WriteBytes, 2*1024*1024), max(6, leftW-14), true)),
	}
	diskBlock := diskStyle.Render(strings.Join(diskLines, "\n"))

	netStyle := m.panelStyle(panelNetwork).Width(leftW).Height(netH)
	netLines := []string{
		panelCaption("net", fmt.Sprintf("%s down / %s up", formatRate(netStats.BytesRecv), formatRate(netStats.BytesSent)), m.selectedPanel == panelNetwork),
		renderTightMetric("down", colorizeValue(formatRate(netStats.BytesRecv), float64(scaleRate(netStats.BytesRecv, 20*1024*1024)))),
		renderTightMetric("up", colorizeValue(formatRate(netStats.BytesSent), float64(scaleRate(netStats.BytesSent, 20*1024*1024)))),
	}
	netBlock := netStyle.Render(strings.Join(netLines, "\n"))

	leftCol := lipgloss.JoinVertical(lipgloss.Left, memBlock, diskBlock, netBlock)

	// Right column: top processes
	procStyle := m.panelStyle(panelProcesses).Width(rightW).Height(h)
	visibleRows := max(3, h-4)
	var procLines []string
	if len(procs) > 0 {
		procLines = append(procLines, panelCaption("top processes", "", m.selectedPanel == panelProcesses))
		for i := 0; i < min(visibleRows, len(procs)) && i < 15; i++ {
			p := procs[i]
			pidStr := col(fmt.Sprintf("%5d", p.PID), titleMuted)
			nameStr := shorten(p.Name, 15)
			cpuStr := col(fmt.Sprintf("%5.1f%%", p.CPUPercent), cyan)
			memStr := col(fmt.Sprintf("%4.1f%%", p.MemPercent), green)
			procLines = append(procLines, fmt.Sprintf("%s  %-15s  %s  %s", pidStr, nameStr, cpuStr, memStr))
		}
	} else {
		procLines = []string{col("no processes", dim)}
	}

	procBlock := procStyle.Render(strings.Join(procLines, "\n"))

	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", procBlock)
}

// GetFilteredProcessesRemote returns processes for the selected server in remote mode.
func (m *Model) GetFilteredProcessesRemote() []types.ProcessInfo {
	ss := m.CurrentServer()
	if ss == nil {
		return nil
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if m.searchText == "" {
		return ss.processes
	}
	var filtered []types.ProcessInfo
	for _, p := range ss.processes {
		if containsIgnoreCase(p.Name, m.searchText) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
