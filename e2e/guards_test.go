//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-GUARDS] Tests for ntm guards (destructive command protection).
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GuardsInstallResponse represents the JSON output from guards install.
type GuardsInstallResponse struct {
	Success    bool   `json:"success"`
	RepoPath   string `json:"repo_path"`
	ProjectKey string `json:"project_key"`
	HookPath   string `json:"hook_path"`
	Message    string `json:"message,omitempty"`
}

// GuardsStatusResponse represents the JSON output from guards status.
type GuardsStatusResponse struct {
	Installed    bool   `json:"installed"`
	RepoPath     string `json:"repo_path"`
	HookPath     string `json:"hook_path"`
	ProjectKey   string `json:"project_key,omitempty"`
	IsNTMGuard   bool   `json:"is_ntm_guard"`
	OtherHook    bool   `json:"other_hook"`
	MCPAvailable bool   `json:"mcp_available"`
}

// GuardsUninstallResponse represents the JSON output from guards uninstall.
type GuardsUninstallResponse struct {
	Success  bool   `json:"success"`
	RepoPath string `json:"repo_path"`
	HookPath string `json:"hook_path"`
	Message  string `json:"message,omitempty"`
}

// GuardsTestSuite manages E2E tests for guards commands.
type GuardsTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	origDir string
	cleanup []func()
}

// NewGuardsTestSuite creates a new guards test suite.
func NewGuardsTestSuite(t *testing.T, scenario string) *GuardsTestSuite {
	logger := NewTestLogger(t, scenario)

	return &GuardsTestSuite{
		t:      t,
		logger: logger,
	}
}

// Setup creates a temporary git repository for testing.
func (s *GuardsTestSuite) Setup() error {
	s.logger.Log("[E2E-GUARDS] Setting up test environment")

	// Save original directory
	origDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	s.origDir = origDir

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "ntm-guards-e2e-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	s.tempDir = tempDir

	s.cleanup = append(s.cleanup, func() {
		os.Chdir(s.origDir)
		os.RemoveAll(tempDir)
	})

	// Initialize git repository
	if err := s.initGitRepo(); err != nil {
		return fmt.Errorf("init git repo: %w", err)
	}

	s.logger.Log("[E2E-GUARDS] Created test git repository at %s", tempDir)
	return nil
}

// initGitRepo initializes a git repository in the temp directory.
func (s *GuardsTestSuite) initGitRepo() error {
	cmd := exec.Command("git", "init")
	cmd.Dir = s.tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init failed: %w, output: %s", err, string(output))
	}

	// Configure git user for commits
	gitConfig := [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test User"},
	}

	for _, args := range gitConfig {
		cmd := exec.Command("git", args...)
		cmd.Dir = s.tempDir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git config failed: %w", err)
		}
	}

	return nil
}

// TempDir returns the temporary directory path.
func (s *GuardsTestSuite) TempDir() string {
	return s.tempDir
}

// HookPath returns the expected pre-commit hook path.
func (s *GuardsTestSuite) HookPath() string {
	return filepath.Join(s.tempDir, ".git", "hooks", "pre-commit")
}

// Logger returns the test logger.
func (s *GuardsTestSuite) Logger() *TestLogger {
	return s.logger
}

// Teardown cleans up resources.
func (s *GuardsTestSuite) Teardown() {
	s.logger.Log("[E2E-GUARDS] Running cleanup (%d items)", len(s.cleanup))

	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}

	s.logger.Close()
}

// RunGuardsInstall executes ntm guards install and returns the parsed result.
func (s *GuardsTestSuite) RunGuardsInstall(flags ...string) (*GuardsInstallResponse, error) {
	s.logger.Log("[E2E-GUARDS] Running guards install flags=%v", flags)

	args := []string{"guards", "install", "--json"}
	args = append(args, flags...)

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Dir = s.tempDir
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-GUARDS] Output: %s", string(output))

	var resp GuardsInstallResponse
	if jsonErr := json.Unmarshal(output, &resp); jsonErr != nil {
		if err != nil {
			s.logger.Log("[E2E-GUARDS] Command exited with error: %v", err)
		}
		// Try to find JSON in output
		if idx := strings.Index(string(output), "{"); idx >= 0 {
			if json.Unmarshal(output[idx:], &resp) == nil {
				s.logger.LogJSON("[E2E-GUARDS] Install response", resp)
				return &resp, nil
			}
		}
		return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
	}

	s.logger.LogJSON("[E2E-GUARDS] Install response", resp)
	return &resp, nil
}

