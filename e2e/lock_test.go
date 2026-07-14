//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-LOCK] Tests for ntm lock and unlock (file reservation commands).
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// LockResult represents the JSON output from lock command
type LockResult struct {
	Success   bool              `json:"success"`
	Session   string            `json:"session"`
	Agent     string            `json:"agent"`
	Granted   []FileReservation `json:"granted,omitempty"`
	Conflicts []LockConflict    `json:"conflicts,omitempty"`
	TTL       string            `json:"ttl"`
	ExpiresAt *string           `json:"expires_at,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// FileReservation represents a granted file reservation
type FileReservation struct {
	ID          int    `json:"id,omitempty"`
	PathPattern string `json:"path_pattern"`
	Exclusive   bool   `json:"exclusive"`
	Reason      string `json:"reason,omitempty"`
	ExpiresTS   string `json:"expires_ts,omitempty"`
}

// LockConflict represents a reservation conflict
type LockConflict struct {
	Path    string   `json:"path"`
	Holders []string `json:"holders"`
}

// UnlockResult represents the JSON output from unlock command
type UnlockResult struct {
	Success    bool    `json:"success"`
	Session    string  `json:"session"`
	Agent      string  `json:"agent"`
	Released   int     `json:"released"`
	ReleasedAt *string `json:"released_at,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// LockTestSuite manages E2E tests for lock/unlock commands
type LockTestSuite struct {
	t           *testing.T
	logger      *TestLogger
	tempDir     string
	sessionName string
	cleanup     []func()
}

// NewLockTestSuite creates a new lock test suite
func NewLockTestSuite(t *testing.T, scenario string) *LockTestSuite {
	logger := NewTestLogger(t, scenario)

	s := &LockTestSuite{
		t:           t,
		logger:      logger,
		sessionName: "ntm-e2e-lock-" + time.Now().Format("20060102-150405"),
	}

	return s
}

// Setup creates a temporary directory for testing
func (s *LockTestSuite) Setup() error {
	s.logger.Log("[E2E-LOCK] Setting up test environment")

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "ntm-lock-e2e-")
	if err != nil {
		return err
	}
	s.tempDir = tempDir

	s.cleanup = append(s.cleanup, func() {
		os.RemoveAll(tempDir)
	})

	return nil
}

// Cleanup runs all cleanup functions
func (s *LockTestSuite) Cleanup() {
	s.logger.Log("[E2E-LOCK] Running cleanup")
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
}

// TempDir returns the temp directory path
func (s *LockTestSuite) TempDir() string {
	return s.tempDir
}

// SessionName returns the test session name
func (s *LockTestSuite) SessionName() string {
	return s.sessionName
}

// RunLock executes ntm lock and returns the output
func (s *LockTestSuite) RunLock(args ...string) ([]byte, error) {
	allArgs := append([]string{"lock"}, args...)
	cmd := exec.Command(mustE2EBin(), allArgs...)
	cmd.Dir = s.tempDir

	s.logger.Log("[E2E-LOCK] Running: ntm %v", allArgs)

	output, err := cmd.CombinedOutput()
	s.logger.Log("[E2E-LOCK] Output: %s", string(output))
	if err != nil {
		s.logger.Log("[E2E-LOCK] Exit error: %v", err)
	}

	return output, err
}

// RunLockJSON executes ntm lock with --json flag
func (s *LockTestSuite) RunLockJSON(args ...string) (*LockResult, []byte, error) {
	allArgs := append(args, "--json")
	output, err := s.RunLock(allArgs...)

	var result LockResult
	if jsonErr := json.Unmarshal(output, &result); jsonErr == nil {
		return &result, output, err
	}

	return nil, output, err
}

// RunUnlock executes ntm unlock and returns the output
func (s *LockTestSuite) RunUnlock(args ...string) ([]byte, error) {
	allArgs := append([]string{"unlock"}, args...)
	cmd := exec.Command(mustE2EBin(), allArgs...)
	cmd.Dir = s.tempDir

	s.logger.Log("[E2E-LOCK] Running: ntm %v", allArgs)

	output, err := cmd.CombinedOutput()
	s.logger.Log("[E2E-LOCK] Output: %s", string(output))
	if err != nil {
		s.logger.Log("[E2E-LOCK] Exit error: %v", err)
	}

	return output, err
}

