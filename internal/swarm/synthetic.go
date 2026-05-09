package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/backpressure"
)

// SyntheticOutputPattern describes a fake agent's observable behavior.
type SyntheticOutputPattern string

const (
	SyntheticPatternIdle        SyntheticOutputPattern = "idle"
	SyntheticPatternWorking     SyntheticOutputPattern = "working"
	SyntheticPatternError       SyntheticOutputPattern = "error"
	SyntheticPatternRateLimit   SyntheticOutputPattern = "rate_limited"
	SyntheticPatternWaitingMail SyntheticOutputPattern = "waiting_for_mail"
	SyntheticPatternWriting     SyntheticOutputPattern = "writing_files"
	SyntheticPatternCompleted   SyntheticOutputPattern = "completed"
)

// SyntheticAgentState is the normalized state emitted by the harness.
type SyntheticAgentState string

const (
	SyntheticStateIdle        SyntheticAgentState = "idle"
	SyntheticStateWorking     SyntheticAgentState = "working"
	SyntheticStateError       SyntheticAgentState = "error"
	SyntheticStateRateLimit   SyntheticAgentState = "rate_limited"
	SyntheticStateWaitingMail SyntheticAgentState = "waiting_for_mail"
	SyntheticStateCompleted   SyntheticAgentState = "completed"
)

var defaultSyntheticPatterns = []SyntheticOutputPattern{
	SyntheticPatternIdle,
	SyntheticPatternWorking,
	SyntheticPatternError,
	SyntheticPatternRateLimit,
	SyntheticPatternWaitingMail,
	SyntheticPatternWriting,
	SyntheticPatternCompleted,
}

var syntheticAgentTypes = []string{"cc", "cod", "gmi"}

// SyntheticScenario configures an in-memory swarm run.
type SyntheticScenario struct {
	TestRunID             string                   `json:"test_run_id"`
	Name                  string                   `json:"name"`
	SessionName           string                   `json:"session_name"`
	PaneCount             int                      `json:"pane_count"`
	CommandCount          int                      `json:"command_count"`
	OutputLinesPerCommand int                      `json:"output_lines_per_command"`
	Patterns              []SyntheticOutputPattern `json:"patterns,omitempty"`
	StartTime             time.Time                `json:"start_time,omitempty"`
}

// SyntheticHarness runs deterministic fake swarm scenarios without tmux or model CLIs.
type SyntheticHarness struct {
	Logger *slog.Logger
}

// NewSyntheticHarness creates a synthetic swarm harness.
func NewSyntheticHarness(logger *slog.Logger) *SyntheticHarness {
	return &SyntheticHarness{Logger: logger}
}