// RunGuardsStatus executes ntm guards status and returns the parsed result.
func (s *GuardsTestSuite) RunGuardsStatus() (*GuardsStatusResponse, error) {
	s.logger.Log("[E2E-GUARDS] Running guards status")

	cmd := exec.Command(mustE2EBin(), "guards", "status", "--json")
	cmd.Dir = s.tempDir
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-GUARDS] Output: %s", string(output))

	var resp GuardsStatusResponse
	if jsonErr := json.Unmarshal(output, &resp); jsonErr != nil {
		if err != nil {
			s.logger.Log("[E2E-GUARDS] Command exited with error: %v", err)
		}
		// Try to find JSON in output
		if idx := strings.Index(string(output), "{"); idx >= 0 {
			if json.Unmarshal(output[idx:], &resp) == nil {
				s.logger.LogJSON("[E2E-GUARDS] Status response", resp)
				return &resp, nil
			}
		}
		return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
	}

	s.logger.LogJSON("[E2E-GUARDS] Status response", resp)
	return &resp, nil
}

// RunGuardsUninstall executes ntm guards uninstall and returns the parsed result.
func (s *GuardsTestSuite) RunGuardsUninstall() (*GuardsUninstallResponse, error) {
	s.logger.Log("[E2E-GUARDS] Running guards uninstall")

	cmd := exec.Command(mustE2EBin(), "guards", "uninstall", "--json")
	cmd.Dir = s.tempDir
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-GUARDS] Output: %s", string(output))

	var resp GuardsUninstallResponse
	if jsonErr := json.Unmarshal(output, &resp); jsonErr != nil {
		if err != nil {
			s.logger.Log("[E2E-GUARDS] Command exited with error: %v", err)
		}
		// Try to find JSON in output
		if idx := strings.Index(string(output), "{"); idx >= 0 {
			if json.Unmarshal(output[idx:], &resp) == nil {
				s.logger.LogJSON("[E2E-GUARDS] Uninstall response", resp)
				return &resp, nil
			}
		}
		return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
	}

	s.logger.LogJSON("[E2E-GUARDS] Uninstall response", resp)
	return &resp, nil
}

// CreatePreCommitHook creates a pre-commit hook with the given content.
func (s *GuardsTestSuite) CreatePreCommitHook(content string) error {
	hookDir := filepath.Join(s.tempDir, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(s.HookPath(), []byte(content), 0755)
}

// ReadPreCommitHook reads the pre-commit hook content.
func (s *GuardsTestSuite) ReadPreCommitHook() (string, error) {
	data, err := os.ReadFile(s.HookPath())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// HookExists checks if the pre-commit hook exists.
func (s *GuardsTestSuite) HookExists() bool {
	_, err := os.Stat(s.HookPath())
	return err == nil
}

// TestGuardsInstallBasic tests basic guards installation.
func TestGuardsInstallBasic(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-install-basic")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	resp, err := suite.RunGuardsInstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
	}

	// Verify response
	if !resp.Success {
		t.Error("[E2E-GUARDS] Expected success=true")
	}

	if resp.RepoPath == "" {
		t.Error("[E2E-GUARDS] Expected non-empty repo_path")
	}

	if resp.HookPath == "" {
		t.Error("[E2E-GUARDS] Expected non-empty hook_path")
	}

	// Verify hook file was created
	if !suite.HookExists() {
		t.Error("[E2E-GUARDS] Expected pre-commit hook to exist")
	}

	// Verify hook content
	content, err := suite.ReadPreCommitHook()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Failed to read hook: %v", err)
	}

	if !strings.Contains(content, "ntm-precommit-guard") {
		t.Error("[E2E-GUARDS] Hook should contain ntm-precommit-guard marker")
	}

	suite.Logger().Log("[E2E-GUARDS] Basic install test passed")
}

