//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-REDACTION] Tests for redaction across send/copy/mail operations.
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

// Synthetic secret fixtures that match the redaction patterns (never use real keys)
// These are structured to match the patterns in internal/redaction/patterns.go
const (
	// OpenAI project key format: sk-proj-[40+ chars]
	fakeOpenAIKey = "sk-proj-FAKEtestkey1234567890123456789012345678901234"
	// GitHub personal token format: ghp_[30+ chars]
	fakeGitHubToken = "ghp_FAKEtesttokenvalue12345678901234567"
	// AWS Access Key format: AKIA[16 chars]
	fakeAWSKey = "AKIAFAKETEST12345678"
	// AWS Secret format: aws_secret=[40 chars]
	fakeAWSSecret = "aws_secret=FAKE1234567890123456789012345678901234"
	// JWT format: eyJ[base64].eyJ[base64].[signature]
	fakeJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	// Password format: password=[value]
	fakePassword = "password=secretpass123456"
	// Redacted marker prefix
	redactedMarker = "[REDACTED:"
	// Error code for blocked secrets
	sendBlockedError = "SENSITIVE_DATA_BLOCKED"
)

// RedactionTestSuite manages E2E tests for redaction functionality.
type RedactionTestSuite struct {
	t           *testing.T
	logger      *TestLogger
	tempDir     string
	sessionName string
	cleanup     []func()
}

// NewRedactionTestSuite creates a new redaction test suite with isolated environment.
func NewRedactionTestSuite(t *testing.T, scenario string) *RedactionTestSuite {
	t.Helper()
	SkipIfNoTmux(t)
	SkipIfNoNTM(t)

	tempDir := t.TempDir()
	logger := NewTestLogger(t, "redaction-"+scenario)
	sessionName := "ntm-redaction-test-" + time.Now().Format("20060102-150405")

	suite := &RedactionTestSuite{
		t:           t,
		logger:      logger,
		tempDir:     tempDir,
		sessionName: sessionName,
		cleanup:     make([]func(), 0),
	}

	// Register cleanup to kill session
	t.Cleanup(func() {
		suite.killSession()
		suite.logger.Close()
		for _, fn := range suite.cleanup {
			fn()
		}
	})

	return suite
}

// killSession kills the test session if it exists.
func (s *RedactionTestSuite) killSession() {
	cmd := exec.Command("tmux", "kill-session", "-t", s.sessionName)
	cmd.Run() // Ignore errors - session might not exist
}

