package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	baseGraphWidth = 60 // Base width for 30s scale
	clearScreen = "\033[2J\033[H"
	moveCursor = "\033[0;0H"
	hideCursor = "\033[?25l"
	showCursor = "\033[?25h"
	
	// Colors
	colorReset   = "\033[0m"
	colorRed     = "\033[0;31m"
	colorBrightRed = "\033[1;31m"
	colorGreen   = "\033[0;32m"
	colorYellow  = "\033[1;33m"
	colorDarkYellow = "\033[0;33m"
	colorBlue    = "\033[0;34m"
	colorDarkBlue = "\033[38;5;17m"
	colorLightBlue = "\033[38;5;39m"
	colorMagenta = "\033[0;35m"
	colorCyan    = "\033[0;36m"
	colorOrange  = "\033[38;5;208m"
)

type CPUStats struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
	irq    uint64
	soft   uint64
	steal  uint64
}

type Monitor struct {
	stressCmd       *exec.Cmd
	stressRunning   bool
	stressAvailable bool
	cores           int
	minTemp        float64
	maxTemp        float64
	cpuTempHistory []struct{cpu, temp float64} // Combined CPU usage and temperature history
	lastCPUStats   []CPUStats
	oldTermState   *term.State
	
	// Display mode
	showHelp           bool         // Toggle between main view and help page
	
	// Time scale functionality
	currentTimeScale   int          // Index into timeScales array
	timeScales         []struct{name string; seconds int; width int; updateInterval int}
	pollCounter        int          // Counter for polls since start
	displayBuffer      []struct{cpu, temp float64} // Fixed display buffer for stable rendering
	
	// Smooth animation fields
	currentCoreUsages  []float64    // Current displayed values
	coreSampleBuffer   [][]float64  // Rolling buffer of samples for each core
	sampleBufferSize   int          // Number of samples to keep
	lastPollTime       time.Time    // When we last polled CPU stats
	lastRenderTime     time.Time    // When we last rendered the display
}

func NewMonitor() *Monitor {
	cores := runtime.NumCPU()
	bufferSize := 4 // Keep 4 samples for averaging
	
	// Define time scales: 30s, 60s, 5min, 30min
	// updateInterval: how many polls before updating graph (1=every poll, 2=every other poll, etc.)
	timeScales := []struct{name string; seconds int; width int; updateInterval int}{
		{"30s", 30, baseGraphWidth, 1},        // Update every poll (500ms)
		{"60s", 60, baseGraphWidth * 2, 2},    // Update every 2 polls (1s)
		{"5min", 300, baseGraphWidth * 10, 10}, // Update every 10 polls (5s)
		{"30min", 1800, baseGraphWidth * 60, 60}, // Update every 60 polls (30s)
	}
	
	m := &Monitor{
		cores:             cores,
		minTemp:           999.0,
		maxTemp:           0.0,
		showHelp:          false, // Start with main view
		timeScales:        timeScales,
		currentTimeScale:  0, // Start with 30s
		cpuTempHistory:    make([]struct{cpu, temp float64}, timeScales[0].width),
		pollCounter:       0,
		displayBuffer:     make([]struct{cpu, temp float64}, baseGraphWidth),
		lastCPUStats:      make([]CPUStats, cores+1), // +1 for total CPU
		currentCoreUsages: make([]float64, cores),
		coreSampleBuffer:  make([][]float64, cores),
		sampleBufferSize:  bufferSize,
		lastPollTime:      time.Now(),
		lastRenderTime:    time.Now(),
	}
	
	// Initialize sample buffers for each core
	for i := 0; i < cores; i++ {
		m.coreSampleBuffer[i] = make([]float64, 0, bufferSize)
	}
	
	// Initialize CPU stats
	m.getCPUStats()
	
	// Check if stress command is available
	m.stressAvailable = m.checkStressAvailable()
	
	return m
}

func (m *Monitor) checkStressAvailable() bool {
	_, err := exec.LookPath("stress")
	return err == nil
}

