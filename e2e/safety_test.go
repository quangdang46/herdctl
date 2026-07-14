//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SAFETY] Tests for ntm safety status/check (destructive command protection).
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/policy"
)

// SafetyStatusResponse mirrors the JSON output from `ntm safety status --json`.
type SafetyStatusResponse struct {
	GeneratedAt   time.Time `json:"generated_at"`
	Installed     bool      `json:"installed"`
	PolicyPath    string    `json:"policy_path,omitempty"`
	BlockedCount  int       `json:"blocked_rules"`
	ApprovalCount int       `json:"approval_rules"`
	AllowedCount  int       `json:"allowed_rules"`
	WrapperPath   string    `json:"wrapper_path,omitempty"`
	HookInstalled bool      `json:"hook_installed"`
}

// SafetyCheckResponse mirrors the JSON output from `ntm safety check --json`.
type SafetyCheckResponse struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Command     string               `json:"command"`
	Action      string               `json:"action"`
	Pattern     string               `json:"pattern,omitempty"`
	Reason      string               `json:"reason,omitempty"`
	Policy      *SafetyPolicyVerdict `json:"policy,omitempty"`
	DCG         *SafetyDCGVerdict    `json:"dcg,omitempty"`
}

type SafetyPolicyVerdict struct {
	Action  string `json:"action"`
	Pattern string `json:"pattern,omitempty"`
	Reason  string `json:"reason,omitempty"`
	SLB     bool   `json:"slb,omitempty"`
}

type SafetyDCGVerdict struct {
	Available bool   `json:"available"`
	Checked   bool   `json:"checked"`
	Blocked   bool   `json:"blocked"`
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SafetyBlockedResponse mirrors the JSON output from `ntm safety blocked --json`.
type SafetyBlockedResponse struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Entries     []policy.BlockedEntry `json:"entries"`
	Count       int                   `json:"count"`
}

// SafetyInstallResponse mirrors the JSON output from `ntm safety install --json`.
type SafetyInstallResponse struct {
	Success    bool      `json:"success"`
	Timestamp  time.Time `json:"timestamp"`
	GitWrapper string    `json:"git_wrapper"`
	RMWrapper  string    `json:"rm_wrapper"`
	Hook       string    `json:"hook"`
	Policy     string    `json:"policy"`
}

// PolicyShowResponse mirrors the JSON output from `ntm policy show --json`.
type PolicyShowResponse struct {
	GeneratedAt time.Time        `json:"generated_at"`
	Version     int              `json:"version"`
	PolicyPath  string           `json:"policy_path,omitempty"`
	IsDefault   bool             `json:"is_default"`
	Stats       PolicyStats      `json:"stats"`
	Automation  PolicyAutomation `json:"automation"`
	Rules       *PolicyRules     `json:"rules,omitempty"`
}

type PolicyStats struct {
	Blocked  int `json:"blocked"`
	Approval int `json:"approval"`
	Allowed  int `json:"allowed"`
	SLBRules int `json:"slb_rules"`
}

type PolicyAutomation struct {
	AutoPush     bool   `json:"auto_push"`
	AutoCommit   bool   `json:"auto_commit"`
	ForceRelease string `json:"force_release"`
}

type PolicyRules struct {
	Blocked          []PolicyRule `json:"blocked,omitempty"`
	ApprovalRequired []PolicyRule `json:"approval_required,omitempty"`
	Allowed          []PolicyRule `json:"allowed,omitempty"`
}

type PolicyRule struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason,omitempty"`
	SLB     bool   `json:"slb,omitempty"`
}

// PolicyValidateResponse mirrors the JSON output from `ntm policy validate --json`.
type PolicyValidateResponse struct {
	GeneratedAt time.Time `json:"generated_at"`
	Valid       bool      `json:"valid"`
	PolicyPath  string    `json:"policy_path"`
	Errors      []string  `json:"errors,omitempty"`
	Warnings    []string  `json:"warnings,omitempty"`
}