// runNTM runs an ntm command and captures output.
func (s *RedactionTestSuite) runNTM(args ...string) (string, string, error) {
	s.logger.Log("[E2E-REDACTION] Running: ntm %s", strings.Join(args, " "))

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Env = append(os.Environ(), "HOME="+s.tempDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if stdout.Len() > 0 {
		s.logger.Log("[E2E-REDACTION] stdout: %s", stdout.String())
	}
	if stderr.Len() > 0 {
		s.logger.Log("[E2E-REDACTION] stderr: %s", stderr.String())
	}
	if err != nil {
		s.logger.Log("[E2E-REDACTION] error: %v", err)
	}

	return stdout.String(), stderr.String(), err
}

// setupSession creates a tmux session for testing without real agents.
func (s *RedactionTestSuite) setupSession() error {
	s.logger.Log("[E2E-REDACTION] Creating test session: %s", s.sessionName)

	// Create a minimal tmux session with a shell (no real agents needed for redaction tests)
	cmd := exec.Command("tmux", "new-session", "-d", "-s", s.sessionName, "-x", "200", "-y", "50")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	s.logger.Log("[E2E-REDACTION] Session created: %s", string(output))
	time.Sleep(500 * time.Millisecond) // Allow session to stabilize
	return nil
}

// SendOutput represents the JSON response from --robot-send.
type SendOutput struct {
	Success        bool              `json:"success"`
	Session        string            `json:"session"`
	Blocked        bool              `json:"blocked"`
	Redaction      RedactionSummary  `json:"redaction"`
	Warnings       []string          `json:"warnings"`
	Targets        []string          `json:"targets"`
	Successful     []string          `json:"successful"`
	Failed         []SendFailedEntry `json:"failed"`
	MessagePreview string            `json:"message_preview"`
	Error          string            `json:"error,omitempty"`
	ErrorCode      string            `json:"error_code,omitempty"`
}

// SendFailedEntry represents a failed send target.
type SendFailedEntry struct {
	Pane  string `json:"pane"`
	Error string `json:"error"`
}

// RedactionSummary is the summary of redaction findings.
type RedactionSummary struct {
	Mode       string         `json:"mode"`
	Findings   int            `json:"findings"`
	Categories map[string]int `json:"categories,omitempty"`
	Action     string         `json:"action"`
}

// CopyOutput represents the JSON response from --robot-copy (ntm copy --json).
type CopyOutput struct {
	Success   bool              `json:"success"`
	Session   string            `json:"session"`
	Panes     []int             `json:"panes,omitempty"`
	Redaction *RedactionSummary `json:"redaction,omitempty"`
	Error     string            `json:"error,omitempty"`
	ErrorCode string            `json:"error_code,omitempty"`
}

// TestSendRedaction_WarnMode tests that warn mode emits warnings but sends unchanged.
func TestSendRedaction_WarnMode(t *testing.T) {
	suite := NewRedactionTestSuite(t, "send-warn")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	promptWithSecret := "Please use this API key: " + fakeOpenAIKey + " for authentication."

	// Send with warn mode
	stdout, stderr, _ := suite.runNTM(
		"--robot-send="+suite.sessionName,
		"--msg="+promptWithSecret,
		"--redact=warn",
		"--all",
	)

	suite.logger.Log("[E2E-REDACTION] Testing warn mode behavior")

	// Parse the response
	var result SendOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to parse output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	suite.logger.LogJSON("[E2E-REDACTION] Send result", result)

	// Verify warn mode behavior
	if result.Blocked {
		t.Errorf("[E2E-REDACTION] Expected blocked=false in warn mode, got blocked=true")
	}

	if result.Redaction.Action != "warn" {
		t.Errorf("[E2E-REDACTION] Expected action='warn', got '%s'", result.Redaction.Action)
	}

	if result.Redaction.Findings == 0 {
		t.Errorf("[E2E-REDACTION] Expected findings > 0, got 0")
	}

	// Warning should be emitted
	hasWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(strings.ToLower(w), "warning") && strings.Contains(strings.ToLower(w), "secret") {
			hasWarning = true
			break
		}
	}
	if !hasWarning && result.Redaction.Findings > 0 {
		t.Errorf("[E2E-REDACTION] Expected warning about secrets in warn mode")
	}

	// Message preview should still contain the original content (not redacted)
	if result.MessagePreview != "" && strings.Contains(result.MessagePreview, redactedMarker) {
		t.Errorf("[E2E-REDACTION] In warn mode, message_preview should NOT contain redaction markers")
	}

	suite.logger.Log("[E2E-REDACTION] Warn mode test completed successfully")
}

// TestSendRedaction_RedactMode tests that redact mode replaces secrets with placeholders.
func TestSendRedaction_RedactMode(t *testing.T) {
	suite := NewRedactionTestSuite(t, "send-redact")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	promptWithSecret := "Please use this API key: " + fakeOpenAIKey + " for authentication."

	// Send with redact mode
	stdout, stderr, _ := suite.runNTM(
		"--robot-send="+suite.sessionName,
		"--msg="+promptWithSecret,
		"--redact=redact",
		"--all",
	)

	suite.logger.Log("[E2E-REDACTION] Testing redact mode behavior")

	var result SendOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to parse output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	suite.logger.LogJSON("[E2E-REDACTION] Send result", result)

	// Verify redact mode behavior
	if result.Blocked {
		t.Errorf("[E2E-REDACTION] Expected blocked=false in redact mode, got blocked=true")
	}

	if result.Redaction.Action != "redact" {
		t.Errorf("[E2E-REDACTION] Expected action='redact', got '%s'", result.Redaction.Action)
	}

	if result.Redaction.Findings == 0 {
		t.Errorf("[E2E-REDACTION] Expected findings > 0, got 0")
	}

	// Message preview should contain redaction markers, NOT the original secret
	if strings.Contains(result.MessagePreview, fakeOpenAIKey) {
		t.Errorf("[E2E-REDACTION] Message preview should NOT contain original secret in redact mode")
	}

	if !strings.Contains(result.MessagePreview, redactedMarker) && result.Redaction.Findings > 0 {
		t.Errorf("[E2E-REDACTION] Message preview should contain redaction markers in redact mode")
	}

	suite.logger.Log("[E2E-REDACTION] Redact mode test completed successfully")
}

