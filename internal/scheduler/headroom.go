package scheduler

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HeadroomConfig configures pre-spawn resource headroom guardrails.
type HeadroomConfig struct {
	// Enabled toggles headroom checking.
	Enabled bool `json:"enabled"`

	// Threshold is the usage percentage (0.0-1.0) above which spawns are blocked.
	// Default: 0.75 (75%)
	Threshold float64 `json:"threshold"`

	// WarnThreshold is the usage percentage (0.0-1.0) at which warnings are logged.
	// Default: 0.70 (70%)
	WarnThreshold float64 `json:"warn_threshold"`

	// RecheckInterval is how often to recheck resource headroom when blocked.
	// Default: 5s
	RecheckInterval time.Duration `json:"recheck_interval"`

	// MinHeadroom is the minimum number of free process slots required.
	// Default: 50
	MinHeadroom int `json:"min_headroom"`

	// CacheTimeout is how long to cache headroom measurements.
	// Default: 2s
	CacheTimeout time.Duration `json:"cache_timeout"`
}

// DefaultHeadroomConfig returns sensible default configuration.
func DefaultHeadroomConfig() HeadroomConfig {
	return HeadroomConfig{
		Enabled:         true,
		Threshold:       0.75,
		WarnThreshold:   0.70,
		RecheckInterval: 5 * time.Second,
		MinHeadroom:     50,
		CacheTimeout:    2 * time.Second,
	}
}

// HeadroomGuard checks system resource headroom before allowing spawns.
type HeadroomGuard struct {
	mu sync.Mutex

	config HeadroomConfig

	// blocked indicates if spawns are currently blocked due to resource constraints.
	blocked bool

	// blockReason contains the reason for blocking.
	blockReason string

	// lastCheck is when headroom was last checked.
	lastCheck time.Time

	// cachedLimits caches the computed resource limits.
	cachedLimits *ResourceLimits

	// cachedUsage caches the current resource usage.
	cachedUsage *ResourceUsage

	// callbacks for state changes
	onBlocked   func(reason string, limits *ResourceLimits, usage *ResourceUsage)
	onUnblocked func()
	onWarning   func(reason string, limits *ResourceLimits, usage *ResourceUsage)

	// recheckTicker handles periodic rechecking when blocked.
	recheckTicker     *time.Ticker
	recheckTickerStop chan struct{} // per-ticker stop signal
	recheckStop       chan struct{} // permanent stop for Stop()
	stopped           bool
}

// ResourceLimits holds the effective resource limits from various sources.
type ResourceLimits struct {
	// UlimitNproc is the user's process limit from ulimit -u.
	UlimitNproc int `json:"ulimit_nproc"`

	// CgroupPidsMax is the cgroup v2 pids.max limit (0 if not available).
	CgroupPidsMax int `json:"cgroup_pids_max"`

	// SystemdTasksMax is the systemd TasksMax limit (0 if not available).
	SystemdTasksMax int `json:"systemd_tasks_max"`

	// KernelPidMax is the kernel's pid_max from /proc/sys/kernel/pid_max.
	KernelPidMax int `json:"kernel_pid_max"`

	// EffectiveLimit is min(all non-zero limits).
	EffectiveLimit int `json:"effective_limit"`

	// Source indicates which limit is the effective one.
	Source string `json:"source"`
}

// ResourceUsage holds the current resource usage.
type ResourceUsage struct {
	// CgroupPidsCurrent is the cgroup v2 pids.current (0 if not available).
	CgroupPidsCurrent int `json:"cgroup_pids_current"`

	// ProcessCount is the number of processes owned by the current user.
	ProcessCount int `json:"process_count"`

	// EffectiveUsage is the best estimate of current usage.
	EffectiveUsage int `json:"effective_usage"`

	// Source indicates which usage metric is being used.
	Source string `json:"source"`
}