// PolicyResetResponse mirrors the JSON output from `ntm policy reset --json`.
type PolicyResetResponse struct {
	Success    bool   `json:"success"`
	PolicyPath string `json:"policy_path"`
	Action     string `json:"action"`
}

// SafetyTestSuite manages E2E tests for safety commands.
type SafetyTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
}

// NewSafetyTestSuite creates a new safety test suite.
func NewSafetyTestSuite(t *testing.T, scenario string) *SafetyTestSuite {
	SkipIfNoNTM(t)

	tempDir := t.TempDir()
	logger := NewTestLogger(t, "safety-"+scenario)

	suite := &SafetyTestSuite{
		t:       t,
		logger:  logger,
		tempDir: tempDir,
	}

	t.Cleanup(func() {
		logger.Close()
	})

	return suite
}

func (s *SafetyTestSuite) runSafetyStatus() (*SafetyStatusResponse, string, string, error) {
	args := []string{"safety", "status", "--json"}
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

	var resp SafetyStatusResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Status response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runSafetyCheck(command string) (*SafetyCheckResponse, string, string, error) {
	args := []string{"safety", "check", command, "--json"}
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

	var resp SafetyCheckResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Check response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runSafetyInstall(force bool) (*SafetyInstallResponse, string, string, error) {
	args := []string{"safety", "install", "--json"}
	if force {
		args = append(args, "--force")
	}
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

	var resp SafetyInstallResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Install response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runPolicyShow(showAll bool) (*PolicyShowResponse, string, string, error) {
	args := []string{"policy", "show", "--json"}
	if showAll {
		args = append(args, "--all")
	}
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

	var resp PolicyShowResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Policy show response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runPolicyValidate(path string) (*PolicyValidateResponse, string, string, error) {
	args := []string{"policy", "validate", "--json"}
	if path != "" {
		args = append(args, path)
	}
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

	var resp PolicyValidateResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Policy validate response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runPolicyReset(force bool) (*PolicyResetResponse, string, string, error) {
	args := []string{"policy", "reset", "--json"}
	if force {
		args = append(args, "--force")
	}
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

	var resp PolicyResetResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Policy reset response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) runSafetyBlocked(hours, limit int) (*SafetyBlockedResponse, string, string, error) {
	args := []string{"safety", "blocked", "--json"}
	if hours > 0 {
		args = append(args, "--hours", strconv.Itoa(hours))
	}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
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

	var resp SafetyBlockedResponse
	if jsonErr := json.Unmarshal([]byte(stdoutStr), &resp); jsonErr != nil {
		s.logger.Log("[E2E-SAFETY] JSON parse error: %v", jsonErr)
		return nil, stdoutStr, stderrStr, jsonErr
	}

	s.logger.LogJSON("[E2E-SAFETY] Blocked response", resp)
	return &resp, stdoutStr, stderrStr, err
}

func (s *SafetyTestSuite) writeBlockedLog(entries []policy.BlockedEntry) (string, error) {
	logDir := filepath.Join(s.tempDir, ".ntm", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", err
	}
	logPath := filepath.Join(logDir, "blocked.jsonl")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, entry := range entries {
		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}
		if err := enc.Encode(entry); err != nil {
			return "", err
		}
	}
	return logPath, nil
}

func TestSafetyStatus_JSON(t *testing.T) {
	suite := NewSafetyTestSuite(t, "status")

	resp, _, _, err := suite.runSafetyStatus()
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse status response: %v", err)
	}

	if resp.GeneratedAt.IsZero() {
		t.Errorf("[E2E-SAFETY] Expected generated_at to be set")
	}
	if resp.BlockedCount <= 0 {
		t.Errorf("[E2E-SAFETY] Expected blocked_rules > 0, got %d", resp.BlockedCount)
	}
	if resp.AllowedCount <= 0 {
		t.Errorf("[E2E-SAFETY] Expected allowed_rules > 0, got %d", resp.AllowedCount)
	}
	if resp.ApprovalCount <= 0 {
		t.Errorf("[E2E-SAFETY] Expected approval_rules > 0, got %d", resp.ApprovalCount)
	}
	if resp.WrapperPath == "" {
		t.Errorf("[E2E-SAFETY] Expected wrapper_path to be set")
	}

	suite.logger.Log("[E2E-SAFETY] safety_status_test_completed")
}

func TestPolicyShow_JSON_Default(t *testing.T) {
	suite := NewSafetyTestSuite(t, "policy-show")

	resp, _, _, err := suite.runPolicyShow(false)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse policy show response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for policy show, got error: %v", err)
	}
	if resp.GeneratedAt.IsZero() {
		t.Errorf("[E2E-SAFETY] Expected generated_at to be set")
	}
	if !resp.IsDefault {
		t.Errorf("[E2E-SAFETY] Expected is_default=true when no custom policy exists")
	}
	if resp.Version <= 0 {
		t.Errorf("[E2E-SAFETY] Expected version > 0, got %d", resp.Version)
	}
	if resp.Stats.Blocked == 0 || resp.Stats.Approval == 0 || resp.Stats.Allowed == 0 {
		t.Errorf("[E2E-SAFETY] Expected non-zero rule stats, got %+v", resp.Stats)
	}

	suite.logger.Log("[E2E-SAFETY] policy_show_default_completed")
}

func TestPolicyShow_JSON_All(t *testing.T) {
	suite := NewSafetyTestSuite(t, "policy-show-all")

	resp, _, _, err := suite.runPolicyShow(true)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse policy show response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for policy show --all, got error: %v", err)
	}
	if resp.Rules == nil {
		t.Fatalf("[E2E-SAFETY] Expected rules to be populated with --all")
	}
	if len(resp.Rules.Blocked) == 0 || len(resp.Rules.ApprovalRequired) == 0 || len(resp.Rules.Allowed) == 0 {
		t.Fatalf("[E2E-SAFETY] Expected rule lists to be non-empty, got %+v", resp.Rules)
	}

	suite.logger.Log("[E2E-SAFETY] policy_show_all_completed")
}

func TestPolicyValidate_JSON_ValidFile(t *testing.T) {
	suite := NewSafetyTestSuite(t, "policy-validate-valid")

	policyPath := filepath.Join(suite.tempDir, "policy.yaml")
	policyData := []byte(strings.Join([]string{
		"version: 1",
		"blocked:",
		"  - pattern: \"rm\\\\s+-rf\\\\s+/$\"",
		"    reason: \"dangerous\"",
		"automation:",
		"  auto_push: false",
		"  auto_commit: true",
		"  force_release: approval",
		"",
	}, "\n"))
	if err := os.WriteFile(policyPath, policyData, 0644); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to write policy file: %v", err)
	}

	resp, _, _, err := suite.runPolicyValidate(policyPath)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse policy validate response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for valid policy, got error: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("[E2E-SAFETY] Expected valid=true, got false (errors=%v)", resp.Errors)
	}
	if resp.PolicyPath != policyPath {
		t.Errorf("[E2E-SAFETY] Expected policy_path %q, got %q", policyPath, resp.PolicyPath)
	}

	suite.logger.Log("[E2E-SAFETY] policy_validate_valid_completed")
}

