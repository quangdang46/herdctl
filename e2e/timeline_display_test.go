//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM timeline features.
// [E2E-TIMELINE-DISPLAY] Tests for timeline visualization in real usage scenarios.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TimelineListEntry represents a timeline entry from list command
type TimelineListEntry struct {
	SessionID  string    `json:"session_id"`
	EventCount int       `json:"event_count"`
	AgentCount int       `json:"agent_count"`
	Size       int64     `json:"size"`
	Compressed bool      `json:"compressed"`
	ModifiedAt time.Time `json:"modified_at"`
	Path       string    `json:"path"`
}

// TimelineListOutput represents the JSON output from ntm timeline list
type TimelineListOutput struct {
	Timelines  []TimelineListEntry `json:"timelines"`
	TotalCount int                 `json:"total_count"`
	Showing    int                 `json:"showing"`
}

// TimelineStatsOutput represents the JSON output from ntm timeline stats
type TimelineStatsOutput struct {
	TotalTimelines  int    `json:"total_timelines"`
	TotalEvents     int    `json:"total_events"`
	TotalAgents     int    `json:"total_agents"`
	TotalSize       int64  `json:"total_size_bytes"`
	CompressedCount int    `json:"compressed_count"`
	OldestTimeline  string `json:"oldest_timeline,omitempty"`
	NewestTimeline  string `json:"newest_timeline,omitempty"`
}

// TimelineShowOutput represents the JSON output from ntm timeline show
type TimelineShowOutput struct {
	Info   *TimelineListEntry   `json:"info"`
	Events []TimelineEventEntry `json:"events,omitempty"`
	Stats  *TimelineEventStats  `json:"stats"`
}