// HeadroomStatus represents the current headroom status.
type HeadroomStatus struct {
	// Available is the number of free process slots.
	Available int `json:"available"`

	// UsagePercent is the current usage as a percentage (0.0-1.0).
	UsagePercent float64 `json:"usage_percent"`

	// Blocked indicates if spawns are blocked.
	Blocked bool `json:"blocked"`

	// BlockReason is the reason for blocking (empty if not blocked).
	BlockReason string `json:"block_reason,omitempty"`

	// Limits contains the detected resource limits.
	Limits *ResourceLimits `json:"limits"`

	// Usage contains the current resource usage.
	Usage *ResourceUsage `json:"usage"`

	// LastCheck is when headroom was last checked.
	LastCheck time.Time `json:"last_check"`
}

// NewHeadroomGuard creates a new headroom guard.
func NewHeadroomGuard(cfg HeadroomConfig) *HeadroomGuard {
	return &HeadroomGuard{
		config:      cfg,
		recheckStop: make(chan struct{}),
	}
}

// SetCallbacks sets the callbacks for state changes.
func (h *HeadroomGuard) SetCallbacks(
	onBlocked func(reason string, limits *ResourceLimits, usage *ResourceUsage),
	onUnblocked func(),
	onWarning func(reason string, limits *ResourceLimits, usage *ResourceUsage),
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onBlocked = onBlocked
	h.onUnblocked = onUnblocked
	h.onWarning = onWarning
}

// CheckHeadroom checks if there is sufficient resource headroom for spawning.
// Returns true if spawning is allowed, false otherwise.
func (h *HeadroomGuard) CheckHeadroom() (bool, string) {
	if !h.config.Enabled {
		return true, ""
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if we can use cached values
	if time.Since(h.lastCheck) < h.config.CacheTimeout && h.cachedLimits != nil && h.cachedUsage != nil {
		return h.evaluateCached()
	}

	// Get fresh measurements
	limits := h.detectLimits()
	usage := h.detectUsage()

	h.cachedLimits = limits
	h.cachedUsage = usage
	h.lastCheck = time.Now()

	return h.evaluate(limits, usage)
}

// evaluateCached evaluates headroom using cached values.
func (h *HeadroomGuard) evaluateCached() (bool, string) {
	return h.evaluate(h.cachedLimits, h.cachedUsage)
}

// evaluate checks headroom given limits and usage.
func (h *HeadroomGuard) evaluate(limits *ResourceLimits, usage *ResourceUsage) (bool, string) {
	if limits.EffectiveLimit == 0 {
		// No limits detected, allow spawning
		return true, ""
	}

	available := limits.EffectiveLimit - usage.EffectiveUsage
	usagePercent := float64(usage.EffectiveUsage) / float64(limits.EffectiveLimit)

	// Check if we're above the blocking threshold
	if usagePercent >= h.config.Threshold {
		reason := fmt.Sprintf(
			"resource headroom exhausted: %.1f%% usage (%d/%d processes, source: %s/%s)",
			usagePercent*100, usage.EffectiveUsage, limits.EffectiveLimit,
			usage.Source, limits.Source,
		)
		h.setBlocked(true, reason, limits, usage)
		return false, reason
	}

	// Check minimum headroom
	if available < h.config.MinHeadroom {
		reason := fmt.Sprintf(
			"insufficient headroom: only %d free slots (need %d), %d/%d processes",
			available, h.config.MinHeadroom, usage.EffectiveUsage, limits.EffectiveLimit,
		)
		h.setBlocked(true, reason, limits, usage)
		return false, reason
	}

	// Check warning threshold
	if usagePercent >= h.config.WarnThreshold {
		reason := fmt.Sprintf(
			"resource headroom warning: %.1f%% usage (%d/%d processes)",
			usagePercent*100, usage.EffectiveUsage, limits.EffectiveLimit,
		)
		if h.onWarning != nil {
			h.onWarning(reason, limits, usage)
		}
		slog.Warn("resource headroom warning",
			"usage_percent", usagePercent*100,
			"effective_usage", usage.EffectiveUsage,
			"effective_limit", limits.EffectiveLimit,
			"available", available,
		)
	}

	// Clear blocked state if we were blocked
	if h.blocked {
		h.setBlocked(false, "", limits, usage)
	}

	return true, ""
}

// setBlocked updates the blocked state and triggers callbacks.
func (h *HeadroomGuard) setBlocked(blocked bool, reason string, limits *ResourceLimits, usage *ResourceUsage) {
	wasBlocked := h.blocked
	h.blocked = blocked
	h.blockReason = reason

	if blocked && !wasBlocked {
		// Transitioned to blocked state
		slog.Warn("spawn blocked due to resource constraints",
			"reason", reason,
			"effective_limit", limits.EffectiveLimit,
			"effective_usage", usage.EffectiveUsage,
			"limit_source", limits.Source,
			"usage_source", usage.Source,
		)
		if h.onBlocked != nil {
			h.onBlocked(reason, limits, usage)
		}
		h.startRecheck()
	} else if !blocked && wasBlocked {
		// Transitioned to unblocked state
		slog.Info("spawn unblocked - resource headroom restored",
			"effective_limit", limits.EffectiveLimit,
			"effective_usage", usage.EffectiveUsage,
		)
		if h.onUnblocked != nil {
			h.onUnblocked()
		}
		h.stopRecheck()
	}
}

// startRecheck starts the periodic recheck ticker.
func (h *HeadroomGuard) startRecheck() {
	if h.recheckTicker != nil {
		return // Already running
	}

	interval := h.config.RecheckInterval
	if interval <= 0 {
		interval = 5 * time.Second // Default fallback
	}
	ticker := time.NewTicker(interval)
	tickerStop := make(chan struct{})
	h.recheckTicker = ticker
	h.recheckTickerStop = tickerStop
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.recheck()
			case <-tickerStop:
				return
			case <-h.recheckStop:
				return
			}
		}
	}()
}