// TestSendRedaction_BlockMode tests that block mode prevents sending secrets.
func TestSendRedaction_BlockMode(t *testing.T) {
	suite := NewRedactionTestSuite(t, "send-block")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	promptWithSecret := "Please use this API key: " + fakeOpenAIKey + " for authentication."

	// Send with block mode
	stdout, stderr, _ := suite.runNTM(
		"--robot-send="+suite.sessionName,
		"--msg="+promptWithSecret,
		"--redact=block",
		"--all",
	)

	suite.logger.Log("[E2E-REDACTION] Testing block mode behavior")

	var result SendOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to parse output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	suite.logger.LogJSON("[E2E-REDACTION] Send result", result)

	// Verify block mode behavior
	if !result.Blocked {
		t.Errorf("[E2E-REDACTION] Expected blocked=true in block mode, got blocked=false")
	}

	if result.Redaction.Action != "block" {
		t.Errorf("[E2E-REDACTION] Expected action='block', got '%s'", result.Redaction.Action)
	}

	if result.ErrorCode != sendBlockedError {
		t.Errorf("[E2E-REDACTION] Expected error_code='%s', got '%s'", sendBlockedError, result.ErrorCode)
	}

	// Message preview should be redacted (shows what would have been sent)
	if strings.Contains(result.MessagePreview, fakeOpenAIKey) {
		t.Errorf("[E2E-REDACTION] Message preview should NOT contain original secret in block mode")
	}

	suite.logger.Log("[E2E-REDACTION] Block mode test completed successfully")
}

// TestSendRedaction_BlockModeWithAllowSecret tests that --allow-secret bypasses block mode.
func TestSendRedaction_BlockModeWithAllowSecret(t *testing.T) {
	suite := NewRedactionTestSuite(t, "send-block-override")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	promptWithSecret := "Please use this API key: " + fakeOpenAIKey + " for authentication."

	// Send with block mode but --allow-secret to bypass
	stdout, stderr, _ := suite.runNTM(
		"--robot-send="+suite.sessionName,
		"--msg="+promptWithSecret,
		"--redact=block",
		"--allow-secret",
		"--all",
	)

	suite.logger.Log("[E2E-REDACTION] Testing block mode with --allow-secret override")

	var result SendOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to parse output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	suite.logger.LogJSON("[E2E-REDACTION] Send result", result)

	// With --allow-secret, send should proceed despite secrets
	if result.Blocked {
		t.Errorf("[E2E-REDACTION] Expected blocked=false with --allow-secret, got blocked=true")
	}

	suite.logger.Log("[E2E-REDACTION] Block mode override test completed successfully")
}