func TestPolicyValidate_JSON_MissingFile(t *testing.T) {
	suite := NewSafetyTestSuite(t, "policy-validate-missing")

	missingPath := filepath.Join(suite.tempDir, "missing-policy.yaml")
	resp, _, _, err := suite.runPolicyValidate(missingPath)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse policy validate response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for policy validate missing file, got error: %v", err)
	}
	if resp.Valid {
		t.Fatalf("[E2E-SAFETY] Expected valid=false for missing policy file")
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("[E2E-SAFETY] Expected errors for missing policy file")
	}

	suite.logger.Log("[E2E-SAFETY] policy_validate_missing_completed")
}

func TestPolicyReset_JSON(t *testing.T) {
	suite := NewSafetyTestSuite(t, "policy-reset")

	resp, _, _, err := suite.runPolicyReset(true)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse policy reset response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for policy reset, got error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("[E2E-SAFETY] Expected success=true, got false")
	}
	if resp.Action != "reset" {
		t.Fatalf("[E2E-SAFETY] Expected action=reset, got %q", resp.Action)
	}
	if resp.PolicyPath == "" {
		t.Fatalf("[E2E-SAFETY] Expected policy_path to be set")
	}
	if _, statErr := os.Stat(resp.PolicyPath); statErr != nil {
		t.Fatalf("[E2E-SAFETY] Expected policy file to exist: %v", statErr)
	}

	suite.logger.Log("[E2E-SAFETY] policy_reset_completed")
}

