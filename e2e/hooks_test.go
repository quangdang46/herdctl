//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM CLI commands.
// [E2E-HOOKS] Tests for ntm hooks install/status/uninstall commands.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// HooksInstallResult represents JSON output from ntm hooks install --json.
type HooksInstallResult struct {
	Success  bool   `json:"success"`
	HookType string `json:"hook_type,omitempty"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
}

// HooksUninstallResult represents JSON output from ntm hooks uninstall --json.
type HooksUninstallResult struct {
	Success  bool   `json:"success"`
	HookType string `json:"hook_type,omitempty"`
	Restored bool   `json:"restored,omitempty"`
	Error    string `json:"error,omitempty"`
}

// HooksStatusResult represents JSON output from ntm hooks status --json.
type HooksStatusResult struct {
	RepoRoot string     `json:"repo_root"`
	HooksDir string     `json:"hooks_dir"`
	Hooks    []HookInfo `json:"hooks"`
	Error    string     `json:"error,omitempty"`
}

// HookInfo mirrors internal/hooks.HookInfo for JSON assertions.
type HookInfo struct {
	Type      string `json:"type"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
	IsNTM     bool   `json:"is_ntm"`
	HasBackup bool   `json:"has_backup"`
}

// HooksTestSuite manages E2E tests for hooks commands.
type HooksTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	ntmPath string
	tempDir string
	repoDir string
	cleanup []func()
}

// NewHooksTestSuite creates a new hooks test suite with a temp git repo.
func NewHooksTestSuite(t *testing.T, scenario string) *HooksTestSuite {
	logger := NewTestLogger(t, scenario)

	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	tempDir, err := os.MkdirTemp("", "ntm-hooks-e2e-")
	if err != nil {
		t.Fatalf("[E2E-HOOKS] Failed to create temp dir: %v", err)
	}

	suite := &HooksTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
		tempDir: tempDir,
		repoDir: filepath.Join(tempDir, "repo"),
	}

	suite.cleanup = append(suite.cleanup, func() {
		os.RemoveAll(tempDir)
	})

	return suite
}

// SetupRepo initializes a git repo with an initial commit.
func (s *HooksTestSuite) SetupRepo() {
	s.logger.Log("[E2E-HOOKS] Setting up git repo at %s", s.repoDir)

	if _, err := exec.LookPath("git"); err != nil {
		s.t.Skip("git not found in PATH")
	}

	if err := os.MkdirAll(s.repoDir, 0755); err != nil {
		s.t.Fatalf("[E2E-HOOKS] Failed to create repo dir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = s.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Fatalf("[E2E-HOOKS] git init failed: %v output=%s", err, string(out))
	}

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = s.repoDir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = s.repoDir
	_ = cmd.Run()

	readmePath := filepath.Join(s.repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# hooks e2e\n"), 0644); err != nil {
		s.t.Fatalf("[E2E-HOOKS] write README failed: %v", err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = s.repoDir
	_ = cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = s.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		s.t.Fatalf("[E2E-HOOKS] git commit failed: %v output=%s", err, string(out))
	}
}

// Cleanup runs registered cleanup functions and closes the logger.
func (s *HooksTestSuite) Cleanup() {
	s.logger.Close()
	for _, fn := range s.cleanup {
		fn()
	}
}

func (s *HooksTestSuite) runHooksCommand(args ...string) ([]byte, error) {
	cmd := exec.Command(s.ntmPath, args...)
	cmd.Dir = s.repoDir
	return cmd.CombinedOutput()
}

func (s *HooksTestSuite) installHook() (*HooksInstallResult, error) {
	s.logger.Log("[E2E-HOOKS] Running: ntm hooks install --json")
	out, err := s.runHooksCommand("hooks", "install", "--json")
	if err != nil {
		s.logger.Log("[E2E-HOOKS] install failed: %v output=%s", err, string(out))
		return nil, fmt.Errorf("install failed: %w output=%s", err, string(out))
	}

	var res HooksInstallResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("parse install output: %w output=%s", err, string(out))
	}
	return &res, nil
}

func (s *HooksTestSuite) uninstallHook() (*HooksUninstallResult, error) {
	s.logger.Log("[E2E-HOOKS] Running: ntm hooks uninstall --json")
	out, err := s.runHooksCommand("hooks", "uninstall", "--json")
	if err != nil {
		s.logger.Log("[E2E-HOOKS] uninstall failed: %v output=%s", err, string(out))
		return nil, fmt.Errorf("uninstall failed: %w output=%s", err, string(out))
	}

	var res HooksUninstallResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("parse uninstall output: %w output=%s", err, string(out))
	}
	return &res, nil
}