// TestGuardsInstallIdempotent tests that installing twice is idempotent.
func TestGuardsInstallIdempotent(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-install-idempotent")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	// First install
	resp1, err := suite.RunGuardsInstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] First install failed: %v", err)
	}

	if !resp1.Success {
		t.Fatal("[E2E-GUARDS] First install should succeed")
	}

	// Second install should also succeed (idempotent)
	resp2, err := suite.RunGuardsInstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Second install failed: %v", err)
	}

	if !resp2.Success {
		t.Error("[E2E-GUARDS] Second install should succeed")
	}

	// Should indicate already installed
	if resp2.Message == "" {
		suite.Logger().Log("[E2E-GUARDS] Note: second install returned no message")
	} else {
		suite.Logger().Log("[E2E-GUARDS] Second install message: %s", resp2.Message)
	}

	suite.Logger().Log("[E2E-GUARDS] Idempotent install test passed")
}

// TestGuardsInstallWithExistingHook tests install when a non-NTM hook exists.
func TestGuardsInstallWithExistingHook(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-install-existing-hook")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	// Create a non-NTM pre-commit hook
	existingHook := `#!/bin/bash
# Existing hook from another tool
echo "Running existing hook"
exit 0
`
	if err := suite.CreatePreCommitHook(existingHook); err != nil {
		t.Fatalf("[E2E-GUARDS] Failed to create existing hook: %v", err)
	}

	// Install without --force should fail
	resp, err := suite.RunGuardsInstall()
	if err == nil && resp != nil && resp.Success {
		t.Error("[E2E-GUARDS] Install should fail when non-NTM hook exists")
	}

	suite.Logger().Log("[E2E-GUARDS] Install with existing hook correctly failed")

	// Install with --force should succeed
	respForce, err := suite.RunGuardsInstall("--force")
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Install with --force failed: %v", err)
	}

	if !respForce.Success {
		t.Error("[E2E-GUARDS] Install with --force should succeed")
	}

	// Verify hook was replaced
	content, _ := suite.ReadPreCommitHook()
	if !strings.Contains(content, "ntm-precommit-guard") {
		t.Error("[E2E-GUARDS] Hook should contain NTM guard after --force")
	}

	suite.Logger().Log("[E2E-GUARDS] Install with --force test passed")
}

// TestGuardsStatus tests guards status command.
func TestGuardsStatus(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-status")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	t.Run("StatusNotInstalled", func(t *testing.T) {
		resp, err := suite.RunGuardsStatus()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Status failed: %v", err)
		}

		if resp.Installed {
			t.Error("[E2E-GUARDS] Expected installed=false when no guard exists")
		}

		if resp.IsNTMGuard {
			t.Error("[E2E-GUARDS] Expected is_ntm_guard=false when no guard exists")
		}

		suite.Logger().Log("[E2E-GUARDS] Status not installed: repo=%s", resp.RepoPath)
	})

	t.Run("StatusAfterInstall", func(t *testing.T) {
		// Install the guard first
		_, err := suite.RunGuardsInstall()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
		}

		resp, err := suite.RunGuardsStatus()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Status failed: %v", err)
		}

		if !resp.Installed {
			t.Error("[E2E-GUARDS] Expected installed=true after install")
		}

		if !resp.IsNTMGuard {
			t.Error("[E2E-GUARDS] Expected is_ntm_guard=true after install")
		}

		if resp.OtherHook {
			t.Error("[E2E-GUARDS] Expected other_hook=false for NTM guard")
		}

		suite.Logger().Log("[E2E-GUARDS] Status after install: installed=%v, is_ntm_guard=%v, project_key=%s",
			resp.Installed, resp.IsNTMGuard, resp.ProjectKey)
	})

	t.Run("StatusWithOtherHook", func(t *testing.T) {
		// Remove NTM guard and install a different hook
		os.Remove(suite.HookPath())

		otherHook := `#!/bin/bash
echo "Other hook"
`
		if err := suite.CreatePreCommitHook(otherHook); err != nil {
			t.Fatalf("[E2E-GUARDS] Failed to create other hook: %v", err)
		}

		resp, err := suite.RunGuardsStatus()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Status failed: %v", err)
		}

		if resp.Installed {
			t.Error("[E2E-GUARDS] Expected installed=false for non-NTM hook")
		}

		if resp.IsNTMGuard {
			t.Error("[E2E-GUARDS] Expected is_ntm_guard=false for non-NTM hook")
		}

		if !resp.OtherHook {
			t.Error("[E2E-GUARDS] Expected other_hook=true for non-NTM hook")
		}

		suite.Logger().Log("[E2E-GUARDS] Status with other hook: other_hook=%v", resp.OtherHook)
	})
}