func TestSafetyInstall_JSON(t *testing.T) {
	suite := NewSafetyTestSuite(t, "install")

	resp, _, _, err := suite.runSafetyInstall(false)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse install response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for safety install, got error: %v", err)
	}
	if !resp.Success {
		t.Fatalf("[E2E-SAFETY] Expected success=true, got false")
	}
	if resp.GitWrapper == "" || resp.RMWrapper == "" || resp.Hook == "" || resp.Policy == "" {
		t.Fatalf("[E2E-SAFETY] Expected wrapper/hook/policy paths to be set, got %+v", resp)
	}

	for _, path := range []string{resp.GitWrapper, resp.RMWrapper, resp.Hook, resp.Policy} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("[E2E-SAFETY] Expected path to exist: %s (err=%v)", path, statErr)
		}
	}

	status, _, _, statusErr := suite.runSafetyStatus()
	if status == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse status response: %v", statusErr)
	}
	if !status.Installed {
		t.Fatalf("[E2E-SAFETY] Expected installed=true after safety install")
	}

	suite.logger.Log("[E2E-SAFETY] safety_install_test_completed")
}

func TestSafetyCheck_Blocked(t *testing.T) {
	suite := NewSafetyTestSuite(t, "check-blocked")

	command := "git reset --hard HEAD~1"
	resp, _, _, err := suite.runSafetyCheck(command)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse check response: %v", err)
	}

	if resp.Action != "block" {
		t.Errorf("[E2E-SAFETY] Expected action=block, got %q", resp.Action)
	}
	if resp.Policy == nil || resp.Policy.Action != "block" {
		t.Errorf("[E2E-SAFETY] Expected policy action=block, got %+v", resp.Policy)
	}
	if resp.Pattern == "" {
		t.Errorf("[E2E-SAFETY] Expected non-empty pattern for blocked command")
	}
	if err == nil {
		t.Errorf("[E2E-SAFETY] Expected non-nil error (exit code 1) for blocked command")
	}

	suite.logger.Log("[E2E-SAFETY] safety_check_blocked_completed")
}

func TestSafetyCheck_Allowed(t *testing.T) {
	suite := NewSafetyTestSuite(t, "check-allowed")

	command := "git reset --soft HEAD~1"
	resp, _, _, err := suite.runSafetyCheck(command)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse check response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for allowed command, got error: %v", err)
	}

	if resp.Action != "allow" {
		t.Errorf("[E2E-SAFETY] Expected action=allow, got %q", resp.Action)
	}
	if resp.Policy == nil || resp.Policy.Action != "allow" {
		t.Errorf("[E2E-SAFETY] Expected policy action=allow, got %+v", resp.Policy)
	}
	if resp.Pattern == "" {
		t.Errorf("[E2E-SAFETY] Expected non-empty pattern for allowed command")
	}

	suite.logger.Log("[E2E-SAFETY] safety_check_allowed_completed")
}

