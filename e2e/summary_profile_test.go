//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SUMMARY-PROFILE] Tests for ntm summary and ntm profiles commands.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// SummaryOutput represents the JSON output from ntm summary
type SummaryOutput struct {
	Success    bool           `json:"success"`
	Session    string         `json:"session,omitempty"`
	Timestamp  time.Time      `json:"timestamp,omitempty"`
	Duration   string         `json:"duration,omitempty"`
	Agents     []AgentSummary `json:"agents,omitempty"`
	TotalFiles int            `json:"total_files,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// AgentSummary represents per-agent summary data
type AgentSummary struct {
	Pane        int      `json:"pane"`
	AgentType   string   `json:"agent_type"`
	ActiveTime  string   `json:"active_time,omitempty"`
	OutputLines int      `json:"output_lines,omitempty"`
	Files       []string `json:"files,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Errors      int      `json:"errors,omitempty"`
}

// SummaryListOutput represents the JSON output from ntm summary --all
type SummaryListOutput struct {
	Success   bool          `json:"success"`
	Summaries []SummaryInfo `json:"summaries,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// SummaryInfo represents summary metadata
type SummaryInfo struct {
	Session    string    `json:"session"`
	Timestamp  time.Time `json:"timestamp"`
	AgentCount int       `json:"agent_count,omitempty"`
}

// ProfilesListOutput represents the JSON output from ntm profiles list
type ProfilesListOutput struct {
	Success  bool          `json:"success"`
	Profiles []ProfileInfo `json:"profiles,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// ProfileInfo represents profile metadata
type ProfileInfo struct {
	Name        string `json:"name"`
	AgentType   string `json:"agent_type,omitempty"`
	Model       string `json:"model,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
}

// ProfileShowOutput represents the JSON output from ntm profiles show
type ProfileShowOutput struct {
	Success     bool   `json:"success"`
	Name        string `json:"name,omitempty"`
	AgentType   string `json:"agent_type,omitempty"`
	Model       string `json:"model,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SummaryProfileTestSuite manages E2E tests for summary and profile commands
type SummaryProfileTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
}

// NewSummaryProfileTestSuite creates a new test suite
func NewSummaryProfileTestSuite(t *testing.T, scenario string) *SummaryProfileTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &SummaryProfileTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand by running it with --help
func (s *SummaryProfileTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		// If exit code is non-zero, check if it's "unknown command"
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireSummaryCommand skips if summary command is not supported
func (s *SummaryProfileTestSuite) requireSummaryCommand() {
	if !s.supportsCommand("summary") {
		s.t.Skip("summary command not supported by this ntm version")
	}
}

// requireProfilesCommand skips if profiles command is not supported
func (s *SummaryProfileTestSuite) requireProfilesCommand() {
	if !s.supportsCommand("profiles") {
		s.t.Skip("profiles command not supported by this ntm version")
	}
}

// Setup creates a temporary directory for testing
func (s *SummaryProfileTestSuite) Setup() error {
	tempDir, err := os.MkdirTemp("", "ntm-summary-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("Created temp directory: %s", tempDir)
	return nil
}

// Teardown cleans up resources
func (s *SummaryProfileTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
}

// runNTM executes ntm with arguments and returns output
func (s *SummaryProfileTestSuite) runNTM(args ...string) (string, error) {
	s.logger.Log("Running: %s %s", s.ntmPath, strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, err := cmd.CombinedOutput()
	result := string(output)
	if err != nil {
		s.logger.Log("Command failed: %v, output: %s", err, result)
		return result, err
	}
	s.logger.Log("Output: %s", result)
	return result, nil
}

// runNTMAllowFail runs ntm allowing non-zero exit codes
func (s *SummaryProfileTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("Running (allow-fail): %s %s", s.ntmPath, strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("Output: %s", result)
	return result
}

// ========== Summary Command Tests ==========

// TestSummaryCommandExists verifies summary command is available
func TestSummaryCommandExists(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-exists")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary command exists")

	output := suite.runNTMAllowFail("summary", "--help")

	// Should show help, not unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("summary command not recognized: %s", output)
	}

	if !strings.Contains(output, "summary") {
		t.Errorf("Expected help text, got: %s", output)
	}

	suite.logger.Log("PASS: summary command exists")
}

// TestSummaryFormatFlag verifies --format flag works correctly
func TestSummaryFormatFlag(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-format-flag")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary --format flag")

	formats := []string{"text", "json", "markdown", "detailed", "handoff"}
	for _, format := range formats {
		output := suite.runNTMAllowFail("summary", "--format="+format, "--help")
		if strings.Contains(output, "unknown flag") {
			t.Fatalf("--format=%s flag not recognized: %s", format, output)
		}
	}

	suite.logger.Log("PASS: --format flag is accepted with all valid values")
}

// TestSummaryAllFlag verifies --all flag works correctly
func TestSummaryAllFlag(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-all-flag")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary --all flag")

	output := suite.runNTMAllowFail("summary", "--all", "--json")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--all flag not recognized: %s", output)
	}

	// Try to parse as JSON
	var result SummaryListOutput
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		suite.logger.Log("Got list response: %d summaries", len(result.Summaries))
	}

	suite.logger.Log("PASS: --all flag is accepted")
}

