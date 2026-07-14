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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/history"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

type robotBoundaryProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type robotProcessFixture struct {
	ntmPath    string
	tmuxPath   string
	session    string
	root       string
	projectDir string
	configDir  string
	dataDir    string
	env        []string
	paneID     string
}

func runBuiltRobotProcess(t *testing.T, ntmPath, dir string, env []string, args ...string) robotBoundaryProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ntmPath, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append([]string(nil), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm %q timed out", args)
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run ntm %q: %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	t.Logf("[E2E-ROBOT-PROCESS] exit=%d args=%q stdout=%s stderr=%s", exitCode, args, truncateString(stdout.String(), 500), truncateString(stderr.String(), 500))
	return robotBoundaryProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func decodeSingleRobotJSON(t *testing.T, payload []byte, destination any) {
	t.Helper()
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		t.Fatalf("expected exactly one valid JSON document, got %q", payload)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		t.Fatalf("decode robot JSON: %v\npayload=%s", err, payload)
	}
}

func newRobotProcessFixture(t *testing.T, scenario string) *robotProcessFixture {
	t.Helper()
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	root := t.TempDir()
	fixture := &robotProcessFixture{
		ntmPath:    ntmPath,
		tmuxPath:   tmuxPath,
		session:    fmt.Sprintf("ntm-e2e-%s-%d-%d", scenario, os.Getpid(), time.Now().UnixNano()),
		root:       root,
		projectDir: filepath.Join(root, "project"),
		configDir:  filepath.Join(root, "config"),
		dataDir:    filepath.Join(root, "data"),
	}
	homeDir := filepath.Join(root, "home")
	// tmux's Unix socket path is capped at roughly 108 bytes. Keep its private
	// root short even when Go's per-test temporary directory is deeply nested.
	tmuxDir := filepath.Join("/tmp", fmt.Sprintf("ntm-rp-%d-%d", os.Getpid(), time.Now().UnixNano()))
	for _, dir := range []string{fixture.projectDir, fixture.configDir, fixture.dataDir, homeDir, tmuxDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create process fixture directory %s: %v", dir, err)
		}
	}
	fixture.env = atomicAssignmentIsolatedEnv(map[string]string{
		"HOME":                homeDir,
		"XDG_CONFIG_HOME":     fixture.configDir,
		"XDG_DATA_HOME":       fixture.dataDir,
		"TMUX_TMPDIR":         tmuxDir,
		"NO_COLOR":            "1",
		"TERM":                "xterm-256color",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "",
		"TOON_DEFAULT_FORMAT": "",
	})

	tmuxConfig := filepath.Join(root, "tmux.conf")
	config := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(tmuxConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	fixture.paneID = strings.TrimSpace(fixture.mustTMUXOutput(t,
		"-f", tmuxConfig,
		"new-session", "-d", "-s", fixture.session,
		"-x", "160", "-y", "48", "-c", fixture.projectDir,
		"-P", "-F", "#{pane_id}",
		"/bin/bash --noprofile --norc -i",
	))
	if fixture.paneID == "" {
		t.Fatal("private tmux server returned an empty pane ID")
	}
	fixture.mustTMUXOutput(t, "select-pane", "-t", fixture.paneID, "-T", fixture.session+"__cod_1")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, fixture.tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), fixture.env...)
		_ = cmd.Run()
	})
	return fixture
}

func (f *robotProcessFixture) mustTMUXOutput(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("tmux %q timed out", args)
	}
	if err != nil {
		t.Fatalf("tmux %q: %v output=%s", args, err, output)
	}
	return string(output)
}

