//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-PREFLIGHT] Tests for prompt preflight validation: secret detection, destructive commands.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Synthetic test fixtures for preflight detection (never use real keys).
// These patterns match the internal/lint and internal/redaction detection rules.
const (
	// Anthropic API key format: sk-ant-api03-[50+ chars]
	preflightFakeAnthropicKey = "sk-ant-api03-FAKEtestkey12345678901234567890123456789012345678901234"
	// OpenAI project key format: sk-proj-[40+ chars]
	preflightFakeOpenAIKey = "sk-proj-FAKEtestkey1234567890123456789012345678901234"
	// GitHub personal token format: ghp_[30+ chars]
	preflightFakeGitHubToken = "ghp_FAKEtesttokenvalue12345678901234567"
	// AWS Access Key format: AKIA[16 chars]
	preflightFakeAWSKey = "AKIAFAKETEST12345678"
	// JWT format: eyJ[base64].eyJ[base64].[signature]
	preflightFakeJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
)

// PreflightResult represents the JSON output from ntm preflight --json.
type PreflightResult struct {
	Success         bool               `json:"success"`
	Timestamp       string             `json:"timestamp"`
	Version         string             `json:"version"`
	PreviewHash     string             `json:"preview_hash"`
	PreviewLen      int                `json:"preview_len"`
	EstimatedTokens int                `json:"estimated_tokens"`
	Findings        []PreflightFinding `json:"findings"`
	ErrorCount      int                `json:"error_count"`
	WarningCount    int                `json:"warning_count"`
	InfoCount       int                `json:"info_count"`
	Preview         string             `json:"preview,omitempty"`
	DCGAvailable    bool               `json:"dcg_available"`
	Error           string             `json:"error,omitempty"`
	ErrorCode       string             `json:"error_code,omitempty"`
}

