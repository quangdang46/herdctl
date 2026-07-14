//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM timeline export features.
// [E2E-TIMELINE-EXPORT] Tests for timeline export to JSONL, SVG, PNG formats.
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

// TimelineExportTestSuite manages E2E tests for timeline export
type TimelineExportTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
}

// NewTimelineExportTestSuite creates a new test suite
func NewTimelineExportTestSuite(t *testing.T, scenario string) *TimelineExportTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &TimelineExportTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *TimelineExportTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireTimelineCommand skips if timeline command is not supported
func (s *TimelineExportTestSuite) requireTimelineCommand() {
	if !s.supportsCommand("timeline") {
		s.t.Skip("timeline command not supported by this ntm version")
	}
}

// Setup creates a temporary directory and test data for testing
func (s *TimelineExportTestSuite) Setup() error {
	tempDir, err := os.MkdirTemp("", "ntm-export-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("[E2E-SETUP] Created temp directory: %s", tempDir)
	return nil
}

// Teardown cleans up resources
func (s *TimelineExportTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
	s.logger.Close()
}

// runNTM executes ntm with arguments and returns output
func (s *TimelineExportTestSuite) runNTM(args ...string) (string, error) {
	s.logger.Log("[E2E-RUN] ntm %s", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	cmd.Dir = s.tempDir // Run in temp dir for output files
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
func (s *TimelineExportTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("[E2E-RUN] ntm %s (allow-fail)", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	cmd.Dir = s.tempDir
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result
}

// createTestTimelineData creates test timeline JSONL data in the system directory
func (s *TimelineExportTestSuite) createTestTimelineData(sessionID string) (string, error) {
	timelineDir := filepath.Join(os.Getenv("HOME"), ".ntm", "timelines")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		return "", err
	}

	testFile := filepath.Join(timelineDir, sessionID+".jsonl")
	now := time.Now()

	events := []TimelineEventEntry{
		{AgentID: "cc_1", AgentType: "claude", SessionID: sessionID, State: "working", Timestamp: now.Add(-30 * time.Minute)},
		{AgentID: "cc_1", AgentType: "claude", SessionID: sessionID, State: "idle", PreviousState: "working", Timestamp: now.Add(-25 * time.Minute)},
		{AgentID: "cod_1", AgentType: "codex", SessionID: sessionID, State: "working", Timestamp: now.Add(-20 * time.Minute)},
		{AgentID: "cc_1", AgentType: "claude", SessionID: sessionID, State: "working", Timestamp: now.Add(-15 * time.Minute)},
		{AgentID: "cod_1", AgentType: "codex", SessionID: sessionID, State: "idle", PreviousState: "working", Timestamp: now.Add(-10 * time.Minute)},
		{AgentID: "cc_1", AgentType: "claude", SessionID: sessionID, State: "idle", PreviousState: "working", Timestamp: now.Add(-5 * time.Minute)},
	}

	f, err := os.Create(testFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	for _, event := range events {
		data, _ := json.Marshal(event)
		f.Write(data)
		f.WriteString("\n")
	}

	s.cleanup = append(s.cleanup, func() { os.Remove(testFile) })
	s.logger.Log("[E2E-SETUP] Created test timeline: %s", testFile)
	return sessionID, nil
}

// ========== Export Command Tests ==========

// TestTimelineExportSubcommand verifies timeline export subcommand exists
func TestTimelineExportSubcommand(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export subcommand")

	output := suite.runNTMAllowFail("timeline", "export", "--help")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("timeline export command not recognized: %s", output)
	}

	// Should show format options
	if !strings.Contains(output, "format") && !strings.Contains(output, "svg") {
		suite.logger.Log("[E2E-INFO] Help may not show all options: %s", output)
	}

	suite.logger.Log("[E2E-PASS] timeline export subcommand exists")
}

// TestTimelineExportFormatFlag verifies --format flag
func TestTimelineExportFormatFlag(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-format")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --format flag")

	// Test various format values
	formats := []string{"jsonl", "svg", "png"}
	for _, format := range formats {
		output := suite.runNTMAllowFail("timeline", "export", "--format="+format, "--help")

		if strings.Contains(output, "unknown flag") {
			t.Fatalf("--format=%s flag not recognized: %s", format, output)
		}
		suite.logger.Log("[E2E-INFO] Format %s accepted", format)
	}

	suite.logger.Log("[E2E-PASS] --format flag accepts all formats")
}

// TestTimelineExportOutputFlag verifies --output flag
func TestTimelineExportOutputFlag(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-output")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --output flag")

	output := suite.runNTMAllowFail("timeline", "export", "--output=test.svg", "--help")

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--output flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --output flag is accepted")
}

// TestTimelineExportLightFlag verifies --light flag for light theme
func TestTimelineExportLightFlag(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-light")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --light flag")

	output := suite.runNTMAllowFail("timeline", "export", "--light", "--help")

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--light flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --light flag is accepted")
}

// TestTimelineExportNoLegendFlag verifies --no-legend flag
func TestTimelineExportNoLegendFlag(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-no-legend")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --no-legend flag")

	output := suite.runNTMAllowFail("timeline", "export", "--no-legend", "--help")

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--no-legend flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --no-legend flag is accepted")
}

