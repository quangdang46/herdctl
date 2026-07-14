//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// robot_ms_test.go validates robot wrappers for the Meta Skill (ms) CLI.
//
// Bead: bd-2m8bs - Task: Add robot skill query wrapper for MS
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type msEnvelope struct {
	Success   bool   `json:"success"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

type msSearchOutput struct {
	msEnvelope
	Query  string          `json:"query"`
	Count  int             `json:"count"`
	Skills json.RawMessage `json:"skills"`
	Source string          `json:"source,omitempty"`
}

type msShowOutput struct {
	msEnvelope
	ID     string          `json:"id"`
	Skill  json.RawMessage `json:"skill,omitempty"`
	Source string          `json:"source,omitempty"`
}

func runMSCommand(t *testing.T, suite *TestSuite, ntmPath string, env []string, args ...string) ([]byte, int, time.Duration, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, ntmPath, args...)
	if env != nil {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		suite.Logger().Log("[E2E-MS] cmd=%s exit=%d duration=%s timeout=true", strings.Join(args, " "), exitCode, duration)
		slog.Error(fmt.Sprintf("[E2E-MS] cmd=%s exit=%d timeout=true", strings.Join(args, " "), exitCode))
		return output, exitCode, duration, fmt.Errorf("command timed out after 60s")
	}

	suite.Logger().Log("[E2E-MS] cmd=%s exit=%d duration=%s bytes=%d", strings.Join(args, " "), exitCode, duration, len(output))
	slog.Info(fmt.Sprintf("[E2E-MS] cmd=%s exit=%d bytes=%d", strings.Join(args, " "), exitCode, len(output)))
	return output, exitCode, duration, err
}

func parseMSJSON(t *testing.T, suite *TestSuite, label string, output []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(output, v); err != nil {
		t.Fatalf("[E2E-MS] %s JSON parse failed: %v output=%s", label, err, string(output))
	}
	suite.Logger().LogJSON("[E2E-MS] "+label+" parsed", v)
}

func extractSkillID(raw json.RawMessage) (string, int) {
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", 0
	}
	for _, item := range items {
		if id, ok := item["id"].(string); ok && id != "" {
			return id, len(items)
		}
		if id, ok := item["slug"].(string); ok && id != "" {
			return id, len(items)
		}
		if id, ok := item["name"].(string); ok && id != "" {
			return id, len(items)
		}
	}
	return "", len(items)
}

func TestE2E_RobotMS_MissingBinary(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "robot_ms_missing")
	defer suite.Teardown()

	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm not found on PATH")
	}
	if !supportsRobotFlagInHelp(ntmPath, "--robot-ms-search") {
		t.Skip("robot-ms-search not supported by current ntm binary")
	}

	output, exitCode, _, err := runMSCommand(t, suite, ntmPath, stripPathEnv(), "--robot-ms-search=commit workflow")
	if err != nil {
		suite.Logger().Log("[E2E-MS] missing binary err=%v", err)
	}

	var search msSearchOutput
	parseMSJSON(t, suite, "search_missing", output, &search)

	suite.Logger().Log("[E2E-MS] missing binary exit=%d success=%t", exitCode, search.Success)
	if search.Success {
		t.Fatalf("[E2E-MS] expected failure when ms missing")
	}
	if search.ErrorCode != "DEPENDENCY_MISSING" {
		t.Fatalf("[E2E-MS] expected DEPENDENCY_MISSING, got=%s", search.ErrorCode)
	}
	slog.Error(fmt.Sprintf("[E2E-MS] error=%s hint=%s", search.Error, search.Hint))
}

func TestE2E_RobotMS_SearchShow(t *testing.T) {
	CommonE2EPrerequisites(t)

	if _, err := exec.LookPath("ms"); err != nil {
		t.Skip("ms not installed; skipping search/show")
	}

	suite := NewTestSuite(t, "robot_ms_live")
	defer suite.Teardown()

	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm not found on PATH")
	}
	if !supportsRobotFlagInHelp(ntmPath, "--robot-ms-search") {
		t.Skip("robot-ms-search not supported by current ntm binary")
	}

	output, exitCode, _, err := runMSCommand(t, suite, ntmPath, nil, "--robot-ms-search=commit workflow")
	if err != nil {
		t.Fatalf("[E2E-MS] search failed: %v", err)
	}

	var search msSearchOutput
	parseMSJSON(t, suite, "search", output, &search)
	slog.Info(fmt.Sprintf("[E2E-MS] cmd=robot-ms-search exit=%d count=%d source=%s", exitCode, search.Count, search.Source))

	if !search.Success {
		slog.Error(fmt.Sprintf("[E2E-MS] error=%s hint=%s", search.Error, search.Hint))
		t.Fatalf("[E2E-MS] search failed: %s", search.Error)
	}
	if !json.Valid(search.Skills) {
		t.Fatalf("[E2E-MS] search skills JSON invalid")
	}

	skillID, _ := extractSkillID(search.Skills)
	if skillID != "" && supportsRobotFlagInHelp(ntmPath, "--robot-ms-show") {
		showOut, showExit, _, showErr := runMSCommand(t, suite, ntmPath, nil, "--robot-ms-show="+skillID)
		if showErr != nil {
			t.Fatalf("[E2E-MS] show failed: %v", showErr)
		}

		var show msShowOutput
		parseMSJSON(t, suite, "show", showOut, &show)
		slog.Info(fmt.Sprintf("[E2E-MS] cmd=robot-ms-show exit=%d id=%s source=%s", showExit, show.ID, show.Source))

		if !show.Success {
			slog.Error(fmt.Sprintf("[E2E-MS] error=%s hint=%s", show.Error, show.Hint))
			t.Fatalf("[E2E-MS] show failed: %s", show.Error)
		}
		if !json.Valid(show.Skill) {
			t.Fatalf("[E2E-MS] show skill JSON invalid")
		}
	} else {
		suite.Logger().Log("[E2E-MS] skipping show: no skill ID or flag unsupported")
	}
}