// SyntheticRunResult is the machine-readable artifact from a synthetic run.
type SyntheticRunResult struct {
	Scenario    SyntheticScenario `json:"scenario"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
	Panes       []SyntheticPane   `json:"panes"`
	Events      []SyntheticEvent  `json:"events"`
	Metrics     SyntheticMetrics  `json:"metrics"`
}

// SyntheticPane is an in-memory fake tmux pane.
type SyntheticPane struct {
	ID           string                 `json:"id"`
	SessionName  string                 `json:"session_name"`
	Index        int                    `json:"index"`
	AgentType    string                 `json:"agent_type"`
	Pattern      SyntheticOutputPattern `json:"pattern"`
	State        SyntheticAgentState    `json:"state"`
	CommandCount int                    `json:"command_count"`
	EventCount   int                    `json:"event_count"`
	OutputTail   []string               `json:"output_tail"`
}

// SyntheticEvent records a fake pane event.
type SyntheticEvent struct {
	Seq           int                    `json:"seq"`
	Timestamp     time.Time              `json:"timestamp"`
	SessionName   string                 `json:"session_name"`
	PaneID        string                 `json:"pane_id"`
	PaneIndex     int                    `json:"pane_index"`
	AgentType     string                 `json:"agent_type"`
	Pattern       SyntheticOutputPattern `json:"pattern"`
	State         SyntheticAgentState    `json:"state"`
	Kind          string                 `json:"kind"`
	Message       string                 `json:"message"`
	LatencyMicros int64                  `json:"latency_micros"`
	OutputLines   []string               `json:"output_lines"`
}

// SyntheticMetrics summarizes harness scale and responsiveness.
type SyntheticMetrics struct {
	TestRunID               string `json:"test_run_id"`
	ScenarioName            string `json:"scenario_name"`
	SessionName             string `json:"session_name"`
	PaneCount               int    `json:"pane_count"`
	CommandCount            int    `json:"command_count"`
	EventCount              int    `json:"event_count"`
	LatencyP50Micros        int64  `json:"latency_p50_micros"`
	LatencyP95Micros        int64  `json:"latency_p95_micros"`
	LatencyMaxMicros        int64  `json:"latency_max_micros"`
	SyntheticDurationMicros int64  `json:"synthetic_duration_micros"`
	MemoryBaselineBytes     int64  `json:"memory_baseline_bytes"`
	MemoryPeakBytes         int64  `json:"memory_peak_bytes"`
	MemoryGrowthBytes       int64  `json:"memory_growth_bytes"`
	// Goroutines is the absolute goroutine count after the run.
	// Preserved alongside GoroutinesLeaked so a steady-state leak
	// (per-run delta = 0 but absolute count drifts upward) remains
	// observable, and so historical artifacts written under the
	// pre-bd-75unj schema parse cleanly.
	GoroutinesBaseline int `json:"goroutines_baseline"`
	GoroutinesPeak     int `json:"goroutines_peak"`
	Goroutines         int `json:"goroutines"`
	GoroutinesLeaked   int `json:"goroutines_leaked"`
}

// Run executes a deterministic in-memory scenario.
func (h *SyntheticHarness) Run(ctx context.Context, scenario SyntheticScenario) (*SyntheticRunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	scenario = normalizeSyntheticScenario(scenario)
	if err := validateSyntheticScenario(scenario); err != nil {
		return nil, err
	}

	logger := syntheticLogger(h)
	startedAt := scenario.StartTime
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
		scenario.StartTime = startedAt
	}

	logger.Info("synthetic_swarm_start",
		"test_run_id", scenario.TestRunID,
		"session", scenario.SessionName,
		"scenario", scenario.Name,
		"pane_count", scenario.PaneCount,
		"command_count", scenario.CommandCount)

	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutinesBefore := runtime.NumGoroutine()

	result := &SyntheticRunResult{
		Scenario:  scenario,
		StartedAt: startedAt,
		Panes:     make([]SyntheticPane, 0, scenario.PaneCount),
		Events:    make([]SyntheticEvent, 0, scenario.PaneCount*scenario.CommandCount),
	}
	latencies := make([]int64, 0, scenario.PaneCount*scenario.CommandCount)
	seq := 0

	for paneIndex := 1; paneIndex <= scenario.PaneCount; paneIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pattern := scenario.Patterns[(paneIndex-1)%len(scenario.Patterns)]
		state := stateForSyntheticPattern(pattern)
		pane := SyntheticPane{
			ID:          fmt.Sprintf("%s:%d", scenario.SessionName, paneIndex),
			SessionName: scenario.SessionName,
			Index:       paneIndex,
			AgentType:   syntheticAgentTypes[(paneIndex-1)%len(syntheticAgentTypes)],
			Pattern:     pattern,
			State:       state,
		}

		var outputTail []string
		for commandIndex := 1; commandIndex <= scenario.CommandCount; commandIndex++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			seq++
			latency := syntheticLatencyMicros(paneIndex, commandIndex, pattern)
			latencies = append(latencies, latency)
			lines := syntheticOutputLines(scenario, pane, commandIndex)
			outputTail = append(outputTail, lines...)
			outputTail = lastSyntheticLines(outputTail, 12)

			result.Events = append(result.Events, SyntheticEvent{
				Seq:           seq,
				Timestamp:     startedAt.Add(time.Duration(seq) * time.Millisecond),
				SessionName:   scenario.SessionName,
				PaneID:        pane.ID,
				PaneIndex:     pane.Index,
				AgentType:     pane.AgentType,
				Pattern:       pattern,
				State:         state,
				Kind:          "pane_output",
				Message:       syntheticMessage(pattern, paneIndex, commandIndex),
				LatencyMicros: latency,
				OutputLines:   lines,
			})

			pane.CommandCount++
			pane.EventCount++
		}

		pane.OutputTail = outputTail
		result.Panes = append(result.Panes, pane)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	goroutinesAfter := runtime.NumGoroutine()

	result.CompletedAt = startedAt.Add(time.Duration(seq) * time.Millisecond)
	result.Metrics = SyntheticMetrics{
		TestRunID:               scenario.TestRunID,
		ScenarioName:            scenario.Name,
		SessionName:             scenario.SessionName,
		PaneCount:               scenario.PaneCount,
		CommandCount:            scenario.CommandCount,
		EventCount:              seq,
		LatencyP50Micros:        syntheticPercentile(latencies, 50),
		LatencyP95Micros:        syntheticPercentile(latencies, 95),
		LatencyMaxMicros:        syntheticPercentile(latencies, 100),
		SyntheticDurationMicros: int64(result.CompletedAt.Sub(result.StartedAt) / time.Microsecond),
		MemoryBaselineBytes:     int64(before.Alloc),
		MemoryPeakBytes:         int64(after.Alloc),
		MemoryGrowthBytes:       nonNegativeMemoryGrowth(before, after),
		GoroutinesBaseline:      goroutinesBefore,
		GoroutinesPeak:          goroutinesAfter,
		Goroutines:              goroutinesAfter,
		GoroutinesLeaked:        nonNegativeIntDelta(goroutinesBefore, goroutinesAfter),
	}

	logger.Info("synthetic_swarm_complete",
		"test_run_id", scenario.TestRunID,
		"session", scenario.SessionName,
		"scenario", scenario.Name,
		"pane_count", result.Metrics.PaneCount,
		"event_count", result.Metrics.EventCount,
		"command_count", result.Metrics.CommandCount,
		"latency_p95_micros", result.Metrics.LatencyP95Micros,
		"memory_growth_bytes", result.Metrics.MemoryGrowthBytes,
		"goroutines", result.Metrics.Goroutines,
		"goroutines_leaked", result.Metrics.GoroutinesLeaked)

	return result, nil
}

// WriteArtifact writes the run result as stable indented JSON.
func (r *SyntheticRunResult) WriteArtifact(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

const SyntheticExperimentSchemaVersion = "ntm.swarm.experiment.v1"

// SyntheticExperimentGate identifies the cost class for a named scenario.
type SyntheticExperimentGate string

const (
	SyntheticExperimentGateShort     SyntheticExperimentGate = "short"
	SyntheticExperimentGateBenchmark SyntheticExperimentGate = "benchmark"
	SyntheticExperimentGateLoad      SyntheticExperimentGate = "load"
)

// SyntheticExperimentScenario is a named, registry-backed synthetic swarm run.
type SyntheticExperimentScenario struct {
	ID          string                    `json:"id"`
	Gate        SyntheticExperimentGate   `json:"gate"`
	Description string                    `json:"description,omitempty"`
	OptIn       bool                      `json:"opt_in"`
	Synthetic   SyntheticScenario         `json:"synthetic"`
	Budget      SyntheticExperimentBudget `json:"budget"`
}

// SyntheticExperimentBudget describes budget checks for comparing one run
// against a baseline without relying on fragile absolute timings alone.
type SyntheticExperimentBudget struct {
	Name                        string  `json:"name"`
	MaxLatencyP95Micros         int64   `json:"max_latency_p95_micros"`
	MaxMemoryGrowthBytes        int64   `json:"max_memory_growth_bytes"`
	MaxGoroutinesLeaked         int     `json:"max_goroutines_leaked"`
	MinEventThroughputPerSecond float64 `json:"min_event_throughput_per_second"`
	WarnRegressionRatio         float64 `json:"warn_regression_ratio"`
	FailRegressionRatio         float64 `json:"fail_regression_ratio"`
}

// SyntheticExperimentOptions configures a lab run.
type SyntheticExperimentOptions struct {
	Now                func() time.Time
	Logger             *slog.Logger
	ArtifactRoot       string
	Baseline           *SyntheticExperimentArtifact
	BackpressureInputs []backpressure.SurfaceInput
}

// SyntheticExperimentArtifact is the stable robot-readable experiment summary.
type SyntheticExperimentArtifact struct {
	SchemaVersion string                        `json:"schema_version"`
	GeneratedAt   string                        `json:"generated_at"`
	TestRunID     string                        `json:"test_run_id"`
	ScenarioID    string                        `json:"scenario_id"`
	Scenario      string                        `json:"scenario"`
	Gate          SyntheticExperimentGate       `json:"gate"`
	OptIn         bool                          `json:"opt_in"`
	Metrics       SyntheticExperimentMetrics    `json:"metrics"`
	Backpressure  SyntheticBackpressureArtifact `json:"backpressure"`
	Budget        SyntheticExperimentBudget     `json:"budget"`
	Comparison    SyntheticExperimentComparison `json:"comparison"`
	ArtifactPaths SyntheticExperimentPaths      `json:"artifact_paths"`
}

// SyntheticExperimentMetrics is a compact versioned projection of run metrics.
type SyntheticExperimentMetrics struct {
	PaneCount                int     `json:"pane_count"`
	CommandCount             int     `json:"command_count"`
	LatencyP50MS             float64 `json:"latency_p50_ms"`
	LatencyP95MS             float64 `json:"latency_p95_ms"`
	LatencyP99MS             float64 `json:"latency_p99_ms"`
	LatencyMaxMS             float64 `json:"latency_max_ms"`
	LatencySamples           int     `json:"latency_samples"`
	MemoryBaselineBytes      int64   `json:"memory_baseline_bytes"`
	MemoryPeakBytes          int64   `json:"memory_peak_bytes"`
	MemoryGrowthBytes        int64   `json:"memory_growth_bytes"`
	GoroutinesBaseline       int     `json:"goroutines_baseline"`
	GoroutinesPeak           int     `json:"goroutines_peak"`
	GoroutinesLeaked         int     `json:"goroutines_leaked"`
	EventCount               int     `json:"event_count"`
	EventThroughputPerSecond float64 `json:"event_throughput_per_second"`
	SyntheticDurationMicros  int64   `json:"synthetic_duration_micros"`
}

// SyntheticBackpressureArtifact freezes the overload state observed by a run.
type SyntheticBackpressureArtifact struct {
	SchemaVersion string                         `json:"schema_version"`
	Decision      backpressure.Decision          `json:"decision"`
	ReasonCodes   []backpressure.ReasonCode      `json:"reason_codes"`
	Surfaces      []backpressure.SurfaceSnapshot `json:"surfaces"`
	DroppedCount  int64                          `json:"dropped_count"`
	MaxQueueDepth int                            `json:"max_queue_depth"`
}

// SyntheticExperimentPaths records the files written for one experiment run.
type SyntheticExperimentPaths struct {
	Root         string `json:"root,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Latency      string `json:"latency,omitempty"`
	Memory       string `json:"memory,omitempty"`
	Goroutines   string `json:"goroutines,omitempty"`
	Backpressure string `json:"backpressure,omitempty"`
}

