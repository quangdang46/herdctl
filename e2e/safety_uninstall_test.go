//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SAFETY] Tests for ntm safety uninstall.
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

// SafetyUninstallResponse mirrors the JSON output from `ntm safety uninstall --json`.
type SafetyUninstallResponse struct {
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
	Removed   []string  `json:"removed"`
}

func (s *SafetyTestSuite) runSafetyUninstall() (*SafetyUninstallResponse, string, string, error) {
	args := []string{"safety", "uninstall", "--json"}
	s.logger.Log("[E2E-SAFETY] Running: ntm %s", strings.Join(args, " "))

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Env = append(os.Environ(), "HOME="+s.tempDir)
	cmd.Dir = s.tempDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	s.logger.Log("[E2E-SAFETY] stdout: %s", stdoutStr)
	if stderrStr != "" {
		s.logger.Log("[E2E-SAFETY] stderr: %s", stderrStr)
	}
	if err != nil {
		s.logger.Log("[E2E-SAFETY] error: %v", err)
	}

	var resp SafetyUninstallResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Uninstall response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func TestSafetyUninstall_JSON(t *testing.T) {
	suite := NewSafetyTestSuite(t, "uninstall")

	installResp, _, _, err := suite.runSafetyInstall(false)
	if installResp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse install response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected install exit code 0, got error: %v", err)
	}
	if !installResp.Success {
		t.Fatalf("[E2E-SAFETY] Expected install success=true, got false")
	}

	gitWrapper := filepath.Join(suite.tempDir, ".ntm", "bin", "git")
	rmWrapper := filepath.Join(suite.tempDir, ".ntm", "bin", "rm")
	hookPath := filepath.Join(suite.tempDir, ".claude", "hooks", "PreToolUse", "ntm-safety.sh")

	if _, err := os.Stat(gitWrapper); err != nil {
		t.Fatalf("[E2E-SAFETY] expected git wrapper to exist: %v", err)
	}
	if _, err := os.Stat(rmWrapper); err != nil {
		t.Fatalf("[E2E-SAFETY] expected rm wrapper to exist: %v", err)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("[E2E-SAFETY] expected hook to exist: %v", err)
	}

	resp, _, _, err := suite.runSafetyUninstall()
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse uninstall response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected uninstall exit code 0, got error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("[E2E-SAFETY] Expected uninstall success=true, got false")
	}
	if resp.Timestamp.IsZero() {
		t.Fatalf("[E2E-SAFETY] Expected timestamp to be set")
	}
	if len(resp.Removed) == 0 {
		t.Fatalf("[E2E-SAFETY] Expected removed list to be non-empty")
	}
	if !containsPath(resp.Removed, gitWrapper) {
		t.Fatalf("[E2E-SAFETY] Expected removed to include %s", gitWrapper)
	}
	if !containsPath(resp.Removed, rmWrapper) {
		t.Fatalf("[E2E-SAFETY] Expected removed to include %s", rmWrapper)
	}
	if !containsPath(resp.Removed, hookPath) {
		t.Fatalf("[E2E-SAFETY] Expected removed to include %s", hookPath)
	}

	if _, err := os.Stat(gitWrapper); !os.IsNotExist(err) {
		t.Fatalf("[E2E-SAFETY] expected git wrapper to be removed, got err=%v", err)
	}
	if _, err := os.Stat(rmWrapper); !os.IsNotExist(err) {
		t.Fatalf("[E2E-SAFETY] expected rm wrapper to be removed, got err=%v", err)
	}
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("[E2E-SAFETY] expected hook to be removed, got err=%v", err)
	}
}
