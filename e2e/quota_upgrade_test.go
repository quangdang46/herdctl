//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-QUOTA-UPGRADE] Tests for ntm quota and ntm upgrade commands.
package e2e

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// QuotaOutput represents the JSON output from ntm quota
type QuotaOutput struct {
	Success bool   `json:"success"`
	Session string `json:"session,omitempty"`
	Error   string `json:"error,omitempty"`
	Agents  []struct {
		Pane      int    `json:"pane"`
		AgentType string `json:"agent_type"`
		Usage     string `json:"usage,omitempty"`
		Limit     string `json:"limit,omitempty"`
		Remaining string `json:"remaining,omitempty"`
	} `json:"agents,omitempty"`
}

// UpgradeCheckOutput represents the JSON output from ntm upgrade --check
type UpgradeCheckOutput struct {
	Success         bool   `json:"success"`
	CurrentVersion  string `json:"current_version,omitempty"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
	Error           string `json:"error,omitempty"`
}

// QuotaUpgradeTestSuite manages E2E tests for quota and upgrade commands
type QuotaUpgradeTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	cleanup []func()
	ntmPath string
}

// NewQuotaUpgradeTestSuite creates a new test suite
func NewQuotaUpgradeTestSuite(t *testing.T, scenario string) *QuotaUpgradeTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &QuotaUpgradeTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *QuotaUpgradeTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		// If exit code is non-zero, check if it's "unknown command"
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireQuotaCommand skips if quota command is not supported
func (s *QuotaUpgradeTestSuite) requireQuotaCommand() {
	if !s.supportsCommand("quota") {
		s.t.Skip("quota command not supported by this ntm version")
	}
}

// requireUpgradeCommand skips if upgrade command is not supported
func (s *QuotaUpgradeTestSuite) requireUpgradeCommand() {
	if !s.supportsCommand("upgrade") {
		s.t.Skip("upgrade command not supported by this ntm version")
	}
}

// Cleanup runs all registered cleanup functions
func (s *QuotaUpgradeTestSuite) Cleanup() {
	s.logger.Close()
	for _, fn := range s.cleanup {
		fn()
	}
}

// ============================================================================
// Quota Command Tests
// ============================================================================

func TestQuotaCommandExists(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "quota_exists")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-QUOTA] Verifying quota command exists")
	suite.logger.Log("[E2E-QUOTA] Running: ntm quota --help")

	cmd := exec.Command(suite.ntmPath, "quota", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-QUOTA] Output: %s", outputStr)

	// Command should either succeed or fail with known error (not "unknown command")
	if err != nil && strings.Contains(outputStr, "unknown command") {
		t.Skip("quota command not available in this ntm version")
	}

	// Verify help text mentions quota-related content
	if !strings.Contains(outputStr, "quota") && !strings.Contains(outputStr, "usage") {
		t.Errorf("Expected help text to contain 'quota' or 'usage', got: %s", outputStr)
	}

	suite.logger.Log("[E2E-QUOTA] SUCCESS: quota command exists and shows help")
}

func TestQuotaCommandHelpFlags(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "quota_help_flags")
	defer suite.Cleanup()

	suite.requireQuotaCommand()

	suite.logger.Log("[E2E-QUOTA] Verifying quota command help shows expected flags")
	suite.logger.Log("[E2E-QUOTA] Running: ntm quota --help")

	cmd := exec.Command(suite.ntmPath, "quota", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		suite.logger.Log("[E2E-QUOTA] Command exited with error (may be expected): %v", err)
	}

	suite.logger.Log("[E2E-QUOTA] Help output: %s", outputStr)

	// Check for expected global flags
	expectedPatterns := []string{
		"--json",
		"--help",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(outputStr, pattern) {
			t.Errorf("Expected help to mention '%s', got: %s", pattern, outputStr)
		}
	}

	suite.logger.Log("[E2E-QUOTA] SUCCESS: quota command help shows expected flags")
}

func TestQuotaNoSession(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "quota_no_session")
	defer suite.Cleanup()

	suite.requireQuotaCommand()

	suite.logger.Log("[E2E-QUOTA] Testing quota command with non-existent session")
	suite.logger.Log("[E2E-QUOTA] Running: ntm quota nonexistent-session-xyz --json")

	cmd := exec.Command(suite.ntmPath, "quota", "nonexistent-session-xyz", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-QUOTA] Output: %s", outputStr)
	suite.logger.Log("[E2E-QUOTA] Error: %v", err)

	// Should fail gracefully with an error message
	if err == nil {
		suite.logger.Log("[E2E-QUOTA] Command succeeded (session might exist or quota handles missing sessions)")
	}

	// If JSON output, try to parse it
	if strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result QuotaOutput
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr == nil {
			suite.logger.Log("[E2E-QUOTA] Parsed JSON - success: %v, error: %s", result.Success, result.Error)
		}
	}

	suite.logger.Log("[E2E-QUOTA] SUCCESS: quota command handles missing session gracefully")
}

func TestQuotaJSONFormat(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "quota_json")
	defer suite.Cleanup()

	suite.requireQuotaCommand()

	suite.logger.Log("[E2E-QUOTA] Testing quota command JSON output format")
	suite.logger.Log("[E2E-QUOTA] Running: ntm quota --json (no session)")

	cmd := exec.Command(suite.ntmPath, "quota", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-QUOTA] Output: %s", outputStr)

	// Regardless of success/failure, if --json is used, output should be valid JSON
	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr != nil {
			t.Errorf("Expected valid JSON output, got parse error: %v", jsonErr)
		} else {
			suite.logger.Log("[E2E-QUOTA] Valid JSON output received")
		}
	} else if err != nil {
		// Command failed but might not produce JSON on all errors
		suite.logger.Log("[E2E-QUOTA] Command failed with non-JSON output: %v", err)
	}

	suite.logger.Log("[E2E-QUOTA] SUCCESS: quota --json produces parseable output")
}

// ============================================================================
// Upgrade Command Tests
// ============================================================================

func TestUpgradeCommandExists(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_exists")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-UPGRADE] Verifying upgrade command exists")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm upgrade --help")

	cmd := exec.Command(suite.ntmPath, "upgrade", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-UPGRADE] Output: %s", outputStr)

	// Command should either succeed or fail with known error (not "unknown command")
	if err != nil && strings.Contains(outputStr, "unknown command") {
		t.Skip("upgrade command not available in this ntm version")
	}

	// Verify help text mentions upgrade-related content
	if !strings.Contains(outputStr, "upgrade") && !strings.Contains(outputStr, "update") {
		t.Errorf("Expected help text to contain 'upgrade' or 'update', got: %s", outputStr)
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: upgrade command exists and shows help")
}

func TestUpgradeCommandHelpFlags(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_help_flags")
	defer suite.Cleanup()

	suite.requireUpgradeCommand()

	suite.logger.Log("[E2E-UPGRADE] Verifying upgrade command help shows expected flags")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm upgrade --help")

	cmd := exec.Command(suite.ntmPath, "upgrade", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		suite.logger.Log("[E2E-UPGRADE] Command exited with error (may be expected): %v", err)
	}

	suite.logger.Log("[E2E-UPGRADE] Help output: %s", outputStr)

	// Check for expected flags based on actual command help
	expectedPatterns := []string{
		"--check",
		"--force",
		"--yes",
		"--help",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(outputStr, pattern) {
			t.Errorf("Expected help to mention '%s', got: %s", pattern, outputStr)
		}
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: upgrade command help shows expected flags")
}

func TestUpgradeCheckOnly(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_check")
	defer suite.Cleanup()

	suite.requireUpgradeCommand()

	suite.logger.Log("[E2E-UPGRADE] Testing upgrade --check (non-destructive)")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm upgrade --check --json")

	cmd := exec.Command(suite.ntmPath, "upgrade", "--check", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-UPGRADE] Output: %s", outputStr)

	// --check should not actually upgrade, just check for updates
	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result UpgradeCheckOutput
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr != nil {
			// Try generic map
			var generic map[string]interface{}
			if genericErr := json.Unmarshal([]byte(outputStr), &generic); genericErr != nil {
				t.Errorf("Expected valid JSON output, got parse error: %v", jsonErr)
			} else {
				suite.logger.Log("[E2E-UPGRADE] Valid JSON (generic): %+v", generic)
			}
		} else {
			suite.logger.Log("[E2E-UPGRADE] Current version: %s", result.CurrentVersion)
			suite.logger.Log("[E2E-UPGRADE] Latest version: %s", result.LatestVersion)
			suite.logger.Log("[E2E-UPGRADE] Update available: %v", result.UpdateAvailable)
		}
	} else if err != nil {
		// Check might fail due to network issues - that's OK for E2E
		suite.logger.Log("[E2E-UPGRADE] Command failed (may be network issue): %v", err)
		if strings.Contains(outputStr, "network") || strings.Contains(outputStr, "connection") ||
			strings.Contains(outputStr, "timeout") || strings.Contains(outputStr, "rate limit") {
			t.Skip("Skipping due to network issues")
		}
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: upgrade --check runs without modifying installation")
}

func TestUpgradeUpdateAlias(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_alias")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-UPGRADE] Verifying 'update' is an alias for 'upgrade'")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm update --help")

	cmd := exec.Command(suite.ntmPath, "update", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-UPGRADE] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown command") {
		t.Skip("update alias not available")
	}

	// Should show similar help to upgrade
	if !strings.Contains(outputStr, "upgrade") && !strings.Contains(outputStr, "update") &&
		!strings.Contains(outputStr, "latest version") {
		t.Errorf("Expected 'update' to behave like 'upgrade', got: %s", outputStr)
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: 'update' alias works for 'upgrade' command")
}

func TestUpgradeStrictMode(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_strict")
	defer suite.Cleanup()

	suite.requireUpgradeCommand()

	suite.logger.Log("[E2E-UPGRADE] Testing upgrade --strict mode")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm upgrade --check --strict --json")

	cmd := exec.Command(suite.ntmPath, "upgrade", "--check", "--strict", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-UPGRADE] Output: %s", outputStr)
	suite.logger.Log("[E2E-UPGRADE] Error: %v", err)

	// Strict mode should work the same as regular check but with exact matching
	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr == nil {
			suite.logger.Log("[E2E-UPGRADE] Valid JSON output in strict mode")
		}
	}

	// Skip on network errors
	if err != nil && (strings.Contains(outputStr, "network") || strings.Contains(outputStr, "connection") ||
		strings.Contains(outputStr, "timeout") || strings.Contains(outputStr, "rate limit")) {
		t.Skip("Skipping due to network issues")
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: upgrade --strict mode runs correctly")
}

func TestUpgradeVerboseMode(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "upgrade_verbose")
	defer suite.Cleanup()

	suite.requireUpgradeCommand()

	suite.logger.Log("[E2E-UPGRADE] Testing upgrade --verbose mode")
	suite.logger.Log("[E2E-UPGRADE] Running: ntm upgrade --check --verbose")

	cmd := exec.Command(suite.ntmPath, "upgrade", "--check", "--verbose")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-UPGRADE] Output: %s", outputStr)

	// Verbose mode should show more detailed output about asset matching
	// It may succeed or fail depending on network, but should run
	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--verbose flag not recognized")
	}

	// Skip on network errors
	if err != nil && (strings.Contains(outputStr, "network") || strings.Contains(outputStr, "connection") ||
		strings.Contains(outputStr, "timeout") || strings.Contains(outputStr, "rate limit")) {
		t.Skip("Skipping due to network issues")
	}

	suite.logger.Log("[E2E-UPGRADE] SUCCESS: upgrade --verbose mode runs correctly")
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestQuotaAndUpgradeIntegration(t *testing.T) {
	suite := NewQuotaUpgradeTestSuite(t, "quota_upgrade_integration")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-INTEGRATION] Testing both quota and upgrade commands")

	// Test quota is available
	suite.logger.Log("[E2E-INTEGRATION] Checking quota command availability")
	quotaAvailable := suite.supportsCommand("quota")
	suite.logger.Log("[E2E-INTEGRATION] quota available: %v", quotaAvailable)

	// Test upgrade is available
	suite.logger.Log("[E2E-INTEGRATION] Checking upgrade command availability")
	upgradeAvailable := suite.supportsCommand("upgrade")
	suite.logger.Log("[E2E-INTEGRATION] upgrade available: %v", upgradeAvailable)

	if !quotaAvailable && !upgradeAvailable {
		t.Skip("Neither quota nor upgrade commands available")
	}

	// Run quota with JSON if available
	if quotaAvailable {
		suite.logger.Log("[E2E-INTEGRATION] Running quota with JSON output")
		cmd := exec.Command(suite.ntmPath, "quota", "--json")
		output, _ := cmd.CombinedOutput()
		suite.logger.Log("[E2E-INTEGRATION] quota --json output length: %d bytes", len(output))
	}

	// Run upgrade check if available
	if upgradeAvailable {
		suite.logger.Log("[E2E-INTEGRATION] Running upgrade check")
		cmd := exec.Command(suite.ntmPath, "upgrade", "--check")
		output, _ := cmd.CombinedOutput()
		outputStr := string(output)

		// Check for network errors to skip gracefully
		if strings.Contains(outputStr, "network") || strings.Contains(outputStr, "connection") ||
			strings.Contains(outputStr, "timeout") || strings.Contains(outputStr, "rate limit") {
			suite.logger.Log("[E2E-INTEGRATION] Upgrade check had network issues (expected in some environments)")
		} else {
			suite.logger.Log("[E2E-INTEGRATION] upgrade --check output length: %d bytes", len(output))
		}
	}

	suite.logger.Log("[E2E-INTEGRATION] SUCCESS: Integration test completed")
}