func (s *HooksTestSuite) statusHooks() (*HooksStatusResult, error) {
	s.logger.Log("[E2E-HOOKS] Running: ntm hooks status --json")
	out, err := s.runHooksCommand("hooks", "status", "--json")
	if err != nil {
		s.logger.Log("[E2E-HOOKS] status failed: %v output=%s", err, string(out))
		return nil, fmt.Errorf("status failed: %w output=%s", err, string(out))
	}

	var res HooksStatusResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("parse status output: %w output=%s", err, string(out))
	}
	return &res, nil
}

func findHookInfo(infos []HookInfo, hookType string) *HookInfo {
	for _, info := range infos {
		if info.Type == hookType {
			copy := info
			return &copy
		}
	}
	return nil
}

// =============================================================================
// Tests
// =============================================================================

func TestHooksInstallStatusUninstall(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewHooksTestSuite(t, "hooks_install_status_uninstall")
	defer suite.Cleanup()

	suite.SetupRepo()

	installRes, err := suite.installHook()
	if err != nil {
		t.Fatalf("install hook failed: %v", err)
	}
	suite.logger.LogJSON("[E2E-HOOKS] install result", installRes)

	if !installRes.Success {
		t.Fatalf("expected install success, got error=%s", installRes.Error)
	}
	if installRes.HookType != "pre-commit" {
		t.Fatalf("expected hook_type pre-commit, got %q", installRes.HookType)
	}

	statusRes, err := suite.statusHooks()
	if err != nil {
		t.Fatalf("status hook failed: %v", err)
	}
	suite.logger.LogJSON("[E2E-HOOKS] status result", statusRes)

	if statusRes.RepoRoot == "" || statusRes.HooksDir == "" {
		t.Fatalf("expected repo_root and hooks_dir in status, got repo_root=%q hooks_dir=%q", statusRes.RepoRoot, statusRes.HooksDir)
	}

	preCommit := findHookInfo(statusRes.Hooks, "pre-commit")
	if preCommit == nil {
		t.Fatalf("pre-commit hook not found in status")
	}
	if !preCommit.Installed {
		t.Fatalf("expected pre-commit installed=true, got false")
	}
	if !preCommit.IsNTM {
		t.Fatalf("expected pre-commit is_ntm=true, got false")
	}

	uninstallRes, err := suite.uninstallHook()
	if err != nil {
		t.Fatalf("uninstall hook failed: %v", err)
	}
	suite.logger.LogJSON("[E2E-HOOKS] uninstall result", uninstallRes)

	if !uninstallRes.Success {
		t.Fatalf("expected uninstall success, got error=%s", uninstallRes.Error)
	}

	statusResAfter, err := suite.statusHooks()
	if err != nil {
		t.Fatalf("status hook after uninstall failed: %v", err)
	}
	suite.logger.LogJSON("[E2E-HOOKS] status after uninstall", statusResAfter)

	preCommitAfter := findHookInfo(statusResAfter.Hooks, "pre-commit")
	if preCommitAfter == nil {
		t.Fatalf("pre-commit hook missing after uninstall")
	}
	if preCommitAfter.Installed {
		t.Fatalf("expected pre-commit installed=false after uninstall, got true")
	}
}
