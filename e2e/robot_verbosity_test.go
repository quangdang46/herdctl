//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-ROBOT-TERSE] Tests for robot verbosity/short-keys/pagination behavior.
// Bead: bd-hs3a0 - Task: E2E robot verbosity/short-keys/pagination
package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func runRobotVerbosityCmd(t *testing.T, args ...string) []byte {
	t.Helper()

	cmd := exec.Command(mustE2EBin(), args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("[E2E-ROBOT-TERSE] command failed: %v output=%s", err, string(output))
	}
	return output
}

func supportsRobotVerbosityFlag(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command(mustE2EBin(), "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--robot-verbosity")
}

func supportsRobotFormatFlag(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command(mustE2EBin(), "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--robot-format")
}

func createTempSession(t *testing.T, suite *TestSuite, label string) string {
	t.Helper()
	session := fmt.Sprintf("e2e_%s_%d", label, time.Now().UnixNano())
	cmd := exec.Command(tmux.BinaryPath(), "new-session", "-d", "-s", session, "-x", "200", "-y", "50")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("[E2E-ROBOT-PAGE] create session failed: %v output=%s", err, string(out))
	}

	suite.cleanup = append(suite.cleanup, func() {
		exec.Command(tmux.BinaryPath(), "kill-session", "-t", session).Run()
	})
	return session
}

func terseFieldCount(out string) int {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, ";"))
}

