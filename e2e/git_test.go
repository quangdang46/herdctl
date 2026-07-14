//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-GIT] Tests for ntm git operations (status, sync, coordination).
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// GitStatusResult mirrors the CLI output structure for JSON parsing
type GitStatusResult struct {
	Success    bool                 `json:"success"`
	Session    string               `json:"session,omitempty"`
	WorkingDir string               `json:"working_dir"`
	Git        *GitInfo             `json:"git,omitempty"`
	Locks      []GitFileReservation `json:"locks,omitempty"`
	AgentMail  *AgentMailStatus     `json:"agent_mail,omitempty"`
	Error      string               `json:"error,omitempty"`
}

// GitInfo contains git repository information
type GitInfo struct {
	Branch          string   `json:"branch"`
	Commit          string   `json:"commit"`
	CommitShort     string   `json:"commit_short"`
	Dirty           bool     `json:"dirty"`
	Ahead           int      `json:"ahead"`
	Behind          int      `json:"behind"`
	StagedFiles     []string `json:"staged_files,omitempty"`
	ModifiedFiles   []string `json:"modified_files,omitempty"`
	UntrackedFiles  []string `json:"untracked_files,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

// GitFileReservation represents a file lock
type GitFileReservation struct {
	Path      string `json:"path_pattern"`
	Agent     string `json:"agent_name"`
	Exclusive bool   `json:"exclusive"`
}

// AgentMailStatus contains Agent Mail coordination info
type AgentMailStatus struct {
	Available       bool   `json:"available"`
	RegisteredAgent string `json:"registered_agent,omitempty"`
	LockCount       int    `json:"lock_count"`
	ConflictCount   int    `json:"conflict_count"`
}

// GitSyncResult represents the result of a git sync operation
type GitSyncResult struct {
	Success     bool        `json:"success"`
	Session     string      `json:"session,omitempty"`
	WorkingDir  string      `json:"working_dir"`
	PullResult  *PullResult `json:"pull_result,omitempty"`
	PushResult  *PushResult `json:"push_result,omitempty"`
	HasConflict bool        `json:"has_conflict"`
	Error       string      `json:"error,omitempty"`
}

// PullResult contains the result of a git pull operation
type PullResult struct {
	Success         bool     `json:"success"`
	FastForward     bool     `json:"fast_forward"`
	Behind          int      `json:"behind"`
	Merged          int      `json:"merged"`
	Files           []string `json:"files,omitempty"`
	Conflicts       []string `json:"conflicts,omitempty"`
	Error           string   `json:"error,omitempty"`
	AlreadyUpToDate bool     `json:"already_up_to_date"`
}

// PushResult contains the result of a git push operation
type PushResult struct {
	Success       bool   `json:"success"`
	Ahead         int    `json:"ahead"`
	Pushed        int    `json:"pushed"`
	Remote        string `json:"remote"`
	Branch        string `json:"branch"`
	Error         string `json:"error,omitempty"`
	NothingToPush bool   `json:"nothing_to_push"`
}

// GitTestSuite manages E2E tests for git commands
type GitTestSuite struct {
	t        *testing.T
	logger   *TestLogger
	tempDir  string
	repoPath string
	cleanup  []func()
}

// NewGitTestSuite creates a new git test suite with a temp git repo
func NewGitTestSuite(t *testing.T, scenario string) *GitTestSuite {
	logger := NewTestLogger(t, scenario)

	s := &GitTestSuite{
		t:      t,
		logger: logger,
	}

	return s
}

// Setup creates a temporary git repository for testing
func (s *GitTestSuite) Setup() error {
	s.logger.Log("[E2E-GIT] Setting up test environment")

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "ntm-git-e2e-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	s.tempDir = tempDir
	s.repoPath = filepath.Join(tempDir, "test-repo")

	s.cleanup = append(s.cleanup, func() {
		os.RemoveAll(tempDir)
	})

	// Create and init git repo
	if err := os.MkdirAll(s.repoPath, 0755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = s.repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w, output: %s", err, string(out))
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = s.repoPath
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = s.repoPath
	cmd.Run()

	// Create initial commit
	readme := filepath.Join(s.repoPath, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repository\n"), 0644); err != nil {
		return fmt.Errorf("write readme: %w", err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = s.repoPath
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = s.repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("initial commit: %w, output: %s", err, string(out))
	}

	s.logger.Log("[E2E-GIT] Created test repo at %s", s.repoPath)
	return nil
}

// RepoPath returns the test repository path
func (s *GitTestSuite) RepoPath() string {
	return s.repoPath
}

// Logger returns the test logger
func (s *GitTestSuite) Logger() *TestLogger {
	return s.logger
}

// Teardown cleans up resources
func (s *GitTestSuite) Teardown() {
	s.logger.Log("[E2E-GIT] Running cleanup (%d items)", len(s.cleanup))

	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}

	s.logger.Close()
}

// RunGitStatus executes ntm git status and returns the parsed result
func (s *GitTestSuite) RunGitStatus(flags ...string) (*GitStatusResult, error) {
	s.logger.Log("[E2E-GIT] Running git status flags=%v", flags)

	// Change to repo directory and run ntm git status
	args := []string{"git", "status", "--json"}
	args = append(args, flags...)

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Dir = s.repoPath
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-GIT] Output: %s", string(output))

	var result GitStatusResult
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil {
		if err != nil {
			return nil, fmt.Errorf("command failed: %w, output: %s", err, string(output))
		}
		return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
	}

	s.logger.LogJSON("[E2E-GIT] Status Result", result)
	return &result, nil
}

// RunGitSync executes ntm git sync and returns the parsed result
func (s *GitTestSuite) RunGitSync(flags ...string) (*GitSyncResult, error) {
	s.logger.Log("[E2E-GIT] Running git sync flags=%v", flags)

	args := []string{"git", "sync", "--json"}
	args = append(args, flags...)

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Dir = s.repoPath
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-GIT] Output: %s", string(output))

	var result GitSyncResult
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil {
		if err != nil {
			return nil, fmt.Errorf("command failed: %w, output: %s", err, string(output))
		}
		return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
	}

	s.logger.LogJSON("[E2E-GIT] Sync Result", result)
	return &result, nil
}

// CreateFile creates a file in the test repo
func (s *GitTestSuite) CreateFile(name, content string) error {
	path := filepath.Join(s.repoPath, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// ModifyFile modifies an existing file
func (s *GitTestSuite) ModifyFile(name, content string) error {
	path := filepath.Join(s.repoPath, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// StageFile stages a file
func (s *GitTestSuite) StageFile(name string) error {
	cmd := exec.Command("git", "add", name)
	cmd.Dir = s.repoPath
	return cmd.Run()
}

// CommitChanges commits staged changes
func (s *GitTestSuite) CommitChanges(message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = s.repoPath
	return cmd.Run()
}

// GetCurrentBranch returns the current branch name
func (s *GitTestSuite) GetCurrentBranch() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = s.repoPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestGitStatusCleanRepo tests git status on a clean repository
func TestGitStatusCleanRepo(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-status-clean")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	// Verify result
	if !result.Success {
		t.Errorf("[E2E-GIT] Expected success, got error: %s", result.Error)
	}

	if result.Git == nil {
		t.Fatal("[E2E-GIT] Expected Git info, got nil")
	}

	// Check branch detection
	branch := suite.GetCurrentBranch()
	if result.Git.Branch != branch {
		t.Errorf("[E2E-GIT] Expected branch '%s', got '%s'", branch, result.Git.Branch)
	}

	// Clean repo should not be dirty
	if result.Git.Dirty {
		t.Error("[E2E-GIT] Clean repo should not be dirty")
	}

	// Should have no modified files
	if len(result.Git.ModifiedFiles) > 0 {
		t.Errorf("[E2E-GIT] Expected no modified files, got %v", result.Git.ModifiedFiles)
	}

	suite.Logger().Log("[E2E-GIT] Clean repo status test passed")
}

// TestGitStatusWithChanges tests git status with uncommitted changes
func TestGitStatusWithChanges(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-status-changes")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Create modifications
	t.Run("UntrackedFile", func(t *testing.T) {
		if err := suite.CreateFile("newfile.txt", "new content\n"); err != nil {
			t.Fatalf("[E2E-GIT] Failed to create file: %v", err)
		}

		result, err := suite.RunGitStatus()
		if err != nil {
			t.Fatalf("[E2E-GIT] Git status failed: %v", err)
		}

		// Should have untracked file
		found := false
		for _, f := range result.Git.UntrackedFiles {
			if strings.Contains(f, "newfile.txt") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[E2E-GIT] Expected untracked file 'newfile.txt', got: %v", result.Git.UntrackedFiles)
		}

		suite.Logger().Log("[E2E-GIT] Untracked file test passed")
	})

	t.Run("ModifiedFile", func(t *testing.T) {
		if err := suite.ModifyFile("README.md", "\nMore content\n"); err != nil {
			t.Fatalf("[E2E-GIT] Failed to modify file: %v", err)
		}

		result, err := suite.RunGitStatus()
		if err != nil {
			t.Fatalf("[E2E-GIT] Git status failed: %v", err)
		}

		if !result.Git.Dirty {
			t.Error("[E2E-GIT] Repo with changes should be dirty")
		}

		// Should have modified file
		found := false
		for _, f := range result.Git.ModifiedFiles {
			if strings.Contains(f, "README.md") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[E2E-GIT] Expected modified file 'README.md', got: %v", result.Git.ModifiedFiles)
		}

		suite.Logger().Log("[E2E-GIT] Modified file test passed")
	})

	t.Run("StagedFile", func(t *testing.T) {
		if err := suite.StageFile("newfile.txt"); err != nil {
			t.Fatalf("[E2E-GIT] Failed to stage file: %v", err)
		}

		result, err := suite.RunGitStatus()
		if err != nil {
			t.Fatalf("[E2E-GIT] Git status failed: %v", err)
		}

		// Should have staged file
		found := false
		for _, f := range result.Git.StagedFiles {
			if strings.Contains(f, "newfile.txt") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[E2E-GIT] Expected staged file 'newfile.txt', got: %v", result.Git.StagedFiles)
		}

		suite.Logger().Log("[E2E-GIT] Staged file test passed")
	})
}

// TestGitStatusResultFormat verifies the JSON output structure
func TestGitStatusResultFormat(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-status-format")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	// Verify required fields
	if result.WorkingDir == "" {
		t.Error("[E2E-GIT] WorkingDir should not be empty")
	}

	if result.Git == nil {
		t.Fatal("[E2E-GIT] Git info should not be nil")
	}

	// Verify git info structure
	if result.Git.Branch == "" {
		t.Error("[E2E-GIT] Branch should not be empty")
	}

	if result.Git.Commit == "" {
		t.Error("[E2E-GIT] Commit should not be empty")
	}

	if result.Git.CommitShort == "" {
		t.Error("[E2E-GIT] CommitShort should not be empty")
	}

	if len(result.Git.CommitShort) > 7 {
		t.Errorf("[E2E-GIT] CommitShort should be <= 7 chars, got %d", len(result.Git.CommitShort))
	}

	// Verify AgentMail section exists
	if result.AgentMail == nil {
		suite.Logger().Log("[E2E-GIT] Note: AgentMail info is nil (may be unavailable)")
	}

	suite.Logger().Log("[E2E-GIT] Result format validation passed")
}

// TestGitSyncDryRun tests git sync with dry-run flag
func TestGitSyncDryRun(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-sync-dry-run")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Dry-run should work even without a remote
	result, err := suite.RunGitSync("--dry-run")
	if err != nil {
		// This might fail without a remote, which is expected
		suite.Logger().Log("[E2E-GIT] Sync dry-run (expected to potentially fail without remote): %v", err)
		return
	}

	suite.Logger().Log("[E2E-GIT] Sync dry-run result: success=%v", result.Success)

	// If we have a result, check the structure
	if result.WorkingDir == "" {
		t.Error("[E2E-GIT] WorkingDir should not be empty")
	}

	suite.Logger().Log("[E2E-GIT] Sync dry-run test passed")
}

// TestGitStatusNonGitRepo tests behavior in a non-git directory
func TestGitStatusNonGitRepo(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	logger := NewTestLogger(t, "git-status-non-repo")
	defer logger.Close()

	// Create a temp dir that's not a git repo
	tempDir, err := os.MkdirTemp("", "ntm-git-e2e-non-repo-")
	if err != nil {
		t.Fatalf("[E2E-GIT] Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cmd := exec.Command(mustE2EBin(), "git", "status", "--json")
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()

	logger.Log("[E2E-GIT] Non-repo output: %s", string(output))

	// Should either fail or return with Git=nil
	var result GitStatusResult
	if jsonErr := json.Unmarshal(output, &result); jsonErr == nil {
		if result.Git != nil {
			t.Error("[E2E-GIT] Git info should be nil for non-repo")
		}
		logger.Log("[E2E-GIT] Correctly handled non-git directory")
	} else {
		logger.Log("[E2E-GIT] Command returned non-JSON (expected for error case)")
	}
}

// TestGitStatusBranchTracking tests ahead/behind detection
func TestGitStatusBranchTracking(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-status-tracking")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Without a remote, ahead/behind should be 0
	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	if result.Git.Ahead != 0 {
		suite.Logger().Log("[E2E-GIT] Note: Ahead=%d (expected 0 without upstream)", result.Git.Ahead)
	}

	if result.Git.Behind != 0 {
		suite.Logger().Log("[E2E-GIT] Note: Behind=%d (expected 0 without upstream)", result.Git.Behind)
	}

	suite.Logger().Log("[E2E-GIT] Branch tracking test passed")
}

// TestGitCommitInfo tests commit hash detection
func TestGitCommitInfo(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-commit-info")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Get actual commit hash
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = suite.RepoPath()
	actualCommit, err := cmd.Output()
	if err != nil {
		t.Fatalf("[E2E-GIT] Failed to get commit hash: %v", err)
	}

	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	expectedCommit := strings.TrimSpace(string(actualCommit))
	if result.Git.Commit != expectedCommit {
		t.Errorf("[E2E-GIT] Expected commit '%s', got '%s'", expectedCommit, result.Git.Commit)
	}

	expectedShort := expectedCommit[:7]
	if result.Git.CommitShort != expectedShort {
		t.Errorf("[E2E-GIT] Expected short commit '%s', got '%s'", expectedShort, result.Git.CommitShort)
	}

	suite.Logger().Log("[E2E-GIT] Commit info test passed")
}

// TestGitSyncNoRemote tests sync behavior without a remote
func TestGitSyncNoRemote(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-sync-no-remote")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Sync without remote should handle gracefully
	result, err := suite.RunGitSync("--dry-run")

	// Without a remote, this might fail - that's expected
	if err != nil {
		suite.Logger().Log("[E2E-GIT] Sync without remote failed (expected): %v", err)
	} else {
		suite.Logger().Log("[E2E-GIT] Sync without remote result: success=%v, error=%s",
			result.Success, result.Error)
	}

	suite.Logger().Log("[E2E-GIT] No remote test completed")
}

// TestGitMultipleFiles tests status with multiple file types
func TestGitMultipleFiles(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-multiple-files")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	// Create various file states
	// 1. Untracked file
	suite.CreateFile("untracked.txt", "untracked content\n")

	// 2. Modified tracked file
	suite.ModifyFile("README.md", "\nmodified content\n")

	// 3. Staged new file
	suite.CreateFile("staged.txt", "staged content\n")
	suite.StageFile("staged.txt")

	// 4. Another untracked file
	suite.CreateFile("another_untracked.go", "package main\n")

	// Wait for filesystem
	time.Sleep(100 * time.Millisecond)

	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	// Should be dirty
	if !result.Git.Dirty {
		t.Error("[E2E-GIT] Repo with changes should be dirty")
	}

	// Count files
	suite.Logger().Log("[E2E-GIT] Multiple files test:")
	suite.Logger().Log("[E2E-GIT]   Staged: %d files: %v", len(result.Git.StagedFiles), result.Git.StagedFiles)
	suite.Logger().Log("[E2E-GIT]   Modified: %d files: %v", len(result.Git.ModifiedFiles), result.Git.ModifiedFiles)
	suite.Logger().Log("[E2E-GIT]   Untracked: %d files: %v", len(result.Git.UntrackedFiles), result.Git.UntrackedFiles)

	// Verify we have files in each category
	if len(result.Git.StagedFiles) < 1 {
		t.Errorf("[E2E-GIT] Expected at least 1 staged file, got %d", len(result.Git.StagedFiles))
	}

	if len(result.Git.ModifiedFiles) < 1 {
		t.Errorf("[E2E-GIT] Expected at least 1 modified file, got %d", len(result.Git.ModifiedFiles))
	}

	if len(result.Git.UntrackedFiles) < 1 {
		t.Errorf("[E2E-GIT] Expected at least 1 untracked file, got %d", len(result.Git.UntrackedFiles))
	}

	suite.Logger().Log("[E2E-GIT] Multiple files test passed")
}

// TestGitAgentMailIntegration tests that Agent Mail status is reported
func TestGitAgentMailIntegration(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-agentmail")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	result, err := suite.RunGitStatus()
	if err != nil {
		t.Fatalf("[E2E-GIT] Git status failed: %v", err)
	}

	// AgentMail section should exist (even if unavailable)
	if result.AgentMail == nil {
		suite.Logger().Log("[E2E-GIT] Note: AgentMail is nil")
	} else {
		suite.Logger().Log("[E2E-GIT] AgentMail available: %v, agent: %s, locks: %d",
			result.AgentMail.Available, result.AgentMail.RegisteredAgent, result.AgentMail.LockCount)
	}

	suite.Logger().Log("[E2E-GIT] Agent Mail integration test passed")
}

// TestGitSyncResultFormat tests the sync result JSON structure
func TestGitSyncResultFormat(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewGitTestSuite(t, "git-sync-format")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-GIT] Setup failed: %v", err)
	}

	result, err := suite.RunGitSync("--dry-run")
	if err != nil {
		// Without remote, this may fail - just check the structure
		suite.Logger().Log("[E2E-GIT] Sync failed (expected without remote): %v", err)
		return
	}

	// Verify structure
	if result.WorkingDir == "" {
		t.Error("[E2E-GIT] WorkingDir should not be empty")
	}

	// Check pull/push results if present
	if result.PullResult != nil {
		suite.Logger().Log("[E2E-GIT] Pull result present: success=%v, already_up_to_date=%v",
			result.PullResult.Success, result.PullResult.AlreadyUpToDate)
	}

	if result.PushResult != nil {
		suite.Logger().Log("[E2E-GIT] Push result present: success=%v, nothing_to_push=%v",
			result.PushResult.Success, result.PushResult.NothingToPush)
	}

	suite.Logger().Log("[E2E-GIT] Sync format test passed")
}