func (m *Monitor) cleanup() {
	if m.stressRunning {
		m.stopStress()
	}
	if m.oldTermState != nil {
		term.Restore(int(os.Stdin.Fd()), m.oldTermState)
	}
	fmt.Print(showCursor)
	fmt.Printf("\n%sExiting...%s\r\n", colorRed, colorReset)
}

func (m *Monitor) startStress() {
	if !m.stressRunning && m.stressAvailable {
		m.stressCmd = exec.Command("stress", "--cpu", strconv.Itoa(m.cores))
		err := m.stressCmd.Start()
		if err == nil {
			m.stressRunning = true
		}
	}
}

func (m *Monitor) stopStress() {
	if m.stressRunning && m.stressCmd != nil {
		m.stressCmd.Process.Kill()
		m.stressRunning = false
		m.stressCmd = nil
	}
}

func (m *Monitor) getCPUStats() []CPUStats {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return m.lastCPUStats
	}
	defer file.Close()

	stats := make([]CPUStats, m.cores+1)
	scanner := bufio.NewScanner(file)
	cpuIndex := 0

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu") {
			break
		}

		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		// Skip the "cpu" label and parse values
		for i := 1; i <= 7 && i < len(fields); i++ {
			val, _ := strconv.ParseUint(fields[i], 10, 64)
			switch i {
			case 1:
				stats[cpuIndex].user = val
			case 2:
				stats[cpuIndex].nice = val
			case 3:
				stats[cpuIndex].system = val
			case 4:
				stats[cpuIndex].idle = val
			case 5:
				stats[cpuIndex].iowait = val
			case 6:
				stats[cpuIndex].irq = val
			case 7:
				stats[cpuIndex].soft = val
			}
		}
		
		cpuIndex++
		if cpuIndex > m.cores {
			break
		}
	}

	return stats
}

func (m *Monitor) calculateCPUUsage() (float64, []float64) {
	currentStats := m.getCPUStats()
	defer func() { m.lastCPUStats = currentStats }()

	coreUsages := make([]float64, m.cores)
	
	// Calculate total CPU usage
	totalUsage := m.calculateSingleCPUUsage(m.lastCPUStats[0], currentStats[0])
	
	// Calculate per-core usage
	for i := 0; i < m.cores; i++ {
		coreUsages[i] = m.calculateSingleCPUUsage(m.lastCPUStats[i+1], currentStats[i+1])
	}
	
	return totalUsage, coreUsages
}

func (m *Monitor) calculateSingleCPUUsage(prev, curr CPUStats) float64 {
	prevIdle := prev.idle + prev.iowait
	currIdle := curr.idle + curr.iowait
	
	prevNonIdle := prev.user + prev.nice + prev.system + prev.irq + prev.soft + prev.steal
	currNonIdle := curr.user + curr.nice + curr.system + curr.irq + curr.soft + curr.steal
	
	prevTotal := prevIdle + prevNonIdle
	currTotal := currIdle + currNonIdle
	
	totalDiff := float64(currTotal - prevTotal)
	idleDiff := float64(currIdle - prevIdle)
	
	if totalDiff == 0 {
		return 0
	}
	
	usage := ((totalDiff - idleDiff) / totalDiff) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	
	return usage
}

func (m *Monitor) getTemperature() float64 {
	// Try k10temp using sensors command first (most accurate for AMD)
	output, err := exec.Command("sensors", "k10temp-pci-00c3").Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Tctl:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					tempStr := strings.TrimSuffix(strings.TrimPrefix(fields[1], "+"), "°C")
					temp, err := strconv.ParseFloat(tempStr, 64)
					if err == nil {
						return temp
					}
				}
			}
		}
	}
	
	// Fallback to sys sensors if k10temp fails
	sensors := []string{
		"/sys/class/hwmon/hwmon0/temp1_input",
		"/sys/class/hwmon/hwmon1/temp1_input",
		"/sys/class/hwmon/hwmon2/temp1_input",
		"/sys/class/hwmon/hwmon3/temp1_input",
		"/sys/class/hwmon/hwmon4/temp1_input",
		"/sys/class/thermal/thermal_zone0/temp",
	}
	
	for _, sensor := range sensors {
		data, err := ioutil.ReadFile(sensor)
		if err == nil {
			temp, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err == nil {
				// Convert from millidegrees to degrees
				return temp / 1000.0
			}
		}
	}
	
	return 0
}