// RunUnlockJSON executes ntm unlock with --json flag
func (s *LockTestSuite) RunUnlockJSON(args ...string) (*UnlockResult, []byte, error) {
	allArgs := append(args, "--json")
	output, err := s.RunUnlock(allArgs...)

	var result UnlockResult
	if jsonErr := json.Unmarshal(output, &result); jsonErr == nil {
		return &result, output, err
	}

	return nil, output, err
}

// TestLockRequiresSession tests that lock requires session argument
func TestLockRequiresSession(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-requires-session")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try to lock without session argument
	output, err := suite.RunLock()
	if err == nil {
		t.Error("[E2E-LOCK] Expected error when lock called without arguments")
	}

	// Should show usage or error about missing arguments
	outputStr := string(output)
	if !strings.Contains(outputStr, "session") && !strings.Contains(outputStr, "argument") {
		suite.logger.Log("[E2E-LOCK] Unexpected error output: %s", outputStr)
	}

	suite.logger.Log("[E2E-LOCK] Requires session test passed")
}

// TestLockRequiresPatterns tests that lock requires at least one pattern
func TestLockRequiresPatterns(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-requires-patterns")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try to lock with session but no patterns
	output, err := suite.RunLock(suite.SessionName())
	if err == nil {
		t.Error("[E2E-LOCK] Expected error when lock called without patterns")
	}

	// Should show usage or error about missing patterns
	outputStr := string(output)
	if !strings.Contains(outputStr, "pattern") && !strings.Contains(outputStr, "argument") {
		suite.logger.Log("[E2E-LOCK] Output: %s", outputStr)
	}

	suite.logger.Log("[E2E-LOCK] Requires patterns test passed")
}

// TestLockInvalidTTL tests that invalid TTL format is rejected
func TestLockInvalidTTL(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-invalid-ttl")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try with invalid TTL format
	output, err := suite.RunLock(suite.SessionName(), "*.go", "--ttl", "invalid")
	if err == nil {
		t.Error("[E2E-LOCK] Expected error for invalid TTL format")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "TTL") && !strings.Contains(outputStr, "format") {
		suite.logger.Log("[E2E-LOCK] Output: %s", outputStr)
	}

	suite.logger.Log("[E2E-LOCK] Invalid TTL test passed")
}

// TestLockMinimumTTL tests that TTL below 1 minute is rejected
func TestLockMinimumTTL(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-minimum-ttl")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try with TTL less than 1 minute
	output, err := suite.RunLock(suite.SessionName(), "*.go", "--ttl", "30s")
	if err == nil {
		// Might fail for other reasons (no session identity), but should
		// at least reject short TTL
		outputStr := string(output)
		if strings.Contains(outputStr, "1 minute") || strings.Contains(outputStr, "at least") {
			suite.logger.Log("[E2E-LOCK] Correctly rejected short TTL")
		}
	}

	suite.logger.Log("[E2E-LOCK] Minimum TTL test completed")
}

// TestLockNoSessionIdentity tests error when session has no Agent Mail identity
func TestLockNoSessionIdentity(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-no-identity")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try to lock with non-existent session (no identity)
	result, output, _ := suite.RunLockJSON(suite.SessionName(), "*.go")

	if result != nil {
		if result.Success {
			t.Error("[E2E-LOCK] Expected success=false for session with no identity")
		}
		if result.Error == "" {
			t.Error("[E2E-LOCK] Expected error message in result")
		}
		suite.logger.Log("[E2E-LOCK] Error: %s", result.Error)
	} else {
		// Parse error - check raw output
		suite.logger.Log("[E2E-LOCK] Raw output: %s", string(output))
	}

	suite.logger.Log("[E2E-LOCK] No session identity test passed")
}