// stopRecheck stops the periodic recheck ticker.
func (h *HeadroomGuard) stopRecheck() {
	if h.recheckTicker != nil {
		h.recheckTicker.Stop()
		h.recheckTicker = nil
		// Signal the goroutine to exit
		if h.recheckTickerStop != nil {
			close(h.recheckTickerStop)
			h.recheckTickerStop = nil
		}
	}
}

// recheck performs a headroom check during the blocked state.
func (h *HeadroomGuard) recheck() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.blocked {
		return
	}

	// Get fresh measurements
	limits := h.detectLimits()
	usage := h.detectUsage()

	h.cachedLimits = limits
	h.cachedUsage = usage
	h.lastCheck = time.Now()

	// Re-evaluate (this will unblock if conditions improve)
	h.evaluate(limits, usage)
}

// Status returns the current headroom status.
func (h *HeadroomGuard) Status() HeadroomStatus {
	if !h.config.Enabled {
		return HeadroomStatus{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Refresh if cache is stale
	if time.Since(h.lastCheck) >= h.config.CacheTimeout || h.cachedLimits == nil || h.cachedUsage == nil {
		h.cachedLimits = h.detectLimits()
		h.cachedUsage = h.detectUsage()
		h.lastCheck = time.Now()
	}

	available := 0
	usagePercent := 0.0
	if h.cachedLimits != nil && h.cachedLimits.EffectiveLimit > 0 {
		available = h.cachedLimits.EffectiveLimit - h.cachedUsage.EffectiveUsage
		usagePercent = float64(h.cachedUsage.EffectiveUsage) / float64(h.cachedLimits.EffectiveLimit)
	}

	return HeadroomStatus{
		Available:    available,
		UsagePercent: usagePercent,
		Blocked:      h.blocked,
		BlockReason:  h.blockReason,
		Limits:       h.cachedLimits,
		Usage:        h.cachedUsage,
		LastCheck:    h.lastCheck,
	}
}

// IsBlocked returns whether spawns are currently blocked.
func (h *HeadroomGuard) IsBlocked() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.blocked
}

// BlockReason returns the current block reason.
func (h *HeadroomGuard) BlockReason() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.blockReason
}