func TestSafetyCheck_ApprovalRequired(t *testing.T) {
	suite := NewSafetyTestSuite(t, "check-approve")

	command := "rm -rf /tmp/ntm-test"
	resp, _, _, err := suite.runSafetyCheck(command)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse check response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for approve action, got error: %v", err)
	}

	if resp.Action != "approve" {
		t.Errorf("[E2E-SAFETY] Expected action=approve, got %q", resp.Action)
	}
	if resp.Policy == nil || resp.Policy.Action != "approve" {
		t.Errorf("[E2E-SAFETY] Expected policy action=approve, got %+v", resp.Policy)
	}
	if resp.Pattern == "" {
		t.Errorf("[E2E-SAFETY] Expected non-empty pattern for approval-required command")
	}

	suite.logger.Log("[E2E-SAFETY] safety_check_approval_completed")
}

func TestSafetyCheck_RCHWrapperPassThrough(t *testing.T) {
	suite := NewSafetyTestSuite(t, "check-rch-wrapper")

	ntmDir := filepath.Join(suite.tempDir, ".ntm")
	if err := os.MkdirAll(ntmDir, 0755); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to create ntm dir: %v", err)
	}

	policyPath := filepath.Join(ntmDir, "policy.yaml")
	policyData := []byte(strings.Join([]string{
		"version: 1",
		"approval_required:",
		"  - pattern: \"^rch\\\\b\"",
		"    reason: \"RCH commands require review\"",
		"",
	}, "\n"))
	if err := os.WriteFile(policyPath, policyData, 0644); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to write policy file: %v", err)
	}

	binDir := filepath.Join(suite.tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to create bin dir: %v", err)
	}

	dcgPath := filepath.Join(binDir, "dcg")
	dcgScript := `#!/bin/bash
set -euo pipefail

if [ "$1" = "check" ]; then
  cmd="${@: -1}"
  if [[ "$cmd" == rch* ]]; then
    printf '{"command":"%s","reason":"blocked by fake dcg"}\n' "$cmd"
    exit 1
  fi
  exit 0
fi

if [ "$1" = "--version" ]; then
  echo "dcg 0.1.0"
  exit 0
fi

exit 0
`
	if err := os.WriteFile(dcgPath, []byte(dcgScript), 0755); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to write fake dcg: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	command := "rch build cargo -- build"
	resp, _, _, err := suite.runSafetyCheck(command)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse check response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for rch command, got error: %v", err)
	}
	if resp.Action != "approve" {
		t.Errorf("[E2E-SAFETY] Expected action=approve for rch command, got %q", resp.Action)
	}
	if resp.DCG == nil || !resp.DCG.Available || !resp.DCG.Checked || resp.DCG.Blocked {
		t.Errorf("[E2E-SAFETY] Expected dcg to allow rch wrapper via passthrough, got %+v", resp.DCG)
	}

	suite.logger.Log("[E2E-SAFETY] safety_check_rch_wrapper_completed")
}

func TestSafetyBlocked_JSON(t *testing.T) {
	suite := NewSafetyTestSuite(t, "blocked")

	entry := policy.BlockedEntry{
		Timestamp: time.Now().Add(-time.Minute),
		Command:   "rm -rf /",
		Pattern:   `rm\\s+-rf\\s+/$`,
		Reason:    "Recursive delete of root is catastrophic",
		Action:    policy.ActionBlock,
	}

	if _, err := suite.writeBlockedLog([]policy.BlockedEntry{entry}); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to write blocked log: %v", err)
	}

	resp, _, _, err := suite.runSafetyBlocked(24, 20)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse blocked response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for safety blocked, got error: %v", err)
	}

	if resp.Count < 1 {
		t.Fatalf("[E2E-SAFETY] Expected count >= 1, got %d", resp.Count)
	}
	if len(resp.Entries) < 1 {
		t.Fatalf("[E2E-SAFETY] Expected entries >= 1, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Command != entry.Command {
		t.Errorf("[E2E-SAFETY] Expected first entry command %q, got %q", entry.Command, resp.Entries[0].Command)
	}
	if resp.Entries[0].Pattern == "" {
		t.Errorf("[E2E-SAFETY] Expected pattern to be populated")
	}

	suite.logger.Log("[E2E-SAFETY] safety_blocked_test_completed")
}