// SyntheticExperimentResult classifies a run against its budget and baseline.
type SyntheticExperimentResult string

const (
	SyntheticExperimentPass            SyntheticExperimentResult = "pass"
	SyntheticExperimentWarn            SyntheticExperimentResult = "warn"
	SyntheticExperimentFail            SyntheticExperimentResult = "fail"
	SyntheticExperimentMissingBaseline SyntheticExperimentResult = "missing_baseline"
	SyntheticExperimentSchemaMismatch  SyntheticExperimentResult = "schema_mismatch"
)

// SyntheticExperimentComparison explains the budget verdict.
type SyntheticExperimentComparison struct {
	Result            SyntheticExperimentResult  `json:"result"`
	BaselineTestRunID string                     `json:"baseline_test_run_id,omitempty"`
	Checks            []SyntheticExperimentCheck `json:"checks"`
	Hint              string                     `json:"hint,omitempty"`
}

// SyntheticExperimentCheck is one metric-level comparison row.
type SyntheticExperimentCheck struct {
	Metric   string                    `json:"metric"`
	Result   SyntheticExperimentResult `json:"result"`
	Current  float64                   `json:"current"`
	Baseline float64                   `json:"baseline,omitempty"`
	Budget   float64                   `json:"budget,omitempty"`
	Limit    string                    `json:"limit"`
}

// SyntheticExperimentSummary is a compact robot-readable lab rollup.
type SyntheticExperimentSummary struct {
	Success       bool                       `json:"success"`
	SchemaVersion string                     `json:"schema_version"`
	GeneratedAt   string                     `json:"generated_at"`
	Results       []SyntheticExperimentRow   `json:"results"`
	Warnings      []string                   `json:"warnings"`
	ArtifactPaths []SyntheticExperimentPaths `json:"artifact_paths"`
}