// Remediation returns guidance for resolving resource constraints.
func (h *HeadroomGuard) Remediation() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.blocked {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Resource headroom exhausted. Recommended actions:\n")
	sb.WriteString("1. Wait for existing processes to complete\n")
	sb.WriteString("2. Reduce the number of concurrent agents (--cc, --cod, --gmi flags)\n")

	if h.cachedLimits != nil {
		switch h.cachedLimits.Source {
		case "ulimit":
			sb.WriteString("3. Increase user process limit: ulimit -u <higher_value>\n")
			sb.WriteString("   Or edit /etc/security/limits.conf for persistent change\n")
		case "cgroup":
			sb.WriteString("3. Increase cgroup pids.max limit (requires root or systemd unit change)\n")
		case "systemd":
			sb.WriteString("3. Increase systemd TasksMax: systemctl edit <service> and set TasksMax=\n")
		case "kernel":
			sb.WriteString("3. Increase kernel pid_max: sysctl -w kernel.pid_max=<higher_value>\n")
		}
	}

	sb.WriteString("4. Kill unused tmux sessions: herdctl kill <session>\n")
	sb.WriteString("5. Check for zombie processes: ps aux | grep defunct\n")

	return sb.String()
}

// detectLimits detects resource limits from various sources.
func (h *HeadroomGuard) detectLimits() *ResourceLimits {
	limits := &ResourceLimits{}

	// 1. ulimit -u (user process limit)
	limits.UlimitNproc = h.getUlimitNproc()

	// 2. cgroup v2 pids.max
	limits.CgroupPidsMax = h.getCgroupPidsMax()

	// 3. systemd TasksMax (for user slice)
	limits.SystemdTasksMax = h.getSystemdTasksMax()

	// 4. kernel pid_max
	limits.KernelPidMax = h.getKernelPidMax()

	// Compute effective limit as min of all non-zero limits
	limits.EffectiveLimit, limits.Source = h.computeEffectiveLimit(limits)

	slog.Debug("detected resource limits",
		"ulimit_nproc", limits.UlimitNproc,
		"cgroup_pids_max", limits.CgroupPidsMax,
		"systemd_tasks_max", limits.SystemdTasksMax,
		"kernel_pid_max", limits.KernelPidMax,
		"effective_limit", limits.EffectiveLimit,
		"source", limits.Source,
	)

	return limits
}

// detectUsage detects current resource usage.
func (h *HeadroomGuard) detectUsage() *ResourceUsage {
	usage := &ResourceUsage{}

	// 1. cgroup v2 pids.current
	usage.CgroupPidsCurrent = h.getCgroupPidsCurrent()

	// 2. Process count from ps
	usage.ProcessCount = h.getProcessCount()

	// Prefer cgroup metric if available, otherwise use ps
	if usage.CgroupPidsCurrent > 0 {
		usage.EffectiveUsage = usage.CgroupPidsCurrent
		usage.Source = "cgroup"
	} else {
		usage.EffectiveUsage = usage.ProcessCount
		usage.Source = "ps"
	}

	slog.Debug("detected resource usage",
		"cgroup_pids_current", usage.CgroupPidsCurrent,
		"process_count", usage.ProcessCount,
		"effective_usage", usage.EffectiveUsage,
		"source", usage.Source,
	)

	return usage
}

// computeEffectiveLimit returns the minimum of all non-zero limits.
func (h *HeadroomGuard) computeEffectiveLimit(limits *ResourceLimits) (int, string) {
	type limitSource struct {
		value  int
		source string
	}

	candidates := []limitSource{
		{limits.UlimitNproc, "ulimit"},
		{limits.CgroupPidsMax, "cgroup"},
		{limits.SystemdTasksMax, "systemd"},
		{limits.KernelPidMax, "kernel"},
	}

	effective := 0
	source := ""

	for _, c := range candidates {
		if c.value > 0 && (effective == 0 || c.value < effective) {
			effective = c.value
			source = c.source
		}
	}

	return effective, source
}

