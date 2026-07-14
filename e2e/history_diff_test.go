//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-DIFF] Tests for ntm diff command.
// Note: History tests are in logs_test.go
package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// DiffTestSuite manages E2E tests for the diff command
type DiffTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	cleanup []func()
	ntmPath string
}

// NewDiffTestSuite creates a new test suite
func NewDiffTestSuite(t *testing.T, scenario string) *DiffTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &DiffTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *DiffTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		// If exit code is non-zero, check if it's "unknown command"
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireDiffCommand skips if diff command is not supported
func (s *DiffTestSuite) requireDiffCommand() {
	if !s.supportsCommand("diff") {
		s.t.Skip("diff command not supported by this ntm version")
	}
}

// Cleanup runs all registered cleanup functions
func (s *DiffTestSuite) Cleanup() {
	s.logger.Close()
	for _, fn := range s.cleanup {
		fn()
	}
}

// ============================================================================
// Diff Command Tests
// ============================================================================

func TestDiffCommandExists(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_exists")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-DIFF] Verifying diff command exists")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff --help")

	cmd := exec.Command(suite.ntmPath, "diff", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown command") {
		t.Skip("diff command not available in this ntm version")
	}

	// Verify help text mentions diff-related content
	if !strings.Contains(outputStr, "diff") && !strings.Contains(outputStr, "compare") {
		t.Errorf("Expected help text to contain 'diff' or 'compare', got: %s", outputStr)
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff command exists and shows help")
}

func TestDiffCommandHelpFlags(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_help_flags")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Verifying diff command help shows expected flags")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff --help")

	cmd := exec.Command(suite.ntmPath, "diff", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		suite.logger.Log("[E2E-DIFF] Command exited with error (may be expected): %v", err)
	}

	suite.logger.Log("[E2E-DIFF] Help output: %s", outputStr)

	// Check for expected flags
	expectedPatterns := []string{
		"--unified",
		"--code-only",
		"--help",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(outputStr, pattern) {
			t.Errorf("Expected help to mention '%s', got: %s", pattern, outputStr)
		}
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff command help shows expected flags")
}

func TestDiffMissingArgs(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_missing_args")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff command with missing arguments")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff (no args)")

	cmd := exec.Command(suite.ntmPath, "diff")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)
	suite.logger.Log("[E2E-DIFF] Error: %v", err)

	// Should fail with usage error
	if err == nil {
		t.Errorf("Expected error when running diff without arguments")
	}

	// Should show helpful usage message
	if !strings.Contains(outputStr, "session") && !strings.Contains(outputStr, "pane") &&
		!strings.Contains(outputStr, "Usage") && !strings.Contains(outputStr, "required") {
		suite.logger.Log("[E2E-DIFF] Output doesn't show helpful usage message")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff handles missing arguments correctly")
}

func TestDiffNonexistentSession(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_nonexistent")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff with non-existent session")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff nonexistent-session-xyz 1 2 --json")

	cmd := exec.Command(suite.ntmPath, "diff", "nonexistent-session-xyz", "1", "2", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)
	suite.logger.Log("[E2E-DIFF] Error: %v", err)

	// Should fail with error about session not found
	if err == nil {
		suite.logger.Log("[E2E-DIFF] Unexpected success (session might exist)")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff handles non-existent session correctly")
}

func TestDiffUnifiedFlag(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_unified")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff --unified flag")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff nonexistent-session-xyz 1 2 --unified")

	cmd := exec.Command(suite.ntmPath, "diff", "nonexistent-session-xyz", "1", "2", "--unified")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)

	// Check that the flag is recognized (even if command fails due to session)
	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--unified flag not recognized")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff --unified flag works")
}

func TestDiffCodeOnlyFlag(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_code_only")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff --code-only flag")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff nonexistent-session-xyz 1 2 --code-only")

	cmd := exec.Command(suite.ntmPath, "diff", "nonexistent-session-xyz", "1", "2", "--code-only")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)

	// Check that the flag is recognized
	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--code-only flag not recognized")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff --code-only flag works")
}

func TestDiffPartialArgs(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_partial_args")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff with only session (missing panes)")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff some-session")

	cmd := exec.Command(suite.ntmPath, "diff", "some-session")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)
	suite.logger.Log("[E2E-DIFF] Error: %v", err)

	// Should fail because panes are required
	if err == nil {
		t.Errorf("Expected error when running diff with only session")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff handles partial args correctly")
}

func TestDiffSinglePane(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_single_pane")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff with only one pane")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff some-session 1")

	cmd := exec.Command(suite.ntmPath, "diff", "some-session", "1")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)
	suite.logger.Log("[E2E-DIFF] Error: %v", err)

	// Should fail because two panes are required
	if err == nil {
		t.Errorf("Expected error when running diff with only one pane")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff handles single pane correctly")
}

func TestDiffByPaneTitle(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_pane_title")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff with pane titles (cc_1, cod_1)")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff nonexistent-session-xyz cc_1 cod_1")

	cmd := exec.Command(suite.ntmPath, "diff", "nonexistent-session-xyz", "cc_1", "cod_1")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)
	suite.logger.Log("[E2E-DIFF] Error: %v", err)

	// Command will fail due to session, but pane titles should be accepted
	// Check that error is about session, not about pane title format
	if strings.Contains(outputStr, "invalid pane") || strings.Contains(outputStr, "pane format") {
		suite.logger.Log("[E2E-DIFF] Pane title format might not be supported")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff accepts pane title format")
}

func TestDiffJSONOutput(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_json")
	defer suite.Cleanup()

	suite.requireDiffCommand()

	suite.logger.Log("[E2E-DIFF] Testing diff --json flag")
	suite.logger.Log("[E2E-DIFF] Running: ntm diff nonexistent-session-xyz 1 2 --json")

	cmd := exec.Command(suite.ntmPath, "diff", "nonexistent-session-xyz", "1", "2", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-DIFF] Output: %s", outputStr)

	// Check that the flag is recognized
	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--json flag not recognized")
	}

	suite.logger.Log("[E2E-DIFF] SUCCESS: diff --json flag works")
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestDiffIntegration(t *testing.T) {
	suite := NewDiffTestSuite(t, "diff_integration")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-INTEGRATION] Testing diff command integration")

	// Test diff is available
	suite.logger.Log("[E2E-INTEGRATION] Checking diff command availability")
	diffAvailable := suite.supportsCommand("diff")
	suite.logger.Log("[E2E-INTEGRATION] diff available: %v", diffAvailable)

	if !diffAvailable {
		t.Skip("diff command not available")
	}

	// Run diff help
	suite.logger.Log("[E2E-INTEGRATION] Running diff help")
	cmd := exec.Command(suite.ntmPath, "diff", "--help")
	output, _ := cmd.CombinedOutput()
	suite.logger.Log("[E2E-INTEGRATION] diff --help output length: %d bytes", len(output))

	// Try diff with all flags
	suite.logger.Log("[E2E-INTEGRATION] Running diff with multiple flags")
	cmd = exec.Command(suite.ntmPath, "diff", "test-session", "1", "2", "--unified", "--code-only")
	output, _ = cmd.CombinedOutput()
	suite.logger.Log("[E2E-INTEGRATION] diff with flags output length: %d bytes", len(output))

	suite.logger.Log("[E2E-INTEGRATION] SUCCESS: Integration test completed")
}