func (f *robotProcessFixture) waitForFileContents(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in %s", want, path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// runRobotFormatCmd executes an ntm robot command with a specific format.
func runRobotFormatCmd(t *testing.T, suite *TestSuite, format string, cmd string, args ...string) []byte {
	t.Helper()

	fullArgs := append([]string{fmt.Sprintf("--robot-format=%s", format)}, args...)
	command := exec.Command(mustE2EBin(), fullArgs...)
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

func TestE2ERobotReplayBuiltBinaryRealTmuxDeliversExactlyOnce(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "replay-once")

	readyPath := filepath.Join(fixture.root, "pane-ready")
	readyCommand := fmt.Sprintf("printf ready > %q", readyPath)
	fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", readyCommand)
	fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
	fixture.waitForFileContents(t, readyPath, "ready")

	marker := fmt.Sprintf("NTM_E2E_REPLAY_ONCE_%d", time.Now().UnixNano())
	markerPath := filepath.Join(fixture.root, "replay-markers.txt")
	prompt := fmt.Sprintf("printf '%%s\\n' %q >> %q", marker, markerPath)
	entry := history.HistoryEntry{
		ID:        fmt.Sprintf("%d-e2ereplay", time.Now().UnixMilli()),
		Timestamp: time.Now().UTC(),
		Session:   fixture.session,
		Targets:   []string{fixture.paneID},
		Prompt:    prompt,
		Source:    history.SourceCLI,
		Success:   true,
	}
	historyPayload, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal replay history entry: %v", err)
	}
	historyDir := filepath.Join(fixture.dataDir, "ntm")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		t.Fatalf("create replay history directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, "history.jsonl"), append(historyPayload, '\n'), 0o600); err != nil {
		t.Fatalf("write replay history: %v", err)
	}

	process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
		"--robot-replay="+fixture.session,
		"--id="+entry.ID,
		"--robot-format=json",
	)
	if process.exitCode != 0 {
		t.Fatalf("robot replay exit=%d, want 0; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
	}
	if len(bytes.TrimSpace(process.stderr)) != 0 {
		t.Fatalf("robot replay wrote stderr: %s", process.stderr)
	}

	type replaySendResult struct {
		Success    bool              `json:"success"`
		Timestamp  string            `json:"timestamp"`
		Session    string            `json:"session"`
		Targets    []string          `json:"targets"`
		Successful []string          `json:"successful"`
		Failed     []json.RawMessage `json:"failed"`
	}
	var envelope struct {
		Success         bool              `json:"success"`
		Timestamp       string            `json:"timestamp"`
		HistoryID       string            `json:"history_id"`
		OriginalCommand string            `json:"original_command"`
		Session         string            `json:"session"`
		TargetPanes     []string          `json:"target_panes"`
		Replayed        bool              `json:"replayed"`
		SendResult      *replaySendResult `json:"send_result"`
	}
	decodeSingleRobotJSON(t, process.stdout, &envelope)
	if !envelope.Success || !envelope.Replayed || envelope.HistoryID != entry.ID ||
		envelope.OriginalCommand != prompt || envelope.Session != fixture.session {
		t.Fatalf("robot replay envelope identity/state = %+v", envelope)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.Timestamp); err != nil {
		t.Fatalf("robot replay timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
	}
	if len(envelope.TargetPanes) != 1 || envelope.TargetPanes[0] != fixture.paneID {
		t.Fatalf("robot replay target_panes=%v, want [%s]", envelope.TargetPanes, fixture.paneID)
	}
	if envelope.SendResult == nil || !envelope.SendResult.Success || envelope.SendResult.Session != fixture.session {
		t.Fatalf("robot replay send_result = %+v", envelope.SendResult)
	}
	if _, err := time.Parse(time.RFC3339Nano, envelope.SendResult.Timestamp); err != nil {
		t.Fatalf("robot replay send timestamp %q is not RFC3339: %v", envelope.SendResult.Timestamp, err)
	}
	if len(envelope.SendResult.Targets) != 1 || envelope.SendResult.Targets[0] != "0" ||
		len(envelope.SendResult.Successful) != 1 || envelope.SendResult.Successful[0] != "0" ||
		envelope.SendResult.Failed == nil || len(envelope.SendResult.Failed) != 0 {
		t.Fatalf("robot replay send delivery arrays = targets:%v successful:%v failed:%v",
			envelope.SendResult.Targets, envelope.SendResult.Successful, envelope.SendResult.Failed)
	}

	fixture.waitForFileContents(t, markerPath, marker)
	// A duplicate send submitted by the replay wrapper would execute immediately.
	time.Sleep(250 * time.Millisecond)
	markers, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read replay marker file: %v", err)
	}
	if count := strings.Count(string(markers), marker); count != 1 {
		t.Fatalf("replay marker count=%d, want exactly 1; contents=%q", count, markers)
	}
}