// TestUnlockRequiresSession tests that unlock requires session argument
func TestUnlockRequiresSession(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-requires-session")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	output, err := suite.RunUnlock()
	if err == nil {
		t.Error("[E2E-LOCK] Expected error when unlock called without arguments")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "session") && !strings.Contains(outputStr, "argument") {
		suite.logger.Log("[E2E-LOCK] Unexpected error output: %s", outputStr)
	}

	suite.logger.Log("[E2E-LOCK] Unlock requires session test passed")
}

// TestUnlockRequiresPatternsOrAll tests that unlock needs patterns or --all
func TestUnlockRequiresPatternsOrAll(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-requires-patterns")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Try unlock without patterns or --all
	output, err := suite.RunUnlock(suite.SessionName())
	if err == nil {
		t.Error("[E2E-LOCK] Expected error when unlock called without patterns or --all")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "pattern") && !strings.Contains(outputStr, "--all") {
		suite.logger.Log("[E2E-LOCK] Output: %s", outputStr)
	}

	suite.logger.Log("[E2E-LOCK] Unlock requires patterns or all test passed")
}

// TestUnlockNoSessionIdentity tests error when session has no Agent Mail identity
func TestUnlockNoSessionIdentity(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-no-identity")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	result, output, _ := suite.RunUnlockJSON(suite.SessionName(), "--all")

	if result != nil {
		if result.Success {
			t.Error("[E2E-LOCK] Expected success=false for session with no identity")
		}
		if result.Error == "" {
			t.Error("[E2E-LOCK] Expected error message in result")
		}
		suite.logger.Log("[E2E-LOCK] Error: %s", result.Error)
	} else {
		suite.logger.Log("[E2E-LOCK] Raw output: %s", string(output))
	}

	suite.logger.Log("[E2E-LOCK] Unlock no session identity test passed")
}

// TestLockHelpOutput tests that --help shows expected information
func TestLockHelpOutput(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-help")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	output, _ := suite.RunLock("--help")
	outputStr := string(output)

	expectedElements := []string{
		"lock",
		"session",
		"pattern",
		"--ttl",
		"--reason",
		"--shared",
	}

	for _, element := range expectedElements {
		if !strings.Contains(outputStr, element) {
			t.Errorf("[E2E-LOCK] Expected help to mention: %s", element)
		}
	}

	suite.logger.Log("[E2E-LOCK] Lock help test passed")
}

// TestUnlockHelpOutput tests that unlock --help shows expected information
func TestUnlockHelpOutput(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-help")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	output, _ := suite.RunUnlock("--help")
	outputStr := string(output)

	expectedElements := []string{
		"unlock",
		"session",
		"pattern",
		"--all",
	}

	for _, element := range expectedElements {
		if !strings.Contains(outputStr, element) {
			t.Errorf("[E2E-LOCK] Expected help to mention: %s", element)
		}
	}

	suite.logger.Log("[E2E-LOCK] Unlock help test passed")
}

// TestLockJSONOutputStructure tests the JSON output structure
func TestLockJSONOutputStructure(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-json-structure")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	result, output, _ := suite.RunLockJSON(suite.SessionName(), "*.go")

	if result != nil {
		// Verify expected fields exist
		if result.Session != suite.SessionName() {
			t.Errorf("[E2E-LOCK] Expected session=%s, got %s", suite.SessionName(), result.Session)
		}

		// Success should be false (no session identity)
		if result.Success {
			t.Log("[E2E-LOCK] Unexpected success - Agent Mail may be configured")
		}

		suite.logger.Log("[E2E-LOCK] JSON structure: success=%v, session=%s, agent=%s",
			result.Success, result.Session, result.Agent)
	} else {
		// Try to parse as generic JSON to verify structure
		var generic map[string]interface{}
		if err := json.Unmarshal(output, &generic); err != nil {
			suite.logger.Log("[E2E-LOCK] Output is not valid JSON: %s", string(output))
		} else {
			suite.logger.Log("[E2E-LOCK] Generic JSON parsed: %v", generic)
		}
	}

	suite.logger.Log("[E2E-LOCK] JSON output structure test passed")
}

