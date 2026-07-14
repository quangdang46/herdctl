//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-POLICY] Tests for ntm policy edit.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// PolicyEditSuite manages E2E tests for policy edit.
type PolicyEditSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
}

// NewPolicyEditSuite creates a new policy edit suite.
func NewPolicyEditSuite(t *testing.T, scenario string) *PolicyEditSuite {
	SkipIfNoNTM(t)

	tempDir := t.TempDir()
	logger := NewTestLogger(t, "policy-edit-"+scenario)

	suite := &PolicyEditSuite{
		t:       t,
		logger:  logger,
		tempDir: tempDir,
	}

	t.Cleanup(func() {
		logger.Close()
	})

	return suite
}

func (s *PolicyEditSuite) runPolicyEdit() (string, string, error) {
	args := []string{"policy", "edit"}
	s.logger.Log("[E2E-POLICY] Running: ntm %s", strings.Join(args, " "))

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Env = append(os.Environ(), "HOME="+s.tempDir, "EDITOR=true")
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

	return stdoutStr, stderrStr, err
}

func TestPolicyEdit_CreatesDefaultPolicy(t *testing.T) {
	suite := NewPolicyEditSuite(t, "creates-default")

	_, _, err := suite.runPolicyEdit()
	if err != nil {
		t.Fatalf("policy edit failed: %v", err)
	}

	policyPath := filepath.Join(suite.tempDir, ".ntm", "policy.yaml")
	if _, err := os.Stat(policyPath); err != nil {
		t.Fatalf("expected policy file to exist at %s: %v", policyPath, err)
	}
}