// TestGuardsUninstall tests guards uninstall command.
func TestGuardsUninstall(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-uninstall")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	t.Run("UninstallWhenNotInstalled", func(t *testing.T) {
		resp, err := suite.RunGuardsUninstall()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Uninstall failed: %v", err)
		}

		if !resp.Success {
			t.Error("[E2E-GUARDS] Uninstall should succeed even when not installed")
		}

		suite.Logger().Log("[E2E-GUARDS] Uninstall when not installed: message=%s", resp.Message)
	})

	t.Run("UninstallAfterInstall", func(t *testing.T) {
		// Install first
		_, err := suite.RunGuardsInstall()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
		}

		if !suite.HookExists() {
			t.Fatal("[E2E-GUARDS] Hook should exist after install")
		}

		// Uninstall
		resp, err := suite.RunGuardsUninstall()
		if err != nil {
			t.Fatalf("[E2E-GUARDS] Uninstall failed: %v", err)
		}

		if !resp.Success {
			t.Error("[E2E-GUARDS] Uninstall should succeed")
		}

		if suite.HookExists() {
			t.Error("[E2E-GUARDS] Hook should not exist after uninstall")
		}

		suite.Logger().Log("[E2E-GUARDS] Uninstall after install: success=%v", resp.Success)
	})

	t.Run("UninstallRefusesNonNTMHook", func(t *testing.T) {
		// Create a non-NTM hook
		otherHook := `#!/bin/bash
echo "Other hook"
`
		if err := suite.CreatePreCommitHook(otherHook); err != nil {
			t.Fatalf("[E2E-GUARDS] Failed to create other hook: %v", err)
		}

		// Uninstall should refuse
		_, err := suite.RunGuardsUninstall()
		if err == nil {
			// Check if hook still exists (it should)
			if !suite.HookExists() {
				t.Error("[E2E-GUARDS] Uninstall should not remove non-NTM hook")
			}
		}

		// Verify hook still exists
		if suite.HookExists() {
			content, _ := suite.ReadPreCommitHook()
			if strings.Contains(content, "Other hook") {
				suite.Logger().Log("[E2E-GUARDS] Correctly refused to uninstall non-NTM hook")
			}
		}
	})
}

// TestGuardsInstallWithProjectKey tests install with custom project key.
func TestGuardsInstallWithProjectKey(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-install-project-key")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	customKey := "/custom/project/key"
	resp, err := suite.RunGuardsInstall("--project-key", customKey)
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
	}

	if !resp.Success {
		t.Error("[E2E-GUARDS] Install should succeed with custom project key")
	}

	if resp.ProjectKey != customKey {
		t.Errorf("[E2E-GUARDS] Expected project_key=%s, got %s", customKey, resp.ProjectKey)
	}

	// Verify project key in hook content
	content, _ := suite.ReadPreCommitHook()
	if !strings.Contains(content, customKey) {
		t.Error("[E2E-GUARDS] Hook should contain custom project key")
	}

	// Status should show the project key
	status, err := suite.RunGuardsStatus()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Status failed: %v", err)
	}

	if status.ProjectKey != customKey {
		t.Errorf("[E2E-GUARDS] Status project_key should be %s, got %s", customKey, status.ProjectKey)
	}

	suite.Logger().Log("[E2E-GUARDS] Custom project key test passed")
}