func TestE2ERobotInterruptFollowUpRedactionBuiltBinaryRealTmux(t *testing.T) {
	CommonE2EPrerequisites(t)
	fixture := newRobotProcessFixture(t, "interrupt-redaction")

	configPath := filepath.Join(fixture.configDir, "ntm", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	writeRedactionMode := func(mode string) {
		t.Helper()
		contents := fmt.Sprintf("[redaction]\nmode = %q\n", mode)
		if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s redaction config: %v", mode, err)
		}
	}
	waitForPaneCommand := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			current := strings.TrimSpace(fixture.mustTMUXOutput(t, "display-message", "-p", "-t", fixture.paneID, "#{pane_current_command}"))
			if current == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane command=%q, want %q", current, want)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitForPaneOutput := func(want string) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			capture := fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", fixture.paneID, "-S", "-200")
			if strings.Contains(capture, want) {
				return capture
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %q in pane output: %s", want, capture)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	startInterruptibleCommand := func() {
		t.Helper()
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", "cat")
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
		waitForPaneCommand("cat")
	}

	type interruptRedaction struct {
		Mode     string `json:"mode"`
		Findings int    `json:"findings"`
		Action   string `json:"action"`
	}
	type interruptEnvelope struct {
		Success       bool                `json:"success"`
		Timestamp     string              `json:"timestamp"`
		Error         string              `json:"error"`
		ErrorCode     string              `json:"error_code"`
		Session       string              `json:"session"`
		Interrupted   []string            `json:"interrupted"`
		MessageSent   bool                `json:"message_sent"`
		Message       string              `json:"message"`
		Redaction     *interruptRedaction `json:"redaction"`
		ReadyForInput []string            `json:"ready_for_input"`
		Failed        []json.RawMessage   `json:"failed"`
	}
	secret := strings.TrimPrefix(fakePassword, "password=")
	followUp := "Continue using " + fakePassword
	runInterrupt := func(message string) robotBoundaryProcessResult {
		t.Helper()
		return runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
			"--robot-interrupt="+fixture.session,
			"--panes="+fixture.paneID,
			"--msg="+message,
			"--force",
			"--no-wait",
			"--robot-format=json",
		)
	}

	t.Run("block_prevents_interrupt_and_message", func(t *testing.T) {
		writeRedactionMode("block")
		startInterruptibleCommand()
		process := runInterrupt(followUp)
		if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("blocked interrupt exit=%d stderr=%q stdout=%s", process.exitCode, process.stderr, process.stdout)
		}
		if bytes.Contains(process.stdout, []byte(secret)) {
			t.Fatalf("blocked interrupt leaked secret in stdout: %s", process.stdout)
		}
		var envelope interruptEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if envelope.Success || envelope.ErrorCode != "SENSITIVE_DATA_BLOCKED" || envelope.Session != fixture.session ||
			envelope.MessageSent || envelope.Redaction == nil || envelope.Redaction.Action != "block" || envelope.Redaction.Findings == 0 {
			t.Fatalf("blocked interrupt envelope = %+v", envelope)
		}
		if envelope.Interrupted == nil || len(envelope.Interrupted) != 0 ||
			envelope.ReadyForInput == nil || len(envelope.ReadyForInput) != 0 ||
			envelope.Failed == nil || len(envelope.Failed) == 0 {
			t.Fatalf("blocked interrupt result arrays = interrupted:%v ready:%v failed:%v",
				envelope.Interrupted, envelope.ReadyForInput, envelope.Failed)
		}
		if _, err := time.Parse(time.RFC3339Nano, envelope.Timestamp); err != nil {
			t.Fatalf("blocked interrupt timestamp %q is not RFC3339: %v", envelope.Timestamp, err)
		}
		time.Sleep(250 * time.Millisecond)
		waitForPaneCommand("cat")
		capture := fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", fixture.paneID, "-S", "-200")
		if strings.Contains(capture, secret) || strings.Contains(capture, "[REDACTED:PASSWORD:") {
			t.Fatalf("blocked interrupt typed secret into pane: %s", capture)
		}

		// End the deliberately long-running setup command before the redact case.
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "C-c")
		resetPath := filepath.Join(fixture.root, "block-reset-ready")
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "-l", fmt.Sprintf("printf ready > %s", resetPath))
		fixture.mustTMUXOutput(t, "send-keys", "-t", fixture.paneID, "Enter")
		fixture.waitForFileContents(t, resetPath, "ready")
	})

	t.Run("redact_delivers_placeholder_without_secret", func(t *testing.T) {
		writeRedactionMode("redact")
		startInterruptibleCommand()
		process := runInterrupt(followUp)
		if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("redacted interrupt exit=%d stderr=%q stdout=%s", process.exitCode, process.stderr, process.stdout)
		}
		if bytes.Contains(process.stdout, []byte(secret)) {
			t.Fatalf("redacted interrupt leaked secret in stdout: %s", process.stdout)
		}
		var envelope interruptEnvelope
		decodeSingleRobotJSON(t, process.stdout, &envelope)
		if !envelope.Success || envelope.ErrorCode != "" || envelope.Session != fixture.session ||
			!envelope.MessageSent || envelope.Redaction == nil || envelope.Redaction.Action != "redact" || envelope.Redaction.Findings == 0 ||
			!strings.Contains(envelope.Message, "[REDACTED:PASSWORD:") || strings.Contains(envelope.Message, secret) {
			t.Fatalf("redacted interrupt envelope = %+v", envelope)
		}
		if len(envelope.Interrupted) != 1 || len(envelope.ReadyForInput) != 1 ||
			envelope.Failed == nil || len(envelope.Failed) != 0 {
			t.Fatalf("redacted interrupt result arrays = interrupted:%v ready:%v failed:%v",
				envelope.Interrupted, envelope.ReadyForInput, envelope.Failed)
		}
		capture := waitForPaneOutput("[REDACTED:PASSWORD:")
		if strings.Contains(capture, secret) || !strings.Contains(capture, "[REDACTED:PASSWORD:") {
			t.Fatalf("redacted interrupt pane output = %s", capture)
		}
	})
}

func supportsRobotFormat(t *testing.T) bool {
	t.Helper()
	cmd := exec.Command(mustE2EBin(), "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "--robot-format")
}