func (m *Monitor) updateMinMax(temp float64) {
	if temp < m.minTemp && temp > 0 {
		m.minTemp = temp
	}
	if temp > m.maxTemp {
		m.maxTemp = temp
	}
}

func (m *Monitor) shiftCpuTempHistory(newCpu, newTemp float64) {
	copy(m.cpuTempHistory[0:], m.cpuTempHistory[1:])
	m.cpuTempHistory[len(m.cpuTempHistory)-1] = struct{cpu, temp float64}{newCpu, newTemp}
}

func (m *Monitor) resizeHistory() {
	currentWidth := m.timeScales[m.currentTimeScale].width
	if len(m.cpuTempHistory) != currentWidth {
		// Create new history array with current width
		newHistory := make([]struct{cpu, temp float64}, currentWidth)
		
		// Copy existing data, truncating or padding as needed
		if len(m.cpuTempHistory) > currentWidth {
			// Truncate from the left (keep most recent data)
			copy(newHistory, m.cpuTempHistory[len(m.cpuTempHistory)-currentWidth:])
		} else {
			// Pad with zeros on the left, copy existing data to the right
			copy(newHistory[currentWidth-len(m.cpuTempHistory):], m.cpuTempHistory)
		}
		
		m.cpuTempHistory = newHistory
		// Rebuild display buffer when time scale changes
		m.rebuildDisplayBuffer()
	}
}

func (m *Monitor) rebuildDisplayBuffer() {
	// Sample from history to create stable display buffer
	historyLen := len(m.cpuTempHistory)
	for i := 0; i < baseGraphWidth; i++ {
		historyIndex := (i * (historyLen - 1)) / (baseGraphWidth - 1)
		if historyIndex >= historyLen {
			historyIndex = historyLen - 1
		}
		if historyIndex < 0 {
			historyIndex = 0
		}
		m.displayBuffer[i] = m.cpuTempHistory[historyIndex]
	}
}

func (m *Monitor) updateDisplayBuffer(newCpu, newTemp float64) {
	// Shift display buffer left and add new value
	copy(m.displayBuffer[0:], m.displayBuffer[1:])
	m.displayBuffer[len(m.displayBuffer)-1] = struct{cpu, temp float64}{newCpu, newTemp}
}

func interpolateColor(val, min, max float64, r1, g1, b1, r2, g2, b2 int) (int, int, int) {
	// Normalize value between 0 and 1
	t := (val - min) / (max - min)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	
	// Linear interpolation between colors
	r := int(float64(r1) + t*float64(r2-r1))
	g := int(float64(g1) + t*float64(g2-g1))
	b := int(float64(b1) + t*float64(b2-b1))
	
	return r, g, b
}