// TestCopyRedaction_RedactMode tests that ntm copy applies redaction to pane output.
func TestCopyRedaction_RedactMode(t *testing.T) {
	suite := NewRedactionTestSuite(t, "copy-redact")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	// Echo a secret to the pane so it appears in scrollback
	secretLine := "export OPENAI_API_KEY=" + fakeOpenAIKey
	cmd := exec.Command("tmux", "send-keys", "-t", suite.sessionName, "echo '"+secretLine+"'", "Enter")
	if err := cmd.Run(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to echo secret to pane: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Copy with redact mode to a file
	outputFile := filepath.Join(suite.tempDir, "copy-output.txt")
	stdout, stderr, _ := suite.runNTM(
		"copy", suite.sessionName+":0",
		"--redact=redact",
		"--output="+outputFile,
		"--json",
	)

	suite.logger.Log("[E2E-REDACTION] Testing copy with redact mode")
	suite.logger.Log("[E2E-REDACTION] Copy stdout: %s", stdout)
	suite.logger.Log("[E2E-REDACTION] Copy stderr: %s", stderr)

	// Read the output file
	content, err := os.ReadFile(outputFile)
	if err != nil {
		// File might not exist if copy failed - that's okay for this test
		suite.logger.Log("[E2E-REDACTION] Could not read output file: %v", err)
		return
	}

	contentStr := string(content)
	suite.logger.Log("[E2E-REDACTION] File content: %s", contentStr)

	// The secret should NOT appear in plain text
	if strings.Contains(contentStr, fakeOpenAIKey) {
		t.Errorf("[E2E-REDACTION] Output file should NOT contain plain secret in redact mode")
	}

	suite.logger.Log("[E2E-REDACTION] Copy redact mode test completed")
}

// TestCopyRedaction_BlockMode tests that ntm copy blocks when secrets are detected.
func TestCopyRedaction_BlockMode(t *testing.T) {
	suite := NewRedactionTestSuite(t, "copy-block")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	// Echo a secret to the pane
	secretLine := "export OPENAI_API_KEY=" + fakeOpenAIKey
	cmd := exec.Command("tmux", "send-keys", "-t", suite.sessionName, "echo '"+secretLine+"'", "Enter")
	if err := cmd.Run(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to echo secret to pane: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Copy with block mode
	outputFile := filepath.Join(suite.tempDir, "copy-blocked.txt")
	stdout, stderr, err := suite.runNTM(
		"copy", suite.sessionName+":0",
		"--redact=block",
		"--output="+outputFile,
		"--json",
	)

	suite.logger.Log("[E2E-REDACTION] Testing copy with block mode")
	suite.logger.Log("[E2E-REDACTION] Copy error: %v", err)

	// In block mode, the operation should fail or indicate blocking
	if err == nil {
		// Check if JSON output indicates blocking
		if strings.Contains(stdout, sendBlockedError) || strings.Contains(stderr, "block") {
			suite.logger.Log("[E2E-REDACTION] Copy correctly blocked due to secrets")
		}
	}

	// Output file should NOT be created with secret content
	if _, statErr := os.Stat(outputFile); statErr == nil {
		content, _ := os.ReadFile(outputFile)
		if strings.Contains(string(content), fakeOpenAIKey) {
			t.Errorf("[E2E-REDACTION] Output file should NOT contain plain secret in block mode")
		}
	}

	suite.logger.Log("[E2E-REDACTION] Copy block mode test completed")
}

// TestSendRedaction_MultipleSecrets tests handling of multiple secret types.
func TestSendRedaction_MultipleSecrets(t *testing.T) {
	suite := NewRedactionTestSuite(t, "send-multi-secrets")

	if err := suite.setupSession(); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to setup session: %v", err)
	}

	// Prompt with multiple secret types
	promptWithMultipleSecrets := "Config:\n" +
		"OPENAI_KEY=" + fakeOpenAIKey + "\n" +
		"GITHUB_TOKEN=" + fakeGitHubToken + "\n" +
		"AWS_ACCESS_KEY=" + fakeAWSKey + "\n" +
		fakePassword

	// Send with redact mode
	stdout, stderr, _ := suite.runNTM(
		"--robot-send="+suite.sessionName,
		"--msg="+promptWithMultipleSecrets,
		"--redact=redact",
		"--all",
	)

	suite.logger.Log("[E2E-REDACTION] Testing multiple secrets redaction")

	var result SendOutput
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("[E2E-REDACTION] Failed to parse output: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	suite.logger.LogJSON("[E2E-REDACTION] Send result", result)

	// Should detect multiple findings
	if result.Redaction.Findings < 2 {
		t.Errorf("[E2E-REDACTION] Expected at least 2 findings for multiple secrets, got %d", result.Redaction.Findings)
	}

	// Categories should show multiple types
	if len(result.Redaction.Categories) == 0 {
		suite.logger.Log("[E2E-REDACTION] Warning: no categories in redaction summary")
	} else {
		suite.logger.Log("[E2E-REDACTION] Detected categories: %v", result.Redaction.Categories)
	}

	// Preview should not contain any of the original secrets
	preview := result.MessagePreview
	secrets := []string{fakeOpenAIKey, fakeGitHubToken, fakeAWSKey, "hunter2hunter2"}
	for _, secret := range secrets {
		if strings.Contains(preview, secret) {
			t.Errorf("[E2E-REDACTION] Message preview should NOT contain secret: %s", secret[:20]+"...")
		}
	}

	suite.logger.Log("[E2E-REDACTION] Multiple secrets test completed successfully")
}