func TestE2E_RobotVerbosityShortKeysPagination(t *testing.T) {
	CommonE2EPrerequisites(t)
	if !supportsRobotVerbosityFlag(t) {
		t.Skip("ntm --robot-verbosity not supported by current binary")
	}

	suite := NewTestSuite(t, "robot_verbosity")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-ROBOT-TERSE] setup failed: %v", err)
	}

	// Default verbosity: long keys
	defaultOut := runRobotVerbosityCmd(t, "--robot-status")
	var defaultPayload map[string]any
	if err := json.Unmarshal(defaultOut, &defaultPayload); err != nil {
		t.Fatalf("[E2E-ROBOT-SHORT] invalid JSON: %v", err)
	}
	defaultKeys := make([]string, 0, len(defaultPayload))
	for k := range defaultPayload {
		defaultKeys = append(defaultKeys, k)
	}
	log.Printf("[E2E-ROBOT-SHORT] enabled=%t keys=%v", false, defaultKeys)
	if _, ok := defaultPayload["success"]; !ok {
		t.Fatalf("[E2E-ROBOT-SHORT] expected success in default verbosity")
	}
	if _, ok := defaultPayload["timestamp"]; !ok {
		t.Fatalf("[E2E-ROBOT-SHORT] expected timestamp in default verbosity")
	}
	if _, ok := defaultPayload["ok"]; ok {
		t.Fatalf("[E2E-ROBOT-SHORT] unexpected ok key in default verbosity")
	}

	// Terse verbosity: short keys
	terseOut := runRobotVerbosityCmd(t, "--robot-status", "--robot-verbosity=terse")
	log.Printf("[E2E-ROBOT-TERSE] mode=%s fields=%d bytes=%d", "terse", terseFieldCount(string(terseOut)), len(terseOut))

	var tersePayload map[string]any
	if err := json.Unmarshal(terseOut, &tersePayload); err != nil {
		t.Fatalf("[E2E-ROBOT-TERSE] invalid JSON: %v", err)
	}
	terseKeys := make([]string, 0, len(tersePayload))
	for k := range tersePayload {
		terseKeys = append(terseKeys, k)
	}
	log.Printf("[E2E-ROBOT-SHORT] enabled=%t keys=%v", true, terseKeys)

	if _, ok := tersePayload["ok"]; !ok {
		t.Fatalf("[E2E-ROBOT-TERSE] expected ok key in terse output")
	}
	if _, ok := tersePayload["ts"]; !ok {
		t.Fatalf("[E2E-ROBOT-TERSE] expected ts key in terse output")
	}
	if _, ok := tersePayload["v"]; !ok {
		t.Fatalf("[E2E-ROBOT-TERSE] expected v key in terse output")
	}
	if _, ok := tersePayload["of"]; !ok {
		t.Fatalf("[E2E-ROBOT-TERSE] expected of key in terse output")
	}
	if _, ok := tersePayload["s"]; !ok {
		t.Fatalf("[E2E-ROBOT-TERSE] expected s (sessions) in terse output")
	}
	if _, ok := tersePayload["success"]; ok {
		t.Fatalf("[E2E-ROBOT-TERSE] unexpected success key in terse output")
	}
	if _, ok := tersePayload["_agent_hints"]; ok {
		t.Fatalf("[E2E-ROBOT-TERSE] unexpected _agent_hints in terse output")
	}

	if supportsRobotFormatFlag(t) {
		toonOut := runRobotVerbosityCmd(t, "--robot-status", "--robot-verbosity=terse", "--robot-format=toon")
		log.Printf("[E2E-ROBOT-TERSE] mode=%s fields=%d bytes=%d", "toon", terseFieldCount(string(toonOut)), len(toonOut))
		trimmed := strings.TrimSpace(string(toonOut))
		if trimmed == "" {
			t.Fatalf("[E2E-ROBOT-TERSE] expected non-empty TOON output")
		}
		if json.Valid([]byte(trimmed)) {
			t.Fatalf("[E2E-ROBOT-TERSE] TOON output should not be valid JSON: %s", trimmed)
		}
		if !strings.Contains(trimmed, "ok") {
			t.Fatalf("[E2E-ROBOT-TERSE] TOON output missing ok key: %s", trimmed)
		}
	}

	// Pagination: create extra sessions to ensure total > limit
	_ = createTempSession(t, suite, "verbosity_p1")
	_ = createTempSession(t, suite, "verbosity_p2")

	pageOut := runRobotVerbosityCmd(t, "--robot-status", "--robot-limit=1", "--robot-offset=0")
	var pagePayload struct {
		Pagination struct {
			Limit      int  `json:"limit"`
			Count      int  `json:"count"`
			HasMore    bool `json:"has_more"`
			NextCursor *int `json:"next_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(pageOut, &pagePayload); err != nil {
		t.Fatalf("[E2E-ROBOT-PAGE] invalid JSON: %v", err)
	}
	log.Printf("[E2E-ROBOT-PAGE] limit=%d count=%d next=%v", pagePayload.Pagination.Limit, pagePayload.Pagination.Count, pagePayload.Pagination.NextCursor)
	if pagePayload.Pagination.Limit != 1 {
		t.Fatalf("[E2E-ROBOT-PAGE] pagination.limit=%d want=1", pagePayload.Pagination.Limit)
	}
	if !pagePayload.Pagination.HasMore || pagePayload.Pagination.NextCursor == nil {
		t.Fatalf("[E2E-ROBOT-PAGE] expected has_more and next_cursor, got %+v", pagePayload.Pagination)
	}

	// --robot-terse stability: should be non-JSON and stable format
	terseLine := runRobotVerbosityCmd(t, "--robot-terse")
	trimmedTerse := strings.TrimSpace(string(terseLine))
	log.Printf("[E2E-ROBOT-TERSE] mode=%s fields=%d bytes=%d", "robot-terse", terseFieldCount(string(terseLine)), len(terseLine))
	if trimmedTerse != "" && json.Valid([]byte(trimmedTerse)) {
		t.Fatalf("[E2E-ROBOT-TERSE] --robot-terse should not return JSON: %s", trimmedTerse)
	}

	// Allow warnings or other preamble lines; verify at least one terse line starts with S:
	if trimmedTerse != "" {
		found := false
		for _, line := range strings.Split(trimmedTerse, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "S:") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("[E2E-ROBOT-TERSE] expected terse output line to start with S:, got %s", trimmedTerse)
		}
	}
}