// PreflightFinding represents a single finding from preflight.
type PreflightFinding struct {
	ID       string                 `json:"id"`
	Severity string                 `json:"severity"`
	Message  string                 `json:"message"`
	Help     string                 `json:"help"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Start    int                    `json:"start,omitempty"`
	End      int                    `json:"end,omitempty"`
	Line     int                    `json:"line,omitempty"`
}

// PreflightTestSuite manages E2E tests for preflight functionality.
type PreflightTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
}

// NewPreflightTestSuite creates a new preflight test suite.
func NewPreflightTestSuite(t *testing.T, scenario string) *PreflightTestSuite {
	t.Helper()
	SkipIfNoNTM(t)

	tempDir := t.TempDir()
	logger := NewTestLogger(t, "preflight-"+scenario)

	suite := &PreflightTestSuite{
		t:       t,
		logger:  logger,
		tempDir: tempDir,
		cleanup: make([]func(), 0),
	}

	t.Cleanup(func() {
		logger.Close()
		for _, fn := range suite.cleanup {
			fn()
		}
	})

	return suite
}

// runPreflight runs the preflight command and returns parsed results.
func (s *PreflightTestSuite) runPreflight(prompt string, strict bool) (*PreflightResult, string, string, error) {
	args := []string{"preflight", prompt, "--json"}
	if strict {
		args = append(args, "--strict")
	}

	s.logger.Log("[E2E-PREFLIGHT] Running: ntm %s", strings.Join(args, " "))
	s.logger.Log("[E2E-PREFLIGHT] Prompt (truncated): %s", truncateForLog(prompt, 100))

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Env = append(os.Environ(), "HOME="+s.tempDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	s.logger.Log("[E2E-PREFLIGHT] stdout: %s", stdoutStr)
	if stderrStr != "" {
		s.logger.Log("[E2E-PREFLIGHT] stderr: %s", stderrStr)
	}
	if err != nil {
		s.logger.Log("[E2E-PREFLIGHT] error: %v", err)
	}

	// Parse JSON output
	var result PreflightResult
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &result); jsonErr != nil {
		s.logger.Log("[E2E-PREFLIGHT] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-PREFLIGHT] Result", result)
	return &result, stdoutStr, stderrStr, err
}

// findingByID returns the first finding with the given ID.
func (s *PreflightTestSuite) findingByID(result *PreflightResult, id string) *PreflightFinding {
	for _, f := range result.Findings {
		if f.ID == id {
			return &f
		}
	}
	return nil
}

// hasAnyFindingWithID returns true if any finding has the given ID.
func (s *PreflightTestSuite) hasAnyFindingWithID(result *PreflightResult, id string) bool {
	return s.findingByID(result, id) != nil
}

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestPreflightSecretDetection_AnthropicKey tests detection of Anthropic API keys.
func TestPreflightSecretDetection_AnthropicKey(t *testing.T) {
	suite := NewPreflightTestSuite(t, "secret-anthropic")

	prompt := "Please use this API key: " + preflightFakeAnthropicKey + " for authentication."

	result, stdout, stderr, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: Anthropic key detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: secret-anthropic-%d", time.Now().Unix())
	suite.logger.Log("[E2E-PREFLIGHT] command: ntm preflight <prompt> --json")
	suite.logger.Log("[E2E-PREFLIGHT] full_stdout: %s", stdout)
	suite.logger.Log("[E2E-PREFLIGHT] full_stderr: %s", stderr)

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify secret was detected
	if !suite.hasAnyFindingWithID(result, "secret_detected") {
		t.Errorf("[E2E-PREFLIGHT] Expected secret_detected finding for Anthropic key")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	// Verify warning count
	if result.WarningCount == 0 && result.ErrorCount == 0 {
		t.Errorf("[E2E-PREFLIGHT] Expected at least 1 warning or error for secret")
	}

	// Verify robot output structure
	if result.PreviewHash == "" {
		t.Errorf("[E2E-PREFLIGHT] Expected non-empty preview_hash")
	}
	if result.PreviewLen == 0 {
		t.Errorf("[E2E-PREFLIGHT] Expected non-zero preview_len")
	}

	suite.logger.Log("[E2E-PREFLIGHT] secret_detection_test_completed: anthropic_key")
}

// TestPreflightSecretDetection_GitHubToken tests detection of GitHub tokens.
func TestPreflightSecretDetection_GitHubToken(t *testing.T) {
	suite := NewPreflightTestSuite(t, "secret-github")

	prompt := "Token: " + preflightFakeGitHubToken + " - use for auth"

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: GitHub token detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: secret-github-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify secret was detected
	if !suite.hasAnyFindingWithID(result, "secret_detected") {
		t.Errorf("[E2E-PREFLIGHT] Expected secret_detected finding for GitHub token")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	// Check category metadata if available
	finding := suite.findingByID(result, "secret_detected")
	if finding != nil && finding.Metadata != nil {
		if cat, ok := finding.Metadata["category"].(string); ok {
			suite.logger.Log("[E2E-PREFLIGHT] detected_category: %s", cat)
		}
	}

	suite.logger.Log("[E2E-PREFLIGHT] secret_detection_test_completed: github_token")
}

// TestPreflightSecretDetection_AWSKey tests detection of AWS access keys.
func TestPreflightSecretDetection_AWSKey(t *testing.T) {
	suite := NewPreflightTestSuite(t, "secret-aws")

	prompt := "AWS credentials:\nACCESS_KEY=" + preflightFakeAWSKey

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: AWS key detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: secret-aws-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify secret was detected
	if !suite.hasAnyFindingWithID(result, "secret_detected") {
		t.Errorf("[E2E-PREFLIGHT] Expected secret_detected finding for AWS key")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	suite.logger.Log("[E2E-PREFLIGHT] secret_detection_test_completed: aws_key")
}

// TestPreflightSecretDetection_JWT tests detection of JWT tokens.
func TestPreflightSecretDetection_JWT(t *testing.T) {
	suite := NewPreflightTestSuite(t, "secret-jwt")

	prompt := "Bearer " + preflightFakeJWT

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: JWT detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: secret-jwt-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify secret was detected
	if !suite.hasAnyFindingWithID(result, "secret_detected") {
		t.Errorf("[E2E-PREFLIGHT] Expected secret_detected finding for JWT")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	suite.logger.Log("[E2E-PREFLIGHT] secret_detection_test_completed: jwt")
}

// TestPreflightDestructive_RmRf tests detection of rm -rf commands.
func TestPreflightDestructive_RmRf(t *testing.T) {
	suite := NewPreflightTestSuite(t, "destructive-rmrf")

	prompt := "To clean up, run rm -rf / to delete everything"

	result, stdout, stderr, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: rm -rf detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: destructive-rmrf-%d", time.Now().Unix())
	suite.logger.Log("[E2E-PREFLIGHT] command: ntm preflight <prompt> --json")
	suite.logger.Log("[E2E-PREFLIGHT] full_stdout: %s", stdout)
	suite.logger.Log("[E2E-PREFLIGHT] full_stderr: %s", stderr)

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify destructive command was detected
	if !suite.hasAnyFindingWithID(result, "destructive_command") {
		t.Errorf("[E2E-PREFLIGHT] Expected destructive_command finding for rm -rf /")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	// In default mode, destructive should be warning (not error)
	finding := suite.findingByID(result, "destructive_command")
	if finding != nil {
		suite.logger.Log("[E2E-PREFLIGHT] finding_severity: %s", finding.Severity)
		if finding.Severity != "warning" {
			t.Errorf("[E2E-PREFLIGHT] Expected warning severity in default mode, got %s", finding.Severity)
		}
	}

	// Success should be true in default mode (warnings don't block)
	if !result.Success {
		t.Errorf("[E2E-PREFLIGHT] Expected success=true in default mode (warnings don't block)")
	}

	suite.logger.Log("[E2E-PREFLIGHT] destructive_test_completed: rm_rf")
}

// TestPreflightDestructive_GitResetHard tests detection of git reset --hard.
func TestPreflightDestructive_GitResetHard(t *testing.T) {
	suite := NewPreflightTestSuite(t, "destructive-git-reset")

	prompt := "Execute git reset --hard HEAD~10 to undo commits"

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: git reset --hard detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: destructive-git-reset-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify destructive command was detected
	if !suite.hasAnyFindingWithID(result, "destructive_command") {
		t.Errorf("[E2E-PREFLIGHT] Expected destructive_command finding for git reset --hard")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	suite.logger.Log("[E2E-PREFLIGHT] destructive_test_completed: git_reset_hard")
}

// TestPreflightDestructive_GitForcePush tests detection of git push --force.
func TestPreflightDestructive_GitForcePush(t *testing.T) {
	suite := NewPreflightTestSuite(t, "destructive-git-force")

	prompt := "Deploy with git push --force origin main"

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: git push --force detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: destructive-git-force-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify destructive command was detected
	if !suite.hasAnyFindingWithID(result, "destructive_command") {
		t.Errorf("[E2E-PREFLIGHT] Expected destructive_command finding for git push --force")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	suite.logger.Log("[E2E-PREFLIGHT] destructive_test_completed: git_force_push")
}

// TestPreflightDestructive_DropTable tests detection of DROP TABLE.
func TestPreflightDestructive_DropTable(t *testing.T) {
	suite := NewPreflightTestSuite(t, "destructive-drop-table")

	prompt := "Clean the database: DROP TABLE users CASCADE;"

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: DROP TABLE detection")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: destructive-drop-table-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify destructive command was detected
	if !suite.hasAnyFindingWithID(result, "destructive_command") {
		t.Errorf("[E2E-PREFLIGHT] Expected destructive_command finding for DROP TABLE")
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	suite.logger.Log("[E2E-PREFLIGHT] destructive_test_completed: drop_table")
}

// TestPreflightStrictMode_BlocksDestructive tests that strict mode escalates warnings to errors.
func TestPreflightStrictMode_BlocksDestructive(t *testing.T) {
	suite := NewPreflightTestSuite(t, "strict-mode")

	prompt := "Run rm -rf /tmp/important to clean cache"

	result, stdout, stderr, err := suite.runPreflight(prompt, true)

	suite.logger.Log("[E2E-PREFLIGHT] Test: strict mode blocks destructive")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: strict-mode-%d", time.Now().Unix())
	suite.logger.Log("[E2E-PREFLIGHT] command: ntm preflight <prompt> --json --strict")
	suite.logger.Log("[E2E-PREFLIGHT] full_stdout: %s", stdout)
	suite.logger.Log("[E2E-PREFLIGHT] full_stderr: %s", stderr)

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify destructive command was detected with error severity
	finding := suite.findingByID(result, "destructive_command")
	if finding == nil {
		t.Fatalf("[E2E-PREFLIGHT] Expected destructive_command finding")
	}

	if finding.Severity != "error" {
		t.Errorf("[E2E-PREFLIGHT] Expected error severity in strict mode, got %s", finding.Severity)
	}

	// Success should be false when there are errors
	if result.Success {
		t.Errorf("[E2E-PREFLIGHT] Expected success=false in strict mode with errors")
	}

	// Error code should be set
	if result.ErrorCode == "" {
		t.Errorf("[E2E-PREFLIGHT] Expected error_code when blocking")
	}
	suite.logger.Log("[E2E-PREFLIGHT] error_code: %s", result.ErrorCode)

	suite.logger.Log("[E2E-PREFLIGHT] strict_mode_test_completed")
}

// TestPreflightBenignPrompt tests that benign prompts pass without findings.
func TestPreflightBenignPrompt(t *testing.T) {
	suite := NewPreflightTestSuite(t, "benign")

	prompt := "Please fix the bug in the auth.go file where the login function fails on timeout."

	result, _, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: benign prompt")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: benign-%d", time.Now().Unix())

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify no concerning findings
	if suite.hasAnyFindingWithID(result, "secret_detected") {
		t.Errorf("[E2E-PREFLIGHT] Unexpected secret_detected for benign prompt")
	}
	if suite.hasAnyFindingWithID(result, "destructive_command") {
		t.Errorf("[E2E-PREFLIGHT] Unexpected destructive_command for benign prompt")
	}

	// Success should be true
	if !result.Success {
		t.Errorf("[E2E-PREFLIGHT] Expected success=true for benign prompt")
	}

	// ErrorCount should be 0
	if result.ErrorCount != 0 {
		t.Errorf("[E2E-PREFLIGHT] Expected error_count=0, got %d", result.ErrorCount)
	}

	suite.logger.Log("[E2E-PREFLIGHT] benign_prompt_test_completed")
}

// TestPreflightSafePatterns tests that safe patterns are not flagged.
func TestPreflightSafePatterns(t *testing.T) {
	suite := NewPreflightTestSuite(t, "safe-patterns")

	// These are safe patterns that should NOT trigger warnings
	safePrompts := []struct {
		name   string
		prompt string
	}{
		{
			name:   "git_force_with_lease",
			prompt: "Push your changes with git push --force-with-lease origin feature",
		},
		{
			name:   "rm_node_modules",
			prompt: "Clean up by running rm -rf node_modules and reinstall",
		},
		{
			name:   "rm_dist",
			prompt: "Delete the build output with rm -rf dist/",
		},
		{
			name:   "git_soft_reset",
			prompt: "Undo the last commit with git reset --soft HEAD~1",
		},
	}

	for _, tc := range safePrompts {
		t.Run(tc.name, func(t *testing.T) {
			result, _, _, err := suite.runPreflight(tc.prompt, false)

			suite.logger.Log("[E2E-PREFLIGHT] Testing safe pattern: %s", tc.name)

			if result == nil {
				t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
			}

			// Verify destructive command was NOT detected
			if suite.hasAnyFindingWithID(result, "destructive_command") {
				t.Errorf("[E2E-PREFLIGHT] Unexpected destructive_command for safe pattern: %s", tc.name)
				suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
			}

			suite.logger.Log("[E2E-PREFLIGHT] safe_pattern_test_completed: %s", tc.name)
		})
	}
}

// TestPreflightRobotOutput tests the robot output format stability.
func TestPreflightRobotOutput(t *testing.T) {
	suite := NewPreflightTestSuite(t, "robot-output")

	prompt := "Test prompt for robot output validation"

	result, stdout, _, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: robot output format")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: robot-output-%d", time.Now().Unix())
	suite.logger.Log("[E2E-PREFLIGHT] raw_json: %s", stdout)

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Verify required robot fields
	if result.Timestamp == "" {
		t.Errorf("[E2E-PREFLIGHT] Missing timestamp field")
	}
	if result.Version == "" {
		t.Errorf("[E2E-PREFLIGHT] Missing version field")
	}
	if result.PreviewHash == "" {
		t.Errorf("[E2E-PREFLIGHT] Missing preview_hash field")
	}
	if result.PreviewLen == 0 {
		t.Errorf("[E2E-PREFLIGHT] Missing or zero preview_len field")
	}
	if result.Findings == nil {
		t.Errorf("[E2E-PREFLIGHT] Missing findings field (should be empty array, not null)")
	}

	// Verify JSON is valid and parseable
	var rawJSON map[string]interface{}
	if parseErr := json.Unmarshal([]byte(stdout), &rawJSON); parseErr != nil {
		t.Errorf("[E2E-PREFLIGHT] Output is not valid JSON: %v", parseErr)
	}

	suite.logger.LogJSON("[E2E-PREFLIGHT] Parsed robot output", rawJSON)
	suite.logger.Log("[E2E-PREFLIGHT] robot_output_test_completed")
}

// TestPreflightHashStability tests that preview hash is deterministic.
func TestPreflightHashStability(t *testing.T) {
	suite := NewPreflightTestSuite(t, "hash-stability")

	prompt := "Identical prompt for hash stability test"

	// Run preflight multiple times
	var hashes []string
	for i := 0; i < 3; i++ {
		result, _, _, err := suite.runPreflight(prompt, false)
		if result == nil {
			t.Fatalf("[E2E-PREFLIGHT] Failed to parse result on iteration %d: %v", i, err)
		}
		hashes = append(hashes, result.PreviewHash)
		suite.logger.Log("[E2E-PREFLIGHT] Iteration %d hash: %s", i, result.PreviewHash)
	}

	// All hashes should be identical
	for i := 1; i < len(hashes); i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("[E2E-PREFLIGHT] Hash mismatch: iteration 0 = %s, iteration %d = %s", hashes[0], i, hashes[i])
		}
	}

	// Different prompt should have different hash
	differentResult, _, _, _ := suite.runPreflight("A completely different prompt", false)
	if differentResult != nil && differentResult.PreviewHash == hashes[0] {
		t.Errorf("[E2E-PREFLIGHT] Different prompts should have different hashes")
	}

	suite.logger.Log("[E2E-PREFLIGHT] hash_stability_test_completed")
}

// TestPreflightMultipleFindings tests handling of prompts with multiple issues.
func TestPreflightMultipleFindings(t *testing.T) {
	suite := NewPreflightTestSuite(t, "multiple-findings")

	// Prompt with both a secret AND a destructive command
	prompt := "Use API key " + preflightFakeOpenAIKey + " then run rm -rf / to clean up"

	result, stdout, stderr, err := suite.runPreflight(prompt, false)

	suite.logger.Log("[E2E-PREFLIGHT] Test: multiple findings")
	suite.logger.Log("[E2E-PREFLIGHT] test_run_id: multiple-findings-%d", time.Now().Unix())
	suite.logger.Log("[E2E-PREFLIGHT] full_stdout: %s", stdout)
	suite.logger.Log("[E2E-PREFLIGHT] full_stderr: %s", stderr)

	if result == nil {
		t.Fatalf("[E2E-PREFLIGHT] Failed to parse result: %v", err)
	}

	// Should have at least 2 findings
	if len(result.Findings) < 2 {
		t.Errorf("[E2E-PREFLIGHT] Expected at least 2 findings, got %d", len(result.Findings))
		suite.logger.Log("[E2E-PREFLIGHT] findings: %+v", result.Findings)
	}

	// Should have both secret and destructive findings
	hasSecret := suite.hasAnyFindingWithID(result, "secret_detected")
	hasDestructive := suite.hasAnyFindingWithID(result, "destructive_command")

	if !hasSecret {
		t.Errorf("[E2E-PREFLIGHT] Expected secret_detected finding")
	}
	if !hasDestructive {
		t.Errorf("[E2E-PREFLIGHT] Expected destructive_command finding")
	}

	// Log all findings
	for i, f := range result.Findings {
		suite.logger.Log("[E2E-PREFLIGHT] Finding %d: id=%s severity=%s message=%s",
			i, f.ID, f.Severity, f.Message)
	}

	suite.logger.Log("[E2E-PREFLIGHT] multiple_findings_test_completed")
}
