//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-POLICY] Tests for ntm policy automation.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// PolicyAutomationResponse mirrors the JSON output from `ntm policy automation --json`.
type PolicyAutomationResponse struct {
	GeneratedAt  time.Time `json:"generated_at"`
	AutoCommit   bool      `json:"auto_commit"`
	AutoPush     bool      `json:"auto_push"`
	ForceRelease string    `json:"force_release"`
	Modified     bool      `json:"modified,omitempty"`
}

// PolicyAutomationSuite manages E2E tests for policy automation.
type PolicyAutomationSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
}

// NewPolicyAutomationSuite creates a new policy automation test suite.
func NewPolicyAutomationSuite(t *testing.T, scenario string) *PolicyAutomationSuite {
	SkipIfNoNTM(t)

	tempDir := t.TempDir()
	logger := NewTestLogger(t, "policy-automation-"+scenario)

	suite := &PolicyAutomationSuite{
		t:       t,
		logger:  logger,
		tempDir: tempDir,
	}

	t.Cleanup(func() {
		logger.Close()
	})

	return suite
}

func (s *PolicyAutomationSuite) runPolicyAutomation(args ...string) (*PolicyAutomationResponse, string, string, error) {
	baseArgs := []string{"policy", "automation"}
	baseArgs = append(baseArgs, args...)

	s.logger.Log("[E2E-POLICY] Running: ntm %s", strings.Join(baseArgs, " "))

	cmd := exec.Command(mustE2EBin(), baseArgs...)
	cmd.Env = append(os.Environ(), "HOME="+s.tempDir)
	cmd.Dir = s.tempDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	s.logger.Log("[E2E-POLICY] stdout: %s", stdoutStr)
	if stderrStr != "" {
		s.logger.Log("[E2E-POLICY] stderr: %s", stderrStr)
	}
	if err != nil {
		s.logger.Log("[E2E-POLICY] error: %v", err)
	}

	var resp PolicyAutomationResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-POLICY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-POLICY] Automation response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func TestPolicyAutomation_JSON_ShowDefaults(t *testing.T) {
	suite := NewPolicyAutomationSuite(t, "show-defaults")

	resp, _, _, err := suite.runPolicyAutomation("--json")
	if err != nil {
		t.Fatalf("policy automation show failed: %v", err)
	}
	if resp.GeneratedAt.IsZero() {
		t.Fatalf("expected generated_at timestamp to be set")
	}
	if !resp.AutoCommit {
		t.Fatalf("expected auto_commit default true, got false")
	}
	if resp.AutoPush {
		t.Fatalf("expected auto_push default false, got true")
	}
	if resp.ForceRelease != "approval" {
		t.Fatalf("expected force_release default 'approval', got %q", resp.ForceRelease)
	}
	if resp.Modified {
		t.Fatalf("expected modified=false for show path")
	}
}

func TestPolicyAutomation_JSON_Update(t *testing.T) {
	suite := NewPolicyAutomationSuite(t, "update-settings")

	resp, _, _, err := suite.runPolicyAutomation("--auto-push", "--no-auto-commit", "--force-release=auto", "--json")
	if err != nil {
		t.Fatalf("policy automation update failed: %v", err)
	}
	if !resp.Modified {
		t.Fatalf("expected modified=true after update")
	}
	if resp.AutoCommit {
		t.Fatalf("expected auto_commit false after update, got true")
	}
	if !resp.AutoPush {
		t.Fatalf("expected auto_push true after update, got false")
	}
	if resp.ForceRelease != "auto" {
		t.Fatalf("expected force_release 'auto', got %q", resp.ForceRelease)
	}

	showResp, _, _, err := suite.runPolicyAutomation("--json")
	if err != nil {
		t.Fatalf("policy automation show after update failed: %v", err)
	}
	if showResp.AutoCommit {
		t.Fatalf("expected auto_commit false after update, got true")
	}
	if !showResp.AutoPush {
		t.Fatalf("expected auto_push true after update, got false")
	}
	if showResp.ForceRelease != "auto" {
		t.Fatalf("expected force_release 'auto' after update, got %q", showResp.ForceRelease)
	}
}