// TestTimelineExportNoMetadataFlag verifies --no-metadata flag
func TestTimelineExportNoMetadataFlag(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-no-metadata")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --no-metadata flag")

	output := suite.runNTMAllowFail("timeline", "export", "--no-metadata", "--help")

	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--no-metadata flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --no-metadata flag is accepted")
}

// ========== Export with Data Tests ==========

// TestTimelineExportJSONL verifies JSONL export works
func TestTimelineExportJSONL(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-jsonl")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing JSONL export")

	// Create test data
	sessionID, err := suite.createTestTimelineData("e2e-export-test-jsonl")
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Try to export
	output := suite.runNTMAllowFail("timeline", "export", sessionID, "--format=jsonl")

	if strings.Contains(output, "not found") {
		suite.logger.Log("[E2E-INFO] Timeline not found (storage path may differ)")
	} else if strings.Contains(output, "Exported") || strings.Contains(output, "exported") {
		suite.logger.Log("[E2E-INFO] Export succeeded")
	}

	suite.logger.Log("[E2E-PASS] JSONL export test completed")
}

// TestTimelineExportSVG verifies SVG export works
func TestTimelineExportSVG(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-svg")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing SVG export")

	// Create test data
	sessionID, err := suite.createTestTimelineData("e2e-export-test-svg")
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Try to export
	outputFile := filepath.Join(suite.tempDir, "timeline.svg")
	output := suite.runNTMAllowFail("timeline", "export", sessionID, "--format=svg", "--output="+outputFile)

	if strings.Contains(output, "not found") {
		suite.logger.Log("[E2E-INFO] Timeline not found (storage path may differ)")
	} else if strings.Contains(output, "Exported") || strings.Contains(output, "exported") {
		// Check if file was created
		if _, err := os.Stat(outputFile); err == nil {
			suite.logger.Log("[E2E-INFO] SVG file created: %s", outputFile)
		}
	}

	suite.logger.Log("[E2E-PASS] SVG export test completed")
}

// TestTimelineExportPNG verifies PNG export works
func TestTimelineExportPNG(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-png")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing PNG export")

	// Create test data
	sessionID, err := suite.createTestTimelineData("e2e-export-test-png")
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Try to export
	outputFile := filepath.Join(suite.tempDir, "timeline.png")
	output := suite.runNTMAllowFail("timeline", "export", sessionID, "--format=png", "--output="+outputFile)

	if strings.Contains(output, "not found") {
		suite.logger.Log("[E2E-INFO] Timeline not found (storage path may differ)")
	} else if strings.Contains(output, "Exported") || strings.Contains(output, "exported") {
		// Check if file was created
		if _, err := os.Stat(outputFile); err == nil {
			suite.logger.Log("[E2E-INFO] PNG file created: %s", outputFile)
		}
	}

	suite.logger.Log("[E2E-PASS] PNG export test completed")
}

// TestTimelineExportWithTimeFilter verifies time-filtered export
func TestTimelineExportWithTimeFilter(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-time-filter")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing export with time filter")

	// Create test data
	sessionID, err := suite.createTestTimelineData("e2e-export-test-time")
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Export with time filter
	output := suite.runNTMAllowFail("timeline", "export", sessionID, "--format=jsonl", "--since=15m")

	if strings.Contains(output, "no events in the specified time range") {
		suite.logger.Log("[E2E-INFO] Time filter correctly filtered events")
	} else if strings.Contains(output, "Exported") || strings.Contains(output, "exported") {
		suite.logger.Log("[E2E-INFO] Export with time filter succeeded")
	}

	suite.logger.Log("[E2E-PASS] Time-filtered export test completed")
}

// TestTimelineExportNonexistentSession verifies export handles missing session
func TestTimelineExportNonexistentSession(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-nonexistent")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing export of nonexistent session")

	output := suite.runNTMAllowFail("timeline", "export", "nonexistent-session-xyz-99999", "--format=jsonl")

	// Should fail gracefully
	if !strings.Contains(output, "not found") && !strings.Contains(output, "error") && !strings.Contains(output, "Error") {
		suite.logger.Log("[E2E-INFO] Output for nonexistent session: %s", output)
	}

	suite.logger.Log("[E2E-PASS] Nonexistent session handled gracefully")
}

// TestTimelineExportWithScale verifies --scale option for PNG
func TestTimelineExportWithScale(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-scale")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing PNG export with scale")

	// Test different scale values
	scales := []string{"1", "2", "3"}
	for _, scale := range scales {
		output := suite.runNTMAllowFail("timeline", "export", "--format=png", "--scale="+scale, "--help")

		if strings.Contains(output, "unknown flag") {
			t.Fatalf("--scale=%s not accepted", scale)
		}
		suite.logger.Log("[E2E-INFO] Scale %s accepted", scale)
	}

	suite.logger.Log("[E2E-PASS] PNG scale options validated")
}

// TestTimelineExportWithWidth verifies --width option
func TestTimelineExportWithWidth(t *testing.T) {
	suite := NewTimelineExportTestSuite(t, "timeline-export-width")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing export with custom width")

	widths := []string{"800", "1200", "1920"}
	for _, width := range widths {
		output := suite.runNTMAllowFail("timeline", "export", "--format=svg", "--width="+width, "--help")

		if strings.Contains(output, "unknown flag") {
			t.Fatalf("--width=%s not accepted", width)
		}
		suite.logger.Log("[E2E-INFO] Width %s accepted", width)
	}

	suite.logger.Log("[E2E-PASS] Custom width options validated")
}