// SyntheticExperimentRow summarizes one scenario in a robot-safe table.
type SyntheticExperimentRow struct {
	TestRunID    string                    `json:"test_run_id"`
	ScenarioID   string                    `json:"scenario_id"`
	Gate         SyntheticExperimentGate   `json:"gate"`
	PaneCount    int                       `json:"pane_count"`
	CommandCount int                       `json:"command_count"`
	EventCount   int                       `json:"event_count"`
	Result       SyntheticExperimentResult `json:"result"`
	P95MS        float64                   `json:"p95_ms"`
}

// SyntheticExperimentScenarios returns the built-in scenario registry.
func SyntheticExperimentScenarios() []SyntheticExperimentScenario {
	start := time.Unix(1_700_010_000, 0).UTC()
	return []SyntheticExperimentScenario{
		{
			ID:          "short_smoke",
			Gate:        SyntheticExperimentGateShort,
			Description: "fast deterministic no-tmux smoke run",
			Synthetic: SyntheticScenario{
				TestRunID:             "lab-short-smoke",
				Name:                  "short smoke",
				SessionName:           "synthetic_lab_short",
				PaneCount:             6,
				CommandCount:          4,
				OutputLinesPerCommand: 2,
				StartTime:             start,
			},
			Budget: defaultSyntheticExperimentBudget("short"),
		},
		{
			ID:          "benchmark_32_pane",
			Gate:        SyntheticExperimentGateBenchmark,
			Description: "medium synthetic benchmark run for local profiling",
			Synthetic: SyntheticScenario{
				TestRunID:             "lab-benchmark-32",
				Name:                  "benchmark 32 pane",
				SessionName:           "synthetic_lab_benchmark",
				PaneCount:             32,
				CommandCount:          8,
				OutputLinesPerCommand: 1,
				StartTime:             start.Add(time.Minute),
			},
			Budget: defaultSyntheticExperimentBudget("benchmark"),
		},
		{
			ID:          "load_100_pane",
			Gate:        SyntheticExperimentGateLoad,
			Description: "opt-in load run that writes deterministic artifact schemas",
			OptIn:       true,
			Synthetic: SyntheticScenario{
				TestRunID:             "lab-load-100",
				Name:                  "load 100 pane",
				SessionName:           "synthetic_lab_load",
				PaneCount:             100,
				CommandCount:          5,
				OutputLinesPerCommand: 1,
				StartTime:             start.Add(2 * time.Minute),
			},
			Budget: defaultSyntheticExperimentBudget("load"),
		},
	}
}

// FindSyntheticExperimentScenario returns a registry scenario by ID.
func FindSyntheticExperimentScenario(id string) (SyntheticExperimentScenario, bool) {
	for _, scenario := range SyntheticExperimentScenarios() {
		if scenario.ID == id {
			return scenario, true
		}
	}
	return SyntheticExperimentScenario{}, false
}

// RunSyntheticExperiment executes a named scenario, compares it to an optional
// baseline, and optionally writes support-bundle-style artifact files.
func RunSyntheticExperiment(ctx context.Context, scenario SyntheticExperimentScenario, opts SyntheticExperimentOptions) (SyntheticExperimentArtifact, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	scenario = normalizeSyntheticExperimentScenario(scenario)

	result, err := NewSyntheticHarness(logger).Run(ctx, scenario.Synthetic)
	if err != nil {
		return SyntheticExperimentArtifact{}, err
	}
	inputs := opts.BackpressureInputs
	if len(inputs) == 0 {
		inputs = syntheticExperimentBackpressureInputs(result)
	}
	backpressureSnapshot := backpressure.Evaluate(inputs, backpressure.SnapshotOptions{Now: now})
	artifact := BuildSyntheticExperimentArtifact(result, scenario, backpressureSnapshot, now)
	artifact.Comparison = CompareSyntheticExperiment(artifact, opts.Baseline, scenario.Budget)
	if opts.ArtifactRoot != "" {
		paths, err := WriteSyntheticExperimentArtifacts(opts.ArtifactRoot, artifact)
		if err != nil {
			return SyntheticExperimentArtifact{}, err
		}
		artifact.ArtifactPaths = paths
	}

	logger.Info("synthetic_swarm_experiment_complete",
		"test_run_id", artifact.TestRunID,
		"scenario", artifact.ScenarioID,
		"pane_count", artifact.ScenarioPaneCount(),
		"command_count", artifact.ScenarioCommandCount(),
		"event_count", artifact.Metrics.EventCount,
		"budget", artifact.Budget.Name,
		"result", artifact.Comparison.Result,
		"artifact_path", artifact.ArtifactPaths.Summary)

	return artifact, nil
}

// BuildSyntheticExperimentArtifact projects a run into the stable lab schema.
func BuildSyntheticExperimentArtifact(result *SyntheticRunResult, scenario SyntheticExperimentScenario, bp backpressure.BackpressureSnapshot, now func() time.Time) SyntheticExperimentArtifact {
	if now == nil {
		now = time.Now
	}
	if result == nil {
		result = &SyntheticRunResult{}
	}
	scenario = normalizeSyntheticExperimentScenario(scenario)
	return SyntheticExperimentArtifact{
		SchemaVersion: SyntheticExperimentSchemaVersion,
		GeneratedAt:   now().UTC().Format(time.RFC3339Nano),
		TestRunID:     result.Metrics.TestRunID,
		ScenarioID:    scenario.ID,
		Scenario:      scenario.Synthetic.Name,
		Gate:          scenario.Gate,
		OptIn:         scenario.OptIn,
		Metrics:       syntheticExperimentMetrics(result),
		Backpressure:  syntheticBackpressureArtifact(bp),
		Budget:        normalizeSyntheticExperimentBudget(scenario.Budget),
		Comparison:    SyntheticExperimentComparison{Result: SyntheticExperimentMissingBaseline, Checks: []SyntheticExperimentCheck{}},
	}
}

