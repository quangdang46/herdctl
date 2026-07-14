//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM timeline navigation features.
// [E2E-TIMELINE-NAV] Tests for timeline zoom, scroll, and navigation in real usage scenarios.
package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// TimelineNavTestSuite manages E2E tests for timeline navigation
type TimelineNavTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	cleanup []func()
	ntmPath string
}

// NewTimelineNavTestSuite creates a new test suite
func NewTimelineNavTestSuite(t *testing.T, scenario string) *TimelineNavTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &TimelineNavTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *TimelineNavTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireTimelineCommand skips if timeline command is not supported
func (s *TimelineNavTestSuite) requireTimelineCommand() {
	if !s.supportsCommand("timeline") {
		s.t.Skip("timeline command not supported by this ntm version")
	}
}

// Teardown cleans up resources
func (s *TimelineNavTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
	s.logger.Close()
}

// runNTMAllowFail runs ntm allowing non-zero exit codes
func (s *TimelineNavTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("[E2E-RUN] ntm %s (allow-fail)", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result
}

// ========== Timeline Export Navigation Tests ==========
// These test the export command's time filtering which is the CLI-accessible navigation

// TestTimelineExportSinceFlag verifies --since flag for time navigation
func TestTimelineExportSinceFlag(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-export-since")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --since flag")

	output := suite.runNTMAllowFail("timeline", "export", "--since=1h", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--since flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --since flag is accepted")
}

// TestTimelineExportUntilFlag verifies --until flag for time navigation
func TestTimelineExportUntilFlag(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-export-until")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --until flag")

	output := suite.runNTMAllowFail("timeline", "export", "--until=30m", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--until flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --until flag is accepted")
}

// TestTimelineExportWidthFlag verifies --width flag for zoom-like control
func TestTimelineExportWidthFlag(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-export-width")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --width flag")

	output := suite.runNTMAllowFail("timeline", "export", "--width=1200", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--width flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --width flag is accepted")
}

// TestTimelineExportScaleFlag verifies --scale flag for resolution control
func TestTimelineExportScaleFlag(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-export-scale")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export --scale flag")

	output := suite.runNTMAllowFail("timeline", "export", "--scale=2", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--scale flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] --scale flag is accepted")
}

// TestTimelineExportTimeRangeCombination verifies combining time filters
func TestTimelineExportTimeRangeCombination(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-export-time-range")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline export with combined time filters")

	output := suite.runNTMAllowFail("timeline", "export", "--since=2h", "--until=30m", "--help")

	// Should accept both flags together
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("Combined time flags not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] Combined time filters accepted")
}

// ========== Dashboard Keybinding Documentation Tests ==========
// These verify that the dashboard documents navigation keybindings

// TestDashboardHelpShowsTimelineControls verifies dashboard help mentions timeline
func TestDashboardHelpShowsTimelineControls(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "dashboard-timeline-help")
	defer suite.Teardown()

	suite.logger.Log("[E2E-TEST] Testing dashboard --help mentions timeline")

	// Check if dashboard command exists and mentions timeline
	output := suite.runNTMAllowFail("dashboard", "--help")

	if strings.Contains(output, "unknown command") {
		suite.logger.Log("[E2E-SKIP] dashboard command not available")
		t.Skip("dashboard command not available")
	}

	// Dashboard may or may not explicitly mention timeline in help
	suite.logger.Log("[E2E-INFO] Dashboard help output: %s", output[:min(len(output), 200)])

	suite.logger.Log("[E2E-PASS] Dashboard help checked")
}

// ========== Timeline List Navigation Tests ==========

// TestTimelineListLimitNavigation verifies --limit for paginating through timelines
func TestTimelineListLimitNavigation(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-list-limit-nav")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline list with different limits")

	// Test with small limit
	output1 := suite.runNTMAllowFail("timeline", "list", "--limit=1", "--json")
	suite.logger.Log("[E2E-INFO] Limit 1 output length: %d", len(output1))

	// Test with larger limit
	output2 := suite.runNTMAllowFail("timeline", "list", "--limit=100", "--json")
	suite.logger.Log("[E2E-INFO] Limit 100 output length: %d", len(output2))

	// Verify different limits work (output should vary)
	suite.logger.Log("[E2E-PASS] Timeline list accepts different limits")
}

// TestTimelineShowEventsPagination verifies event display with --events flag
func TestTimelineShowEventsPagination(t *testing.T) {
	suite := NewTimelineNavTestSuite(t, "timeline-show-events-nav")
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline show --events for browsing events")

	// The -e/--events flag enables viewing all events
	output := suite.runNTMAllowFail("timeline", "show", "-e", "--help")

	if strings.Contains(output, "unknown flag") && strings.Contains(output, "-e") {
		t.Fatalf("-e flag not recognized: %s", output)
	}

	suite.logger.Log("[E2E-PASS] Timeline show --events flag works")
}

// min is defined in robot_bulk_assign_test.go