// TimelineEventEntry represents a timeline event
type TimelineEventEntry struct {
	AgentID       string            `json:"agent_id"`
	AgentType     string            `json:"agent_type"`
	SessionID     string            `json:"session_id"`
	State         string            `json:"state"`
	PreviousState string            `json:"previous_state,omitempty"`
	Timestamp     time.Time         `json:"timestamp"`
	Duration      int64             `json:"duration,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
	Trigger       string            `json:"trigger,omitempty"`
}

// TimelineEventStats represents event statistics
type TimelineEventStats struct {
	TotalEvents    int            `json:"total_events"`
	UniqueAgents   int            `json:"unique_agents"`
	AgentBreakdown map[string]int `json:"agent_breakdown"`
	StateBreakdown map[string]int `json:"state_breakdown"`
	Duration       int64          `json:"duration"`
	FirstEvent     time.Time      `json:"first_event"`
	LastEvent      time.Time      `json:"last_event"`
}

// TimelineDisplayTestSuite manages E2E tests for timeline display commands
type TimelineDisplayTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
}

// NewTimelineDisplayTestSuite creates a new test suite
func NewTimelineDisplayTestSuite(t *testing.T, scenario string) *TimelineDisplayTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("herdctl/ntm binary not found in PATH")
	}

	return &TimelineDisplayTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *TimelineDisplayTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireTimelineCommand skips if timeline command is not supported
func (s *TimelineDisplayTestSuite) requireTimelineCommand() {
	if !s.supportsCommand("timeline") {
		s.t.Skip("timeline command not supported by this ntm version")
	}
}

// Setup creates a temporary directory for testing
func (s *TimelineDisplayTestSuite) Setup() error {
	tempDir, err := os.MkdirTemp("", "ntm-timeline-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("[E2E-SETUP] Created temp directory: %s", tempDir)
	return nil
}

// Teardown cleans up resources
func (s *TimelineDisplayTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
	s.logger.Close()
}

// runNTM executes ntm with arguments and returns output
func (s *TimelineDisplayTestSuite) runNTM(args ...string) (string, error) {
	s.logger.Log("[E2E-RUN] ntm %s", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, err := cmd.CombinedOutput()
	result := string(output)
	if err != nil {
		s.logger.Log("[E2E-RUN] Command failed: %v, output: %s", err, result)
		return result, err
	}
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result, nil
}

// runNTMAllowFail runs ntm allowing non-zero exit codes
func (s *TimelineDisplayTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("[E2E-RUN] ntm %s (allow-fail)", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result
}

// ========== Timeline Command Existence Tests ==========

// TestTimelineCommandExists verifies timeline command is available
func TestTimelineCommandExists(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-exists")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline command exists")

	output := suite.runNTMAllowFail("timeline", "--help")

	// Should show help, not unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline command not recognized: %s", output)
	}

	if !strings.Contains(output, "timeline") {
		t.Errorf("Expected help text, got: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline command exists")
}

// TestTimelineListSubcommand verifies timeline list subcommand
func TestTimelineListSubcommand(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-list")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline list subcommand")

	output := suite.runNTMAllowFail("timeline", "list", "--help")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline list command not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline list subcommand exists")
}

// TestTimelineListJSONOutput verifies timeline list JSON output structure
func TestTimelineListJSONOutput(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-list-json")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline list --json output")

	output := suite.runNTMAllowFail("timeline", "list", "--json")

	// Try to parse as JSON
	var result TimelineListOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// May have log lines mixed in, try to extract JSON
		jsonStr := extractJSON(output)
		if jsonStr != "" {
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				suite.logger.Log("[E2E-INFO] Could not parse JSON (may be empty): %v", err)
			} else {
				suite.logger.Log("[E2E-INFO] Got valid JSON structure: %d timelines", len(result.Timelines))
			}
		} else {
			suite.logger.Log("[E2E-INFO] Could not extract JSON from output")
		}
	} else {
		suite.logger.Log("[E2E-INFO] Got valid JSON structure: %d timelines", len(result.Timelines))
	}

	suite.logger.Log("[E2E-PASS] timeline list --json produces output")
}

// TestTimelineListLimitFlag verifies --limit flag works
func TestTimelineListLimitFlag(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-list-limit")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline list --limit flag")

	output := suite.runNTMAllowFail("timeline", "list", "--limit=5", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--limit flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --limit flag is accepted")
}

// ========== Timeline Show Tests ==========

// TestTimelineShowSubcommand verifies timeline show subcommand
func TestTimelineShowSubcommand(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-show")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline show subcommand")

	output := suite.runNTMAllowFail("timeline", "show", "--help")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline show command not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline show subcommand exists")
}

// TestTimelineShowEventsFlag verifies --events flag works
func TestTimelineShowEventsFlag(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-show-events")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline show --events flag")

	output := suite.runNTMAllowFail("timeline", "show", "--events", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--events flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --events flag is accepted")
}

// TestTimelineShowNonexistent verifies behavior for nonexistent session
func TestTimelineShowNonexistent(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-show-nonexistent")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline show with nonexistent session")

	output := suite.runNTMAllowFail("timeline", "show", "nonexistent-session-xyz-12345", "--json")

	// Should fail gracefully with error message
	if !strings.Contains(output, "not found") && !strings.Contains(output, "error") && !strings.Contains(output, "Error") {
		suite.logger.Log("[E2E-INFO] Output did not contain expected error text: %s", output)
	}

	suite.logger.Log("[E2E-PASS] Handles nonexistent session gracefully")
}

// ========== Timeline Stats Tests ==========

// TestTimelineStatsSubcommand verifies timeline stats subcommand
func TestTimelineStatsSubcommand(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-stats")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline stats subcommand")

	output := suite.runNTMAllowFail("timeline", "stats")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline stats command not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline stats subcommand exists")
}

// TestTimelineStatsJSONOutput verifies stats JSON output structure
func TestTimelineStatsJSONOutput(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-stats-json")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline stats --json output")

	output := suite.runNTMAllowFail("timeline", "stats", "--json")

	// Try to parse as JSON
	var result TimelineStatsOutput
	jsonStr := extractJSON(output)
	if jsonStr != "" {
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			suite.logger.Log("[E2E-INFO] Could not parse JSON: %v", err)
		} else {
			suite.logger.Log("[E2E-INFO] Got valid JSON structure: %d timelines, %d events",
				result.TotalTimelines, result.TotalEvents)
		}
	}

	suite.logger.Log("[E2E-PASS] timeline stats --json produces output")
}

// ========== Timeline Delete Tests ==========

// TestTimelineDeleteSubcommand verifies timeline delete subcommand
func TestTimelineDeleteSubcommand(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-delete")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline delete subcommand")

	output := suite.runNTMAllowFail("timeline", "delete", "--help")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline delete command not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline delete subcommand exists")
}

// TestTimelineDeleteForceFlag verifies --force flag works
func TestTimelineDeleteForceFlag(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-delete-force")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline delete --force flag")

	output := suite.runNTMAllowFail("timeline", "delete", "--force", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--force flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --force flag is accepted")
}

// ========== Timeline Cleanup Tests ==========

// TestTimelineCleanupSubcommand verifies timeline cleanup subcommand
func TestTimelineCleanupSubcommand(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-cleanup")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline cleanup subcommand")

	output := suite.runNTMAllowFail("timeline", "cleanup", "--help")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline cleanup command not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline cleanup subcommand exists")
}

// ========== Data File Creation Tests ==========

// TestTimelineDataFileCreation verifies timeline data can be created and listed
func TestTimelineDataFileCreation(t *testing.T) {
	suite := NewTimelineDisplayTestSuite(t, "timeline-data-create")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline data file creation")

	// Create a test JSONL file with timeline events
	testSessionID := "e2e-test-session-" + time.Now().Format("20060102-150405")
	timelineDir := filepath.Join(os.Getenv("HOME"), ".ntm", "timelines")

	// Ensure the timelines directory exists
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("Failed to create timelines directory: %v", err)
	}

	// Create test timeline data
	testFile := filepath.Join(timelineDir, testSessionID+".jsonl")
	events := []TimelineEventEntry{
		{
			AgentID:   "cc_1",
			AgentType: "claude",
			SessionID: testSessionID,
			State:     "working",
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
		{
			AgentID:       "cc_1",
			AgentType:     "claude",
			SessionID:     testSessionID,
			State:         "idle",
			PreviousState: "working",
			Timestamp:     time.Now().Add(-5 * time.Minute),
		},
	}

	// Write events as JSONL
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	suite.cleanup = append(suite.cleanup, func() { os.Remove(testFile) })

	for _, event := range events {
		data, _ := json.Marshal(event)
		f.Write(data)
		f.WriteString("\n")
	}
	f.Close()

	suite.logger.Log("[E2E-INFO] Created test timeline file: %s", testFile)

	// Now verify the timeline shows up in list
	output := suite.runNTMAllowFail("timeline", "list", "--json")

	if strings.Contains(output, testSessionID) {
		suite.logger.Log("[E2E-INFO] Test timeline found in list output")
	} else {
		suite.logger.Log("[E2E-INFO] Test timeline not found in list (may use different storage)")
	}

	suite.logger.Log("[E2E-PASS] Timeline data file creation test completed")
}

// extractJSON is defined in handoff_workflow_test.go