// CompareSyntheticExperiment compares a current run with budget and baseline.
func CompareSyntheticExperiment(current SyntheticExperimentArtifact, baseline *SyntheticExperimentArtifact, budget SyntheticExperimentBudget) SyntheticExperimentComparison {
	budget = normalizeSyntheticExperimentBudget(budget)
	if baseline == nil {
		return SyntheticExperimentComparison{
			Result: SyntheticExperimentMissingBaseline,
			Checks: []SyntheticExperimentCheck{},
			Hint:   "No baseline artifact was provided; record this run before enforcing regressions.",
		}
	}
	if baseline.SchemaVersion != current.SchemaVersion {
		return SyntheticExperimentComparison{
			Result:            SyntheticExperimentSchemaMismatch,
			BaselineTestRunID: baseline.TestRunID,
			Checks:            []SyntheticExperimentCheck{},
			Hint:              "Baseline schema differs from current experiment schema.",
		}
	}

	checks := []SyntheticExperimentCheck{
		compareLowerIsBetter("latency_p95_ms", current.Metrics.LatencyP95MS, baseline.Metrics.LatencyP95MS, microsToMillis(budget.MaxLatencyP95Micros), budget),
		compareLowerIsBetter("memory_growth_bytes", float64(current.Metrics.MemoryGrowthBytes), float64(baseline.Metrics.MemoryGrowthBytes), float64(budget.MaxMemoryGrowthBytes), budget),
		compareGoroutinesLeaked(current.Metrics.GoroutinesLeaked, baseline.Metrics.GoroutinesLeaked, budget),
		compareHigherIsBetter("event_throughput_per_second", current.Metrics.EventThroughputPerSecond, baseline.Metrics.EventThroughputPerSecond, budget.MinEventThroughputPerSecond, budget),
	}
	overall := SyntheticExperimentPass
	for _, check := range checks {
		overall = maxSyntheticExperimentResult(overall, check.Result)
	}
	return SyntheticExperimentComparison{
		Result:            overall,
		BaselineTestRunID: baseline.TestRunID,
		Checks:            checks,
	}
}

// WriteSyntheticExperimentArtifacts writes matrix-style JSON artifacts.
func WriteSyntheticExperimentArtifacts(root string, artifact SyntheticExperimentArtifact) (SyntheticExperimentPaths, error) {
	if strings.TrimSpace(root) == "" {
		return SyntheticExperimentPaths{}, fmt.Errorf("artifact root is required")
	}
	dir := filepathForSyntheticExperiment(root, artifact)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	paths := SyntheticExperimentPaths{
		Root:         dir,
		Summary:      dir + string(os.PathSeparator) + "summary.json",
		Latency:      dir + string(os.PathSeparator) + "latency.json",
		Memory:       dir + string(os.PathSeparator) + "mem.json",
		Goroutines:   dir + string(os.PathSeparator) + "goroutines.json",
		Backpressure: dir + string(os.PathSeparator) + "backpressure.json",
	}
	if err := writeJSONFile(paths.Latency, syntheticLatencyArtifact(artifact)); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	if err := writeJSONFile(paths.Memory, syntheticMemoryArtifact(artifact)); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	if err := writeJSONFile(paths.Goroutines, syntheticGoroutineArtifact(artifact)); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	if err := writeJSONFile(paths.Backpressure, artifact.Backpressure); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	artifact.ArtifactPaths = paths
	if err := writeJSONFile(paths.Summary, artifact); err != nil {
		return SyntheticExperimentPaths{}, err
	}
	return paths, nil
}

// BuildSyntheticExperimentSummary returns a compact multi-run robot surface.
func BuildSyntheticExperimentSummary(artifacts []SyntheticExperimentArtifact, now func() time.Time) SyntheticExperimentSummary {
	if now == nil {
		now = time.Now
	}
	rows := make([]SyntheticExperimentRow, 0, len(artifacts))
	paths := make([]SyntheticExperimentPaths, 0, len(artifacts))
	warnings := make([]string, 0)
	success := true
	for _, artifact := range artifacts {
		rows = append(rows, SyntheticExperimentRow{
			TestRunID:    artifact.TestRunID,
			ScenarioID:   artifact.ScenarioID,
			Gate:         artifact.Gate,
			PaneCount:    artifact.ScenarioPaneCount(),
			CommandCount: artifact.ScenarioCommandCount(),
			EventCount:   artifact.Metrics.EventCount,
			Result:       artifact.Comparison.Result,
			P95MS:        artifact.Metrics.LatencyP95MS,
		})
		paths = append(paths, artifact.ArtifactPaths)
		switch artifact.Comparison.Result {
		case SyntheticExperimentFail, SyntheticExperimentSchemaMismatch:
			success = false
		case SyntheticExperimentMissingBaseline:
			warnings = append(warnings, artifact.ScenarioID+": missing baseline")
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ScenarioID != rows[j].ScenarioID {
			return rows[i].ScenarioID < rows[j].ScenarioID
		}
		return rows[i].Gate < rows[j].Gate
	})
	return SyntheticExperimentSummary{
		Success:       success,
		SchemaVersion: SyntheticExperimentSchemaVersion,
		GeneratedAt:   now().UTC().Format(time.RFC3339Nano),
		Results:       rows,
		Warnings:      nonNilStrings(warnings),
		ArtifactPaths: nonNilExperimentPaths(paths),
	}
}