func getTempColor(temp float64) string {
	// Color gradient based on temperature (Celsius)
	// Cool (35-45°C) -> Warm (50-70°C) -> Hot (75-85°C) -> Critical (90°C+)
	type colorStop struct {
		temp    float64
		r, g, b int
	}
	
	stops := []colorStop{
		{35,   0,   0, 255},  // Blue - Cool
		{40,   0, 128, 255},  // Light blue
		{45,   0, 255, 255},  // Cyan
		{50,   0, 255,   0},  // Green - Normal
		{60, 128, 255,   0},  // Yellow-green
		{65, 255, 255,   0},  // Yellow - Warm
		{70, 255, 192,   0},  // Orange-yellow
		{75, 255, 128,   0},  // Orange - Hot
		{80, 255,  64,   0},  // Dark orange
		{85, 255,   0,   0},  // Red - Very Hot
		{90, 255,   0,  64},  // Bright red
		{95, 255,   0, 128},  // Magenta - Critical
		{100, 255,  0, 255}, // Purple - Extreme
	}
	
	// Find which two stops we're between
	var lower, upper colorStop
	for i := 0; i < len(stops)-1; i++ {
		if temp >= stops[i].temp && temp <= stops[i+1].temp {
			lower = stops[i]
			upper = stops[i+1]
			break
		}
	}
	
	// Handle edge cases
	if temp <= stops[0].temp {
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", stops[0].r, stops[0].g, stops[0].b)
	}
	if temp >= stops[len(stops)-1].temp {
		last := stops[len(stops)-1]
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", last.r, last.g, last.b)
	}
	
	// Interpolate between the two stops
	r, g, b := interpolateColor(temp, lower.temp, upper.temp,
		lower.r, lower.g, lower.b,
		upper.r, upper.g, upper.b)
	
	// Return 24-bit true color ANSI escape code
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

func getUsageColor(usage float64) string {
	// Define color gradient stops (usage%, r, g, b)
	// Using a blue -> cyan -> green -> yellow -> orange -> red gradient
	type colorStop struct {
		percent float64
		r, g, b int
	}
	
	stops := []colorStop{
		{0,    0,   0,  128},  // Dark blue
		{10,   0,   0,  255},  // Blue
		{20,   0, 128,  255},  // Light blue
		{30,   0, 255,  255},  // Cyan
		{40,   0, 255,    0},  // Green
		{50, 128, 255,    0},  // Yellow-green
		{60, 255, 255,    0},  // Yellow
		{70, 255, 165,    0},  // Orange
		{80, 255,  64,    0},  // Dark orange
		{90, 255,   0,    0},  // Red
		{100, 255,  0,    0},  // Bright red
	}
	
	// Find which two stops we're between
	var lower, upper colorStop
	for i := 0; i < len(stops)-1; i++ {
		if usage >= stops[i].percent && usage <= stops[i+1].percent {
			lower = stops[i]
			upper = stops[i+1]
			break
		}
	}
	
	// Handle edge cases
	if usage <= stops[0].percent {
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", stops[0].r, stops[0].g, stops[0].b)
	}
	if usage >= stops[len(stops)-1].percent {
		last := stops[len(stops)-1]
		return fmt.Sprintf("\033[38;2;%d;%d;%dm", last.r, last.g, last.b)
	}
	
	// Interpolate between the two stops
	r, g, b := interpolateColor(usage, lower.percent, upper.percent,
		lower.r, lower.g, lower.b,
		upper.r, upper.g, upper.b)
	
	// Return 24-bit true color ANSI escape code
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

func getGridDimensions(count int) (cols, rows int) {
	switch {
	case count <= 4:
		return 2, (count + 1) / 2
	case count <= 6:
		return 3, 2
	case count <= 8:
		return 4, 2
	case count <= 12:
		return 4, 3
	case count <= 16:
		return 4, 4
	case count <= 20:
		return 5, 4
	case count <= 25:
		return 5, 5
	case count <= 30:
		return 6, 5
	case count <= 36:
		return 6, 6
	default:
		cols = int(math.Sqrt(float64(count))) + 1
		return cols, cols
	}
}

func (m *Monitor) updateSampleBuffer(newSamples []float64) {
	// Add new samples to buffer and maintain rolling window
	for i := 0; i < m.cores; i++ {
		if len(m.coreSampleBuffer[i]) >= m.sampleBufferSize {
			// Remove oldest sample
			m.coreSampleBuffer[i] = m.coreSampleBuffer[i][1:]
		}
		// Add new sample
		m.coreSampleBuffer[i] = append(m.coreSampleBuffer[i], newSamples[i])
	}
}

func (m *Monitor) calculateRollingAverage() []float64 {
	avg := make([]float64, m.cores)
	for i := 0; i < m.cores; i++ {
		if len(m.coreSampleBuffer[i]) == 0 {
			avg[i] = 0
			continue
		}
		
		// Calculate weighted average with more weight on recent samples
		sum := 0.0
		totalWeight := 0.0
		for j, sample := range m.coreSampleBuffer[i] {
			// Linear weighting: older samples have less weight
			weight := float64(j + 1)
			sum += sample * weight
			totalWeight += weight
		}
		
		if totalWeight > 0 {
			avg[i] = sum / totalWeight
		}
		
		// Clamp values
		if avg[i] < 0 {
			avg[i] = 0
		}
		if avg[i] > 100 {
			avg[i] = 100
		}
	}
	return avg
}

func (m *Monitor) interpolateCoreUsages() []float64 {
	// Calculate smooth interpolation towards rolling average
	targetValues := m.calculateRollingAverage()
	
	// Smooth transition rate (adjust for desired smoothness)
	smoothingFactor := 0.08 // Lower = smoother, higher = more responsive
	
	// Continuously move towards target
	for i := 0; i < m.cores; i++ {
		diff := targetValues[i] - m.currentCoreUsages[i]
		
		// Apply smoothing
		m.currentCoreUsages[i] += diff * smoothingFactor
		
		// Clamp values
		if m.currentCoreUsages[i] < 0 {
			m.currentCoreUsages[i] = 0
		}
		if m.currentCoreUsages[i] > 100 {
			m.currentCoreUsages[i] = 100
		}
	}
	
	return m.currentCoreUsages
}

func (m *Monitor) displayHelpPage() {
	fmt.Printf("%s=== Kode Kronical Perf Monitor - Help ===%s\r\n\r\n", colorGreen, colorReset)
	
	fmt.Printf("%sControls:%s\r\n", colorCyan, colorReset)
	if m.stressAvailable {
		fmt.Printf("  %sSPACE%s  - Toggle stress test ON/OFF\r\n", colorYellow, colorReset)
	} else {
		fmt.Printf("  %sSPACE%s  - Toggle stress test (stress command not available)\r\n", colorDarkYellow, colorReset)
	}
	fmt.Printf("  %sW%s      - Zoom in (shorter time scale)\r\n", colorYellow, colorReset)
	fmt.Printf("  %sS%s      - Zoom out (longer time scale)\r\n", colorYellow, colorReset)
	fmt.Printf("  %sH%s      - Toggle this help page\r\n", colorYellow, colorReset)
	fmt.Printf("  %sESC/Q%s  - Exit help or quit application\r\n", colorYellow, colorReset)
	fmt.Printf("  %sCtrl+C%s - Quit application\r\n\r\n", colorYellow, colorReset)
	
	fmt.Printf("%sTime Scales:%s\r\n", colorCyan, colorReset)
	fmt.Printf("  30s    - 30 seconds (updates every 500ms)\r\n")
	fmt.Printf("  60s    - 1 minute (updates every 1s)\r\n")
	fmt.Printf("  5min   - 5 minutes (updates every 5s)\r\n")
	fmt.Printf("  30min  - 30 minutes (updates every 30s)\r\n\r\n")
	
	fmt.Printf("%sCPU Core Bars:%s\r\n", colorCyan, colorReset)
	fmt.Printf("  Height - CPU usage (0-100%%)\r\n")
	fmt.Printf("  Color  - Estimated core temperature\r\n")
	fmt.Printf("  Bars:  - ▁▂▃▄▅▆▇█ (0%% to 100%%)\r\n\r\n")
	
	fmt.Printf("%sGraph Display:%s\r\n", colorCyan, colorReset)
	fmt.Printf("  Height - CPU usage percentage\r\n")
	fmt.Printf("  Color  - Temperature at that time\r\n")
	fmt.Printf("  Shows  - Combined CPU usage and temperature history\r\n\r\n")
	
	fmt.Printf("%sTemperature Legend:%s\r\n", colorCyan, colorReset)
	
	// Show temperature ranges with their colors
	tempRanges := []struct {
		temp  float64
		label string
	}{
		{40, "Cool"},
		{50, "Normal"},
		{65, "Warm"},
		{75, "Hot"},
		{85, "Very Hot"},
		{95, "Critical"},
	}
	
	// First line: color blocks and labels
	for i, tempRange := range tempRanges {
		color := getTempColor(tempRange.temp)
		fmt.Printf("%s█%s%s", color, colorReset, tempRange.label)
		if i < len(tempRanges)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Print("\r\n")
	
	// Second line: temperature values aligned under color blocks
	for i, tempRange := range tempRanges {
		tempStr := fmt.Sprintf("%.0fC", tempRange.temp)
		fmt.Print(tempStr)
		if i < len(tempRanges)-1 {
			// Calculate spacing to align next temp under next color block
			labelLen := len(tempRanges[i].label)
			spacing := labelLen + 2 - len(tempStr) // +2 for "█" and space between entries
			for j := 0; j < spacing; j++ {
				fmt.Print(" ")
			}
		}
	}
	fmt.Printf("\r\n\r\n%sPress H, ESC, or Q to return to main view%s\r\n", colorYellow, colorReset)
}

func (m *Monitor) displayTemperatureLegend() {
	fmt.Printf("%sTemperature Legend:%s\r\n", colorCyan, colorReset)
	
	// Show temperature ranges with their colors
	tempRanges := []struct {
		temp  float64
		label string
	}{
		{40, "Cool"},
		{50, "Normal"},
		{65, "Warm"},
		{75, "Hot"},
		{85, "Very Hot"},
		{95, "Critical"},
	}
	
	// First line: color blocks and labels
	for i, tempRange := range tempRanges {
		color := getTempColor(tempRange.temp)
		fmt.Printf("%s█%s%s", color, colorReset, tempRange.label)
		if i < len(tempRanges)-1 {
			fmt.Print(" ")
		}
	}
	fmt.Print("\r\n")
	
	// Second line: temperature values aligned under color blocks
	for i, tempRange := range tempRanges {
		tempStr := fmt.Sprintf("%.0fC", tempRange.temp)
		fmt.Print(tempStr)
		if i < len(tempRanges)-1 {
			// Calculate spacing to align next temp under next color block
			labelLen := len(tempRanges[i].label)
			spacing := labelLen + 2 - len(tempStr) // +2 for "█" and space between entries
			for j := 0; j < spacing; j++ {
				fmt.Print(" ")
			}
		}
	}
	fmt.Print("\r\n\r\n")
}

func (m *Monitor) displayCPUCores(coreUsages []float64, currentTemp float64) {
	cols, rows := getGridDimensions(m.cores)
	
	fmt.Printf("%sCPU Cores (%d cores):%s\r\n", colorCyan, m.cores, colorReset)
	
	// Bar characters for different heights (8 levels + space)
	barChars := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	
	for row := 0; row < rows; row++ {
		rowStart := row * cols
		
		// Display single-line bars
		fmt.Print("  ")
		for col := 0; col < cols; col++ {
			idx := rowStart + col
			if idx < m.cores {
				usage := coreUsages[idx]
				
				// Estimate core temp based on usage and package temp
				// Higher usage = higher temp offset from baseline
				baseTemp := currentTemp - 5 // Assume idle cores are 5°C below package
				tempOffset := (usage / 100.0) * 15 // Up to 15°C rise at 100% usage
				estimatedTemp := baseTemp + tempOffset
				
				// Get color based on estimated temperature
				color := getTempColor(estimatedTemp)
				
				// Map usage (0-100%) to bar character (1-8, minimum ▁)
				barIndex := int(usage / 12.5) // 100% / 8 = 12.5% per bar level
				if barIndex > 8 {
					barIndex = 8
				}
				if barIndex < 1 {
					barIndex = 1 // Always show at least ▁
				}
				
				// Display colored bar character
				fmt.Printf("%s%s%s", color, barChars[barIndex], colorReset)
			} else {
				fmt.Print(" ")
			}
			
			if col < cols-1 {
				fmt.Print(" ")
			}
		}
		fmt.Print("\r\n")
		
		// Print percentages below the bars
		// fmt.Print("  ")
		// for col := 0; col < cols; col++ {
		// 	idx := rowStart + col
		// 	if idx < m.cores {
		// 		fmt.Printf("%3.0f%%", coreUsages[idx])
		// 	} else {
		// 		fmt.Print("    ")
		// 	}
		// 	if col < cols-1 {
		// 		fmt.Print(" ")
		// 	}
		// }
		// fmt.Print("\r\n")
	}
	
	fmt.Print("\r\n") // Extra line before temperature legend
	// Display temperature legend
	m.displayTemperatureLegend()
}

func (m *Monitor) drawCombinedGraph(currentCpu, currentTemp float64) {
	currentScale := m.timeScales[m.currentTimeScale]
	fmt.Printf("%sCPU Usage & Temperature Graph%s Current: %s%.1f%%%s / %s%.1f°C%s%*s\r\n", 
		colorCyan, colorReset, colorYellow, currentCpu, colorReset, colorYellow, currentTemp, colorReset, 20, "")
	
	// Draw 5 rows
	ranges := []string{"81-100%", "61-80% ", "41-60% ", "21-40% ", "0-20%  "}
	
	for row := 4; row >= 0; row-- {
		fmt.Printf("%s%s%s", colorCyan, ranges[4-row], colorReset)
		
		// Use stable display buffer - no recalculation!
		for i := 0; i < baseGraphWidth; i++ {
			cpuVal := m.displayBuffer[i].cpu
			tempVal := m.displayBuffer[i].temp
			
			// Determine if this height level should show a block based on CPU usage
			shouldDraw := false
			switch row {
			case 4:
				shouldDraw = cpuVal > 80
			case 3:
				shouldDraw = cpuVal > 60 && cpuVal <= 80
			case 2:
				shouldDraw = cpuVal > 40 && cpuVal <= 60
			case 1:
				shouldDraw = cpuVal > 20 && cpuVal <= 40
			case 0:
				shouldDraw = cpuVal >= 0 && cpuVal <= 20
			}
			
			if shouldDraw {
				// Color the block based on temperature
				tempColor := getTempColor(tempVal)
				fmt.Printf("%s█%s", tempColor, colorReset)
			} else {
				fmt.Print(" ")
			}
		}
		fmt.Print("\r\n")
	}
	
	fmt.Printf("        %sPress W to zoom in, S to zoom out%s\r\n", colorYellow, colorReset)
	fmt.Printf("        %s%-10s%s\r\n", colorCyan, currentScale.name, colorReset)
}

func (m *Monitor) run() {
	// Setup terminal
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}
	m.oldTermState = oldState
	
	// Clear screen and hide cursor
	fmt.Print(clearScreen)
	fmt.Print(hideCursor)
	
	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	// Input channel
	inputChan := make(chan byte, 1)
	go func() {
		for {
			var b [1]byte
			os.Stdin.Read(b[:])
			inputChan <- b[0]
		}
	}()
	
	// Separate tickers for polling (500ms for frequent sampling) and rendering (60fps)
	pollTicker := time.NewTicker(500 * time.Millisecond)
	renderTicker := time.NewTicker(16 * time.Millisecond) // ~60fps
	defer pollTicker.Stop()
	defer renderTicker.Stop()
	
	// Variables for current display values
	var currentTemp float64
	var currentTotalUsage float64
	
	for {
		select {
		case <-sigChan:
			return
			
		case key := <-inputChan:
			if m.showHelp {
				// In help mode, H/ESC/Q return to main view
				if key == 'h' || key == 'H' || key == 27 || key == 'q' || key == 'Q' { // 27 is ESC
					m.showHelp = false
					fmt.Print(clearScreen) // Clear screen when returning to main view
				}
				if key == 3 { // Ctrl+C still exits
					return
				}
			} else {
				// In main mode
				if key == ' ' {
					if m.stressRunning {
						m.stopStress()
					} else {
						m.startStress()
					}
				} else if key == 'w' || key == 'W' {
					// Zoom in (shorter time scale)
					if m.currentTimeScale > 0 {
						m.currentTimeScale--
						m.pollCounter = 0 // Reset counter to avoid phase issues
						m.resizeHistory()
					}
				} else if key == 's' || key == 'S' {
					// Zoom out (longer time scale)
					if m.currentTimeScale < len(m.timeScales)-1 {
						m.currentTimeScale++
						m.pollCounter = 0 // Reset counter to avoid phase issues
						m.resizeHistory()
					}
				} else if key == 'h' || key == 'H' {
					// Show help page
					m.showHelp = true
					fmt.Print(clearScreen) // Clear screen when showing help page
				} else if key == 'q' || key == 3 { // 3 is Ctrl+C
					return
				}
			}
			
		case <-pollTicker.C:
			// Poll for new CPU data frequently for smooth averaging
			currentTemp = m.getTemperature()
			_, newCoreUsages := m.calculateCPUUsage()
			
			// Update sample buffer with new readings
			m.updateSampleBuffer(newCoreUsages)
			m.lastPollTime = time.Now()
			m.pollCounter++
			
			// Update history with rolling average for smoother graph
			if currentTemp > 0 {
				m.updateMinMax(currentTemp)
			}
			
			// Use rolling average for total CPU history and combine with temp
			avgCores := m.calculateRollingAverage()
			avgTotal := 0.0
			for _, core := range avgCores {
				avgTotal += core
			}
			currentTotalUsage = avgTotal / float64(len(avgCores))
			
			// Only update graph history and display at the appropriate interval for current time scale
			currentScale := m.timeScales[m.currentTimeScale]
			if m.pollCounter%currentScale.updateInterval == 0 {
				m.shiftCpuTempHistory(currentTotalUsage, currentTemp)
				m.updateDisplayBuffer(currentTotalUsage, currentTemp)
			}
			
		case <-renderTicker.C:
			// Render at 60fps with continuously interpolated values
			now := time.Now()
			
			// Get smoothly interpolated core usages
			interpolatedCores := m.interpolateCoreUsages()
			
			// Display
			fmt.Print(moveCursor)
			
			if m.showHelp {
				// Show help page
				m.displayHelpPage()
			} else {
				// Show main monitoring view with minimal instructions
				fmt.Printf("%s=== Kode Kronical Perf Monitor ===%s  %sPress H for help%s\r\n", colorGreen, colorReset, colorYellow, colorReset)
			
				var status string
				if !m.stressAvailable {
					status = fmt.Sprintf("%s[STRESS N/A]%s", colorDarkYellow, colorReset)
				} else if m.stressRunning {
					status = fmt.Sprintf("%s[STRESS ON]%s", colorRed, colorReset)
				} else {
					status = fmt.Sprintf("%s[STRESS OFF]%s", colorGreen, colorReset)
				}
				
				veryHotColor := getTempColor(85.0) // Same color as "Very Hot" in temperature legend
				fmt.Printf("Status: %s  %sCurrent:%s %s%.1f°C%s  %sMin:%s %s%.1f°C%s  %sMax:%s %s%.1f°C%s\r\n\r\n",
					status,
					colorBlue, colorReset, colorYellow, currentTemp, colorReset,
					colorBlue, colorReset, colorGreen, m.minTemp, colorReset,
					colorBlue, colorReset, veryHotColor, m.maxTemp, colorReset)
				
				// Display CPU cores with smooth interpolation and temperature colors
				m.displayCPUCores(interpolatedCores, currentTemp)
				
				// Draw combined graph
				m.drawCombinedGraph(currentTotalUsage, currentTemp)
			}
			
			m.lastRenderTime = now
		}
	}
}

func main() {
	monitor := NewMonitor()
	defer monitor.cleanup()
	monitor.run()
}