// getUlimitNproc returns the user process limit from ulimit -u.
func (h *HeadroomGuard) getUlimitNproc() int {
	// Try reading from /proc/self/limits first (more reliable)
	data, err := os.ReadFile("/proc/self/limits")
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Max processes") {
				fields := strings.Fields(line)
				// Format: "Max processes    soft_limit    hard_limit    units"
				// After Fields(): ["Max", "processes", "soft_limit", "hard_limit", "processes"]
				// We want the soft limit which is fields[2]
				if len(fields) >= 4 {
					if val, err := strconv.Atoi(fields[2]); err == nil && val > 0 {
						return val
					}
				}
			}
		}
	}

	// Fallback to ulimit -u command
	cmd := exec.Command("sh", "-c", "ulimit -u")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	val, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0
	}

	return val
}

// getCgroupPidsMax returns the cgroup v2 pids.max limit.
func (h *HeadroomGuard) getCgroupPidsMax() int {
	// Find our cgroup path
	cgroupPath := h.findCgroupPath()
	if cgroupPath == "" {
		return 0
	}

	// Read pids.max
	pidsMaxPath := filepath.Join(cgroupPath, "pids.max")
	data, err := os.ReadFile(pidsMaxPath)
	if err != nil {
		return 0
	}

	content := strings.TrimSpace(string(data))
	if content == "max" {
		return 0 // Unlimited
	}

	val, err := strconv.Atoi(content)
	if err != nil {
		return 0
	}

	return val
}

// getCgroupPidsCurrent returns the current cgroup v2 pids.current.
func (h *HeadroomGuard) getCgroupPidsCurrent() int {
	cgroupPath := h.findCgroupPath()
	if cgroupPath == "" {
		return 0
	}

	pidsCurrentPath := filepath.Join(cgroupPath, "pids.current")
	data, err := os.ReadFile(pidsCurrentPath)
	if err != nil {
		return 0
	}

	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}

	return val
}

// findCgroupPath finds the cgroup v2 path for the current process.
func (h *HeadroomGuard) findCgroupPath() string {
	// Read /proc/self/cgroup
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}

	// For cgroup v2, the format is "0::/path"
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" {
			// cgroup v2 path
			cgroupPath := parts[2]
			return filepath.Join("/sys/fs/cgroup", cgroupPath)
		}
	}

	return ""
}

// getSystemdTasksMax returns the systemd TasksMax for the current user slice.
func (h *HeadroomGuard) getSystemdTasksMax() int {
	// Try to get TasksMax from user slice
	cmd := exec.Command("systemctl", "--user", "show", "-p", "TasksMax", "user.slice")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Parse output: "TasksMax=12345" or "TasksMax=infinity"
	content := strings.TrimSpace(string(output))
	parts := strings.SplitN(content, "=", 2)
	if len(parts) != 2 {
		return 0
	}

	if parts[1] == "infinity" || parts[1] == "18446744073709551615" {
		return 0 // Unlimited
	}

	val, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}

	return val
}

// getKernelPidMax returns the kernel's pid_max.
func (h *HeadroomGuard) getKernelPidMax() int {
	data, err := os.ReadFile("/proc/sys/kernel/pid_max")
	if err != nil {
		return 0
	}

	val, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}

	return val
}

// getProcessCount returns the number of processes owned by the current user.
func (h *HeadroomGuard) getProcessCount() int {
	uid := os.Getuid()

	// Count processes in /proc owned by current user
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID (numeric)
		_, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read the status file to get the process owner UID
		statusPath := filepath.Join("/proc", entry.Name(), "status")
		data, err := os.ReadFile(statusPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Uid:") {
				fields := strings.Fields(line)
				// Format: "Uid:    real_uid    effective_uid    saved_uid    fs_uid"
				// We want the real UID which is fields[1]
				if len(fields) >= 2 {
					if procUID, err := strconv.Atoi(fields[1]); err == nil && procUID == uid {
						count++
					}
				}
				break
			}
		}
	}

	return count
}

// Stop stops the headroom guard and cleans up resources.
func (h *HeadroomGuard) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stopped {
		return
	}
	h.stopped = true

	h.stopRecheck()
	close(h.recheckStop)
}