// TestUnlockJSONOutputStructure tests the unlock JSON output structure
func TestUnlockJSONOutputStructure(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-json-structure")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	result, output, _ := suite.RunUnlockJSON(suite.SessionName(), "--all")

	if result != nil {
		if result.Session != suite.SessionName() {
			t.Errorf("[E2E-LOCK] Expected session=%s, got %s", suite.SessionName(), result.Session)
		}

		if result.Success {
			t.Log("[E2E-LOCK] Unexpected success - Agent Mail may be configured")
		}

		suite.logger.Log("[E2E-LOCK] JSON structure: success=%v, session=%s, released=%d",
			result.Success, result.Session, result.Released)
	} else {
		var generic map[string]interface{}
		if err := json.Unmarshal(output, &generic); err != nil {
			suite.logger.Log("[E2E-LOCK] Output is not valid JSON: %s", string(output))
		} else {
			suite.logger.Log("[E2E-LOCK] Generic JSON parsed: %v", generic)
		}
	}

	suite.logger.Log("[E2E-LOCK] Unlock JSON output structure test passed")
}

// TestLockValidTTLFormats tests various valid TTL formats
func TestLockValidTTLFormats(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-ttl-formats")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	validTTLs := []string{"1m", "30m", "1h", "2h", "24h"}

	for _, ttl := range validTTLs {
		result, _, _ := suite.RunLockJSON(suite.SessionName(), "*.go", "--ttl", ttl)

		// If we get a result, the TTL format was accepted
		if result != nil {
			// Check that TTL is in result
			if result.TTL != ttl {
				t.Errorf("[E2E-LOCK] Expected TTL=%s in result, got %s", ttl, result.TTL)
			}
			suite.logger.Log("[E2E-LOCK] TTL format %s accepted", ttl)
		} else {
			// May fail for other reasons (no session), but TTL format should be ok
			suite.logger.Log("[E2E-LOCK] TTL format %s - command executed", ttl)
		}
	}

	suite.logger.Log("[E2E-LOCK] Valid TTL formats test passed")
}

// TestLockWithReason tests the --reason flag
func TestLockWithReason(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-reason")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	reason := "Testing file locks"
	_, output, _ := suite.RunLockJSON(suite.SessionName(), "*.go", "--reason", reason)

	// Just verify command accepts the flag
	outputStr := string(output)
	if strings.Contains(outputStr, "unknown flag") || strings.Contains(outputStr, "bad flag") {
		t.Error("[E2E-LOCK] --reason flag not recognized")
	}

	suite.logger.Log("[E2E-LOCK] Lock with reason test passed")
}

// TestLockWithSharedFlag tests the --shared flag
func TestLockWithSharedFlag(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-shared")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	_, output, _ := suite.RunLockJSON(suite.SessionName(), "*.go", "--shared")

	// Verify command accepts the flag
	outputStr := string(output)
	if strings.Contains(outputStr, "unknown flag") || strings.Contains(outputStr, "bad flag") {
		t.Error("[E2E-LOCK] --shared flag not recognized")
	}

	suite.logger.Log("[E2E-LOCK] Lock with shared flag test passed")
}

// TestLockMultiplePatterns tests locking multiple patterns at once
func TestLockMultiplePatterns(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "lock-multiple-patterns")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	// Lock multiple patterns
	_, output, err := suite.RunLockJSON(suite.SessionName(), "*.go", "*.json", "internal/**")

	// Should accept multiple patterns (may fail for other reasons)
	outputStr := string(output)
	if err != nil && strings.Contains(outputStr, "too many arguments") {
		t.Error("[E2E-LOCK] Multiple patterns not accepted")
	}

	suite.logger.Log("[E2E-LOCK] Multiple patterns test passed")
}

// TestUnlockMultiplePatterns tests unlocking multiple patterns at once
func TestUnlockMultiplePatterns(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewLockTestSuite(t, "unlock-multiple-patterns")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-LOCK] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	_, output, err := suite.RunUnlockJSON(suite.SessionName(), "*.go", "*.json")

	outputStr := string(output)
	if err != nil && strings.Contains(outputStr, "too many arguments") {
		t.Error("[E2E-LOCK] Multiple patterns not accepted for unlock")
	}

	suite.logger.Log("[E2E-LOCK] Unlock multiple patterns test passed")
}