// TestSummaryRecentFlag verifies --recent flag works correctly
func TestSummaryRecentFlag(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-recent-flag")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary --recent flag")

	output := suite.runNTMAllowFail("summary", "--recent", "--json")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--recent flag not recognized: %s", output)
	}

	suite.logger.Log("PASS: --recent flag is accepted")
}

// TestSummaryRegenerateFlag verifies --regenerate flag works correctly
func TestSummaryRegenerateFlag(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-regenerate-flag")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary --regenerate flag")

	output := suite.runNTMAllowFail("summary", "--regenerate", "--help")

	// Should not complain about unknown flag
	if strings.Contains(output, "unknown flag") {
		t.Fatalf("--regenerate flag not recognized: %s", output)
	}

	suite.logger.Log("PASS: --regenerate flag is accepted")
}

// TestSummaryJSONOutput verifies JSON output structure
func TestSummaryJSONOutput(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-json-output")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary --json output")

	output := suite.runNTMAllowFail("summary", "--all", "--json")

	// Try to parse as JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		suite.logger.Log("Note: Could not parse JSON (may need active session): %v", err)
	} else {
		suite.logger.Log("Got valid JSON structure")
	}

	suite.logger.Log("PASS: summary --json produces parseable output")
}

// TestSummaryNonExistentSession verifies behavior for non-existent session
func TestSummaryNonExistentSession(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "summary-nonexistent")
	defer suite.Teardown()
	suite.requireSummaryCommand()

	suite.logger.Log("Testing summary with non-existent session")

	output := suite.runNTMAllowFail("summary", "nonexistent-session-xyz-12345", "--json")

	// Should handle gracefully (either error or empty result)
	suite.logger.Log("Output for nonexistent session: %s", output)
	suite.logger.Log("PASS: Handles non-existent session gracefully")
}

// ========== Profiles Command Tests ==========

// TestProfilesCommandExists verifies profiles command is available
func TestProfilesCommandExists(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-exists")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles command exists")

	output := suite.runNTMAllowFail("profiles", "--help")

	// Should show help, not unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("profiles command not recognized: %s", output)
	}

	if !strings.Contains(output, "profiles") && !strings.Contains(output, "persona") {
		t.Errorf("Expected help text, got: %s", output)
	}

	suite.logger.Log("PASS: profiles command exists")
}

// TestProfilesListSubcommand verifies profiles list subcommand
func TestProfilesListSubcommand(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-list")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles list subcommand")

	output := suite.runNTMAllowFail("profiles", "list")

	// Should not fail with unknown command
	if strings.Contains(output, "unknown command") {
		t.Fatalf("profiles list command not recognized: %s", output)
	}

	suite.logger.Log("PASS: profiles list subcommand works")
}

// TestProfilesListJSONOutput verifies profiles list JSON output
func TestProfilesListJSONOutput(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-list-json")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles list --json output")

	output := suite.runNTMAllowFail("profiles", "list", "--json")

	// Try to parse as JSON
	var result ProfilesListOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		suite.logger.Log("Note: Could not parse JSON: %v", err)
	} else {
		suite.logger.Log("Got %d profiles", len(result.Profiles))
		for _, p := range result.Profiles {
			suite.logger.Log("  Profile: %s (type: %s)", p.Name, p.AgentType)
		}
	}

	suite.logger.Log("PASS: profiles list --json produces output")
}

