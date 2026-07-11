//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// robot_format_test.go validates --robot-format selection for JSON/TOON/auto.
//
// Bead: bd-1a6c4 - Task: E2E robot-format selection (json/toon/auto)
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runRobotFormatCmd executes an ntm robot command with a specific format.
func runRobotFormatCmd(t *testing.T, suite *TestSuite, format string, cmd string, args ...string) []byte {
	t.Helper()

	fullArgs := append([]string{fmt.Sprintf("--robot-format=%s", format)}, args...)
	command := exec.Command("ntm", fullArgs...)
	output, err := command.CombinedOutput()

	suite.Logger().Log("[E2E-ROBOT-FORMAT] format=%s cmd=%s bytes=%d", format, cmd, len(output))

	if err != nil {
		suite.Logger().Log("[E2E-ROBOT-FORMAT] error cmd=%s err=%v output=%s", cmd, err, string(output))
		t.Fatalf("[E2E-ROBOT-FORMAT] Command failed: %v", err)
	}

	return output
}

func parseJSONOrFail(t *testing.T, output []byte) map[string]interface{} {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("[E2E-ROBOT-FORMAT] JSON parse failed: %v output=%s", err, string(output))
	}
	return parsed
}

func TestE2E_RobotFormatSelection(t *testing.T) {
	CommonE2EPrerequisites(t)
	if !supportsRobotFormat(t) {
		t.Skip("ntm --robot-format not supported by current binary")
	}

	suite := NewTestSuite(t, "robot_format")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-ROBOT-FORMAT] Setup failed: %v", err)
	}

	session := suite.Session()

	t.Run("status_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "json", "robot-status", "--robot-status")
		parsed := parseJSONOrFail(t, output)

		if _, ok := parsed["sessions"]; !ok {
			t.Fatalf("[E2E-ROBOT-FORMAT] status JSON missing sessions: %v", parsed)
		}
	})

	t.Run("status_toon", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "toon", "robot-status", "--robot-status")

		trimmed := strings.TrimSpace(string(output))
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			if json.Valid([]byte(trimmed)) {
				t.Fatalf("[E2E-ROBOT-FORMAT] status TOON should not be valid JSON, got: %s", string(output))
			}
		}
		if !strings.Contains(string(output), "sessions") {
			t.Fatalf("[E2E-ROBOT-FORMAT] status TOON missing sessions field: %s", string(output))
		}
	})

	t.Run("status_auto_defaults_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "auto", "robot-status", "--robot-status")
		parseJSONOrFail(t, output)
		suite.Logger().Log("[E2E-ROBOT-FORMAT] fallback=%t reason=%s", true, "auto defaults to JSON")
	})

	t.Run("assign_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "json", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		parsed := parseJSONOrFail(t, output)

		if parsed["session"] != session {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign JSON session mismatch: got=%v want=%s", parsed["session"], session)
		}
		if _, ok := parsed["strategy"]; !ok {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign JSON missing strategy: %v", parsed)
		}
	})

	t.Run("assign_toon", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "toon", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		if !strings.Contains(string(output), "session:") {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign TOON missing session: %s", string(output))
		}
		if !strings.Contains(string(output), "strategy:") {
			t.Fatalf("[E2E-ROBOT-FORMAT] assign TOON missing strategy: %s", string(output))
		}
	})

	t.Run("assign_auto_defaults_json", func(t *testing.T) {
		output := runRobotFormatCmd(t, suite, "auto", "robot-assign", fmt.Sprintf("--robot-assign=%s", session))
		parseJSONOrFail(t, output)
	})
}

func TestE2ERobotCapabilitiesBuiltBinaryBudgets(t *testing.T) {
	CommonE2EPrerequisites(t)
	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}

	type capabilityCommand struct {
		Name string `json:"name"`
		Flag string `json:"flag"`
	}
	type capabilityEnvelope struct {
		Success  bool                `json:"success"`
		Commands []capabilityCommand `json:"commands"`
		Filter   *struct {
			Command string `json:"command,omitempty"`
			Compact bool   `json:"compact,omitempty"`
		} `json:"filter,omitempty"`
	}
	run := func(t *testing.T, maxBytes int, args ...string) capabilityEnvelope {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("ntm %q: %v stdout=%s stderr=%s", args, err, stdout.Bytes(), stderr.Bytes())
		}
		if ctx.Err() != nil {
			t.Fatalf("ntm %q timed out: %v", args, ctx.Err())
		}
		if stderr.Len() != 0 {
			t.Fatalf("ntm %q wrote stderr: %s", args, stderr.Bytes())
		}
		payload := bytes.TrimSpace(stdout.Bytes())
		if len(payload) == 0 || len(payload) >= maxBytes {
			t.Fatalf("ntm %q payload size=%d, require 0 < size < %d", args, len(payload), maxBytes)
		}
		if !json.Valid(payload) {
			t.Fatalf("ntm %q did not emit one valid JSON document: %s", args, payload)
		}
		var envelope capabilityEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode ntm %q capabilities: %v", args, err)
		}
		if !envelope.Success || envelope.Commands == nil {
			t.Fatalf("ntm %q capabilities envelope = %+v", args, envelope)
		}
		t.Logf("[E2E-CAPABILITIES] args=%q bytes=%d commands=%d", args, len(payload), len(envelope.Commands))
		return envelope
	}

	full := run(t, 400_000, "--robot-capabilities", "--robot-format=json")
	compact := run(t, 50_000, "--robot-capabilities", "--capability-compact", "--robot-format=json")
	exact := run(t, 4_000, "--robot-capabilities", "--capability-command=send", "--capability-compact", "--robot-format=json")

	if len(full.Commands) == 0 || len(compact.Commands) != len(full.Commands) {
		t.Fatalf("capability command counts full=%d compact=%d", len(full.Commands), len(compact.Commands))
	}
	if len(exact.Commands) != 1 || exact.Commands[0].Name != "send" || exact.Commands[0].Flag != "--robot-send" ||
		exact.Filter == nil || exact.Filter.Command != "send" || !exact.Filter.Compact {
		t.Fatalf("exact compact capability projection = %+v", exact)
	}
}

func supportsRobotFormat(t *testing.T) bool {
	t.Helper()
	cmd := exec.Command("ntm", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "--robot-format")
}