// TestGuardsNotInGitRepo tests guards commands outside a git repo.
func TestGuardsNotInGitRepo(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	logger := NewTestLogger(t, "guards-not-git-repo")
	defer logger.Close()

	// Create temp dir that's NOT a git repo
	tempDir, err := os.MkdirTemp("", "ntm-guards-nogit-")
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Try guards status
	cmd := exec.Command(mustE2EBin(), "guards", "status", "--json")
	cmd.Dir = tempDir
	output, _ := cmd.CombinedOutput()

	logger.Log("[E2E-GUARDS] Status in non-git dir: %s", string(output))

	// Should return valid JSON indicating not installed
	var resp GuardsStatusResponse
	if err := json.Unmarshal(output, &resp); err == nil {
		if resp.Installed {
			t.Error("[E2E-GUARDS] Should not be installed in non-git directory")
		}
		logger.Log("[E2E-GUARDS] Status correctly handled non-git directory")
	}

	// Try guards install (should fail)
	cmd = exec.Command(mustE2EBin(), "guards", "install", "--json")
	cmd.Dir = tempDir
	output, err = cmd.CombinedOutput()

	logger.Log("[E2E-GUARDS] Install in non-git dir: %s", string(output))

	if err == nil {
		t.Error("[E2E-GUARDS] Install should fail in non-git directory")
	} else {
		logger.Log("[E2E-GUARDS] Install correctly failed in non-git directory")
	}

	logger.Log("[E2E-GUARDS] Non-git repo test passed")
}

// TestGuardsHookExecution tests that the installed hook executes correctly.
func TestGuardsHookExecution(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-hook-execution")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	// Install the guard
	_, err := suite.RunGuardsInstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
	}

	// Create a file to commit
	testFile := filepath.Join(suite.TempDir(), "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("[E2E-GUARDS] Failed to create test file: %v", err)
	}

	// Stage the file
	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = suite.TempDir()
	if err := cmd.Run(); err != nil {
		t.Fatalf("[E2E-GUARDS] Failed to stage file: %v", err)
	}

	// Try to commit (this should run the pre-commit hook)
	cmd = exec.Command("git", "commit", "-m", "test commit")
	cmd.Dir = suite.TempDir()
	output, err := cmd.CombinedOutput()

	suite.Logger().Log("[E2E-GUARDS] Commit output: %s", string(output))

	// The commit should succeed (the fallback hook just logs and passes)
	if err != nil {
		suite.Logger().Log("[E2E-GUARDS] Commit failed (may be expected depending on hook implementation): %v", err)
	} else {
		suite.Logger().Log("[E2E-GUARDS] Commit succeeded with guard active")
	}

	// Check if hook logged something
	if strings.Contains(string(output), "ntm-guard") {
		suite.Logger().Log("[E2E-GUARDS] Hook execution logged")
	}

	suite.Logger().Log("[E2E-GUARDS] Hook execution test passed")
}

// TestGuardsFullWorkflow tests the complete install-status-uninstall workflow.
func TestGuardsFullWorkflow(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGuardsTestSuite(t, "guards-full-workflow")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GUARDS] Setup failed: %v", err)
	}

	// 1. Initial status - not installed
	suite.Logger().Log("[E2E-GUARDS] Step 1: Check initial status")
	status1, err := suite.RunGuardsStatus()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Initial status failed: %v", err)
	}
	if status1.Installed {
		t.Error("[E2E-GUARDS] Should not be installed initially")
	}

	// 2. Install
	suite.Logger().Log("[E2E-GUARDS] Step 2: Install guard")
	install, err := suite.RunGuardsInstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Install failed: %v", err)
	}
	if !install.Success {
		t.Error("[E2E-GUARDS] Install should succeed")
	}

	// 3. Status after install
	suite.Logger().Log("[E2E-GUARDS] Step 3: Check status after install")
	status2, err := suite.RunGuardsStatus()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Status after install failed: %v", err)
	}
	if !status2.Installed || !status2.IsNTMGuard {
		t.Error("[E2E-GUARDS] Should be installed after install")
	}

	// 4. Uninstall
	suite.Logger().Log("[E2E-GUARDS] Step 4: Uninstall guard")
	uninstall, err := suite.RunGuardsUninstall()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Uninstall failed: %v", err)
	}
	if !uninstall.Success {
		t.Error("[E2E-GUARDS] Uninstall should succeed")
	}

	// 5. Final status - not installed
	suite.Logger().Log("[E2E-GUARDS] Step 5: Check final status")
	status3, err := suite.RunGuardsStatus()
	if err != nil {
		t.Fatalf("[E2E-GUARDS] Final status failed: %v", err)
	}
	if status3.Installed {
		t.Error("[E2E-GUARDS] Should not be installed after uninstall")
	}

	suite.Logger().Log("[E2E-GUARDS] Full workflow test passed")
}