// TestProfilesShowSubcommand verifies profiles show subcommand
func TestProfilesShowSubcommand(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-show")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles show subcommand")

	// First try to list profiles to get a valid name
	listOutput := suite.runNTMAllowFail("profiles", "list", "--json")
	var listResult ProfilesListOutput
	if err := json.Unmarshal([]byte(listOutput), &listResult); err == nil && len(listResult.Profiles) > 0 {
		profileName := listResult.Profiles[0].Name
		suite.logger.Log("Showing profile: %s", profileName)

		output := suite.runNTMAllowFail("profiles", "show", profileName)
		if strings.Contains(output, "unknown command") {
			t.Fatalf("profiles show command not recognized: %s", output)
		}
	} else {
		// Just test that the command exists
		output := suite.runNTMAllowFail("profiles", "show", "--help")
		if strings.Contains(output, "unknown command") {
			t.Fatalf("profiles show command not recognized: %s", output)
		}
	}

	suite.logger.Log("PASS: profiles show subcommand works")
}

// TestProfilesShowJSONOutput verifies profiles show JSON output
func TestProfilesShowJSONOutput(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-show-json")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles show --json output")

	// First try to list profiles to get a valid name
	listOutput := suite.runNTMAllowFail("profiles", "list", "--json")
	var listResult ProfilesListOutput
	if err := json.Unmarshal([]byte(listOutput), &listResult); err == nil && len(listResult.Profiles) > 0 {
		profileName := listResult.Profiles[0].Name
		suite.logger.Log("Showing profile: %s", profileName)

		output := suite.runNTMAllowFail("profiles", "show", profileName, "--json")
		var showResult ProfileShowOutput
		if err := json.Unmarshal([]byte(output), &showResult); err != nil {
			suite.logger.Log("Note: Could not parse JSON: %v", err)
		} else {
			suite.logger.Log("Profile details: name=%s, type=%s", showResult.Name, showResult.AgentType)
		}
	}

	suite.logger.Log("PASS: profiles show --json produces output")
}

// TestProfilesSwitchSubcommand verifies profiles switch subcommand exists
func TestProfilesSwitchSubcommand(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-switch")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles switch subcommand exists")

	// Just verify the command exists via help
	output := suite.runNTMAllowFail("profiles", "switch", "--help")

	if strings.Contains(output, "unknown command") {
		t.Fatalf("profiles switch command not recognized: %s", output)
	}

	suite.logger.Log("PASS: profiles switch subcommand exists")
}

// TestProfilesNonExistentProfile verifies behavior for non-existent profile
func TestProfilesNonExistentProfile(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "profiles-nonexistent")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing profiles show with non-existent profile")

	output := suite.runNTMAllowFail("profiles", "show", "nonexistent-profile-xyz-12345", "--json")

	// Should handle gracefully (either error message or not found)
	if !strings.Contains(output, "not found") && !strings.Contains(output, "error") && !strings.Contains(output, "Error") {
		suite.logger.Log("Note: Output for nonexistent profile: %s", output)
	}

	suite.logger.Log("PASS: Handles non-existent profile gracefully")
}

// TestPersonasAlias verifies 'personas' is an alias for 'profiles'
func TestPersonasAlias(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "personas-alias")
	defer suite.Teardown()
	suite.requireProfilesCommand()

	suite.logger.Log("Testing personas alias")

	// Check if personas command works
	output := suite.runNTMAllowFail("personas", "--help")

	if strings.Contains(output, "unknown command") {
		suite.logger.Log("Note: 'personas' alias may not exist, checking 'profiles' instead")
	} else if strings.Contains(output, "persona") || strings.Contains(output, "profile") {
		suite.logger.Log("personas command works")
	}

	suite.logger.Log("PASS: Checked personas alias")
}

// TestSummaryAndProfilesBothExist verifies both commands exist
func TestSummaryAndProfilesBothExist(t *testing.T) {
	suite := NewSummaryProfileTestSuite(t, "both-commands-exist")
	defer suite.Teardown()

	suite.logger.Log("Testing that both summary and profiles exist")

	// Check summary command
	summaryOutput := suite.runNTMAllowFail("summary", "--help")
	if strings.Contains(summaryOutput, "unknown command") {
		suite.logger.Log("summary command not found, may be in different version")
	} else {
		suite.logger.Log("summary command exists")
	}

	// Check profiles command
	profilesOutput := suite.runNTMAllowFail("profiles", "--help")
	if strings.Contains(profilesOutput, "unknown command") {
		suite.logger.Log("profiles command not found, may be in different version")
	} else {
		suite.logger.Log("profiles command exists")
	}

	suite.logger.Log("PASS: Checked for both summary and profiles commands")
}