// ScenarioPaneCount returns the pane count implied by the artifact row.
func (a SyntheticExperimentArtifact) ScenarioPaneCount() int {
	return a.Metrics.PaneCount
}

// ScenarioCommandCount returns the command count implied by the artifact row.
func (a SyntheticExperimentArtifact) ScenarioCommandCount() int {
	return a.Metrics.CommandCount
}

func syntheticLogger(h *SyntheticHarness) *slog.Logger {
	if h != nil && h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func normalizeSyntheticScenario(s SyntheticScenario) SyntheticScenario {
	if s.Name == "" {
		s.Name = "synthetic-swarm"
	}
	if s.SessionName == "" {
		s.SessionName = "synthetic_" + sanitizeSyntheticID(s.Name)
	}
	if s.TestRunID == "" {
		s.TestRunID = fmt.Sprintf("%s-%dp-%dc", sanitizeSyntheticID(s.Name), s.PaneCount, s.CommandCount)
	}
	if s.PaneCount == 0 {
		s.PaneCount = 1
	}
	if s.CommandCount == 0 {
		s.CommandCount = 1
	}
	if s.OutputLinesPerCommand == 0 {
		s.OutputLinesPerCommand = 1
	}
	if len(s.Patterns) == 0 {
		s.Patterns = append([]SyntheticOutputPattern(nil), defaultSyntheticPatterns...)
	}
	return s
}

func validateSyntheticScenario(s SyntheticScenario) error {
	if s.PaneCount < 1 {
		return fmt.Errorf("pane count must be positive, got %d", s.PaneCount)
	}
	if s.CommandCount < 1 {
		return fmt.Errorf("command count must be positive, got %d", s.CommandCount)
	}
	if s.OutputLinesPerCommand < 1 {
		return fmt.Errorf("output lines per command must be positive, got %d", s.OutputLinesPerCommand)
	}
	if len(s.Patterns) == 0 {
		return fmt.Errorf("at least one synthetic output pattern is required")
	}
	for _, pattern := range s.Patterns {
		if !isKnownSyntheticPattern(pattern) {
			return fmt.Errorf("unknown synthetic output pattern %q", pattern)
		}
	}
	return nil
}

func isKnownSyntheticPattern(pattern SyntheticOutputPattern) bool {
	switch pattern {
	case SyntheticPatternIdle,
		SyntheticPatternWorking,
		SyntheticPatternError,
		SyntheticPatternRateLimit,
		SyntheticPatternWaitingMail,
		SyntheticPatternWriting,
		SyntheticPatternCompleted:
		return true
	default:
		return false
	}
}

func stateForSyntheticPattern(pattern SyntheticOutputPattern) SyntheticAgentState {
	switch pattern {
	case SyntheticPatternIdle:
		return SyntheticStateIdle
	case SyntheticPatternWorking, SyntheticPatternWriting:
		return SyntheticStateWorking
	case SyntheticPatternError:
		return SyntheticStateError
	case SyntheticPatternRateLimit:
		return SyntheticStateRateLimit
	case SyntheticPatternWaitingMail:
		return SyntheticStateWaitingMail
	case SyntheticPatternCompleted:
		return SyntheticStateCompleted
	default:
		return SyntheticStateError
	}
}

func syntheticLatencyMicros(paneIndex, commandIndex int, pattern SyntheticOutputPattern) int64 {
	base := int64(700 + paneIndex%17*37 + commandIndex%11*23)
	switch pattern {
	case SyntheticPatternIdle:
		return base
	case SyntheticPatternWorking:
		return base + 900
	case SyntheticPatternWriting:
		return base + 1300
	case SyntheticPatternWaitingMail:
		return base + 2100
	case SyntheticPatternRateLimit:
		return base + 3100
	case SyntheticPatternError:
		return base + 4100
	case SyntheticPatternCompleted:
		return base + 500
	default:
		return base
	}
}

func syntheticMessage(pattern SyntheticOutputPattern, paneIndex, commandIndex int) string {
	switch pattern {
	case SyntheticPatternIdle:
		return "idle: waiting for assignment"
	case SyntheticPatternWorking:
		return fmt.Sprintf("working: completed synthetic step %d", commandIndex)
	case SyntheticPatternWriting:
		return fmt.Sprintf("writing files: synthetic pane %d command %d", paneIndex, commandIndex)
	case SyntheticPatternWaitingMail:
		return "waiting for mail thread bd-synthetic"
	case SyntheticPatternRateLimit:
		return "rate-limited: retry after 60s"
	case SyntheticPatternError:
		return fmt.Sprintf("error: synthetic failure at command %d", commandIndex)
	case SyntheticPatternCompleted:
		return "completed synthetic task"
	default:
		return "unknown synthetic state"
	}
}

func syntheticOutputLines(s SyntheticScenario, pane SyntheticPane, commandIndex int) []string {
	lines := make([]string, 0, s.OutputLinesPerCommand)
	for lineIndex := 1; lineIndex <= s.OutputLinesPerCommand; lineIndex++ {
		lines = append(lines, fmt.Sprintf(
			"run=%s session=%s pane=%d agent=%s command=%d line=%d pattern=%s message=%s",
			s.TestRunID,
			s.SessionName,
			pane.Index,
			pane.AgentType,
			commandIndex,
			lineIndex,
			pane.Pattern,
			syntheticMessage(pane.Pattern, pane.Index, commandIndex),
		))
	}
	return lines
}

func lastSyntheticLines(lines []string, limit int) []string {
	if len(lines) <= limit {
		return append([]string(nil), lines...)
	}
	return append([]string(nil), lines[len(lines)-limit:]...)
}

func syntheticPercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	if percentile <= 0 {
		return sorted[0]
	}
	index := int(math.Ceil(percentile/100*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func nonNegativeMemoryGrowth(before, after runtime.MemStats) int64 {
	growth := int64(after.Alloc) - int64(before.Alloc)
	if growth < 0 {
		return 0
	}
	return growth
}

func nonNegativeIntDelta(before, after int) int {
	if after <= before {
		return 0
	}
	return after - before
}

func normalizeSyntheticExperimentScenario(s SyntheticExperimentScenario) SyntheticExperimentScenario {
	if s.ID == "" {
		s.ID = sanitizeSyntheticID(s.Synthetic.Name)
	}
	if s.Gate == "" {
		s.Gate = SyntheticExperimentGateShort
	}
	s.Synthetic = normalizeSyntheticScenario(s.Synthetic)
	s.Budget = normalizeSyntheticExperimentBudget(s.Budget)
	return s
}

func defaultSyntheticExperimentBudget(name string) SyntheticExperimentBudget {
	return SyntheticExperimentBudget{
		Name:                        name,
		MaxLatencyP95Micros:         10_000,
		MaxMemoryGrowthBytes:        32 << 20,
		MaxGoroutinesLeaked:         0,
		MinEventThroughputPerSecond: 1,
		WarnRegressionRatio:         0.10,
		FailRegressionRatio:         0.25,
	}
}

func normalizeSyntheticExperimentBudget(b SyntheticExperimentBudget) SyntheticExperimentBudget {
	def := defaultSyntheticExperimentBudget(b.Name)
	if def.Name == "" {
		def.Name = "default"
	}
	if b.MaxLatencyP95Micros > 0 {
		def.MaxLatencyP95Micros = b.MaxLatencyP95Micros
	}
	if b.MaxMemoryGrowthBytes > 0 {
		def.MaxMemoryGrowthBytes = b.MaxMemoryGrowthBytes
	}
	if b.MaxGoroutinesLeaked >= 0 {
		def.MaxGoroutinesLeaked = b.MaxGoroutinesLeaked
	}
	if b.MinEventThroughputPerSecond > 0 {
		def.MinEventThroughputPerSecond = b.MinEventThroughputPerSecond
	}
	if b.WarnRegressionRatio > 0 {
		def.WarnRegressionRatio = b.WarnRegressionRatio
	}
	if b.FailRegressionRatio > 0 {
		def.FailRegressionRatio = b.FailRegressionRatio
	}
	return def
}

func syntheticExperimentBackpressureInputs(result *SyntheticRunResult) []backpressure.SurfaceInput {
	if result == nil {
		return []backpressure.SurfaceInput{{
			Surface:        backpressure.SurfaceProfiler,
			SourceLoaded:   false,
			MissingWarning: "synthetic run result is unavailable.",
		}}
	}
	capacity := result.Metrics.EventCount
	if capacity < 1 {
		capacity = 1
	}
	return []backpressure.SurfaceInput{
		{
			Surface:       backpressure.SurfaceWebSocket,
			Session:       result.Metrics.SessionName,
			QueueCapacity: capacity,
			SourceLoaded:  true,
		},
		{
			Surface:      backpressure.SurfaceTmuxCapture,
			Session:      result.Metrics.SessionName,
			LatencyMS:    result.Metrics.LatencyP95Micros / 1000,
			SourceLoaded: true,
		},
	}
}

func syntheticExperimentMetrics(result *SyntheticRunResult) SyntheticExperimentMetrics {
	durationSeconds := float64(result.Metrics.SyntheticDurationMicros) / 1_000_000
	throughput := 0.0
	if durationSeconds > 0 {
		throughput = float64(result.Metrics.EventCount) / durationSeconds
	}
	return SyntheticExperimentMetrics{
		PaneCount:                result.Metrics.PaneCount,
		CommandCount:             result.Metrics.CommandCount,
		LatencyP50MS:             microsToMillis(result.Metrics.LatencyP50Micros),
		LatencyP95MS:             microsToMillis(result.Metrics.LatencyP95Micros),
		LatencyP99MS:             microsToMillis(syntheticPercentile(latenciesFromResult(result), 99)),
		LatencyMaxMS:             microsToMillis(result.Metrics.LatencyMaxMicros),
		LatencySamples:           result.Metrics.EventCount,
		MemoryBaselineBytes:      result.Metrics.MemoryBaselineBytes,
		MemoryPeakBytes:          result.Metrics.MemoryPeakBytes,
		MemoryGrowthBytes:        result.Metrics.MemoryGrowthBytes,
		GoroutinesBaseline:       result.Metrics.GoroutinesBaseline,
		GoroutinesPeak:           result.Metrics.GoroutinesPeak,
		GoroutinesLeaked:         result.Metrics.GoroutinesLeaked,
		EventCount:               result.Metrics.EventCount,
		EventThroughputPerSecond: throughput,
		SyntheticDurationMicros:  result.Metrics.SyntheticDurationMicros,
	}
}

func syntheticBackpressureArtifact(snapshot backpressure.BackpressureSnapshot) SyntheticBackpressureArtifact {
	var dropped int64
	var maxQueue int
	for _, surface := range snapshot.Surfaces {
		dropped += surface.DroppedCount
		if surface.QueueDepth > maxQueue {
			maxQueue = surface.QueueDepth
		}
	}
	return SyntheticBackpressureArtifact{
		SchemaVersion: SyntheticExperimentSchemaVersion,
		Decision:      snapshot.Decision,
		ReasonCodes:   append([]backpressure.ReasonCode(nil), snapshot.ReasonCodes...),
		Surfaces:      append([]backpressure.SurfaceSnapshot(nil), snapshot.Surfaces...),
		DroppedCount:  dropped,
		MaxQueueDepth: maxQueue,
	}
}

func latenciesFromResult(result *SyntheticRunResult) []int64 {
	if result == nil {
		return nil
	}
	latencies := make([]int64, 0, len(result.Events))
	for _, event := range result.Events {
		latencies = append(latencies, event.LatencyMicros)
	}
	return latencies
}

func compareLowerIsBetter(metric string, current, baseline, budget float64, b SyntheticExperimentBudget) SyntheticExperimentCheck {
	check := SyntheticExperimentCheck{Metric: metric, Current: current, Baseline: baseline, Budget: budget, Limit: "lower_is_better", Result: SyntheticExperimentPass}
	if budget > 0 && current > budget {
		check.Result = SyntheticExperimentFail
		return check
	}
	if baseline > 0 {
		failAt := baseline * (1 + b.FailRegressionRatio)
		warnAt := baseline * (1 + b.WarnRegressionRatio)
		switch {
		case b.FailRegressionRatio > 0 && current > failAt:
			check.Result = SyntheticExperimentFail
		case b.WarnRegressionRatio > 0 && current > warnAt:
			check.Result = SyntheticExperimentWarn
		}
	}
	return check
}

func compareHigherIsBetter(metric string, current, baseline, budget float64, b SyntheticExperimentBudget) SyntheticExperimentCheck {
	check := SyntheticExperimentCheck{Metric: metric, Current: current, Baseline: baseline, Budget: budget, Limit: "higher_is_better", Result: SyntheticExperimentPass}
	if budget > 0 && current < budget {
		check.Result = SyntheticExperimentFail
		return check
	}
	if baseline > 0 {
		failAt := baseline * (1 - b.FailRegressionRatio)
		warnAt := baseline * (1 - b.WarnRegressionRatio)
		switch {
		case b.FailRegressionRatio > 0 && current < failAt:
			check.Result = SyntheticExperimentFail
		case b.WarnRegressionRatio > 0 && current < warnAt:
			check.Result = SyntheticExperimentWarn
		}
	}
	return check
}

func compareGoroutinesLeaked(current, baseline int, b SyntheticExperimentBudget) SyntheticExperimentCheck {
	check := SyntheticExperimentCheck{
		Metric:   "goroutines_leaked",
		Current:  float64(current),
		Baseline: float64(baseline),
		Budget:   float64(b.MaxGoroutinesLeaked),
		Limit:    "lower_is_better",
		Result:   SyntheticExperimentPass,
	}
	if current > b.MaxGoroutinesLeaked {
		check.Result = SyntheticExperimentFail
		return check
	}
	return compareLowerIsBetter(check.Metric, check.Current, check.Baseline, check.Budget, b)
}

func maxSyntheticExperimentResult(a, b SyntheticExperimentResult) SyntheticExperimentResult {
	if syntheticExperimentRank(b) > syntheticExperimentRank(a) {
		return b
	}
	return a
}

func syntheticExperimentRank(r SyntheticExperimentResult) int {
	switch r {
	case SyntheticExperimentWarn:
		return 1
	case SyntheticExperimentMissingBaseline:
		return 2
	case SyntheticExperimentSchemaMismatch:
		return 3
	case SyntheticExperimentFail:
		return 4
	default:
		return 0
	}
}

func filepathForSyntheticExperiment(root string, artifact SyntheticExperimentArtifact) string {
	return strings.TrimRight(root, string(os.PathSeparator)) +
		string(os.PathSeparator) + sanitizeSyntheticID(artifact.TestRunID) +
		string(os.PathSeparator) + sanitizeSyntheticID(artifact.ScenarioID) +
		string(os.PathSeparator) + string(artifact.Gate)
}

func syntheticLatencyArtifact(artifact SyntheticExperimentArtifact) map[string]any {
	return map[string]any{
		"schema_version": SyntheticExperimentSchemaVersion,
		"test_run_id":    artifact.TestRunID,
		"scenario":       artifact.ScenarioID,
		"p50_ms":         artifact.Metrics.LatencyP50MS,
		"p95_ms":         artifact.Metrics.LatencyP95MS,
		"p99_ms":         artifact.Metrics.LatencyP99MS,
		"max_ms":         artifact.Metrics.LatencyMaxMS,
		"samples":        artifact.Metrics.LatencySamples,
	}
}

func syntheticMemoryArtifact(artifact SyntheticExperimentArtifact) map[string]any {
	return map[string]any{
		"schema_version": SyntheticExperimentSchemaVersion,
		"test_run_id":    artifact.TestRunID,
		"scenario":       artifact.ScenarioID,
		"baseline_bytes": artifact.Metrics.MemoryBaselineBytes,
		"peak_bytes":     artifact.Metrics.MemoryPeakBytes,
		"delta_bytes":    artifact.Metrics.MemoryGrowthBytes,
	}
}

func syntheticGoroutineArtifact(artifact SyntheticExperimentArtifact) map[string]any {
	return map[string]any{
		"schema_version": SyntheticExperimentSchemaVersion,
		"test_run_id":    artifact.TestRunID,
		"scenario":       artifact.ScenarioID,
		"baseline":       artifact.Metrics.GoroutinesBaseline,
		"peak":           artifact.Metrics.GoroutinesPeak,
		"leaked":         artifact.Metrics.GoroutinesLeaked,
	}
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func microsToMillis(v int64) float64 {
	return float64(v) / 1000
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func nonNilExperimentPaths(in []SyntheticExperimentPaths) []SyntheticExperimentPaths {
	if in == nil {
		return []SyntheticExperimentPaths{}
	}
	return in
}

func sanitizeSyntheticID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '.':
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "synthetic"
	}
	return b.String()
}
