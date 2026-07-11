package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/serve"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

func TestLabelIntegration(t *testing.T) {
	testutil.RequireTmuxThrottled(t)
	root := repoRoot(t)
	projectsBase := t.TempDir()
	cfgPath := writeLabelTestConfig(t, projectsBase)
	run := func(args ...string) (stdout string, stderr string, exitCode int) {
		all := append([]string{"--config", cfgPath}, args...)
		return runNTM(t, root, all...)
	}

	t.Run("spawn_label_names_session_and_uses_base_project_dir", func(t *testing.T) {
		base := fmt.Sprintf("labelspawn-%d", time.Now().UnixNano())
		cfg := config.Default()
		cfg.ProjectsBase = projectsBase
		cfg.AgentMail.Enabled = false

		out, err := robot.GetSpawn(robot.SpawnOptions{
			Session: base,
			Label:   "frontend",
			CCCount: 1,
			DryRun:  true,
		}, cfg)
		if err != nil {
			t.Fatalf("GetSpawn error: %v", err)
		}
		if out == nil {
			t.Fatal("GetSpawn returned nil output")
		}
		if !out.Success {
			t.Fatalf("GetSpawn success=false: %s", out.Error)
		}
		if out.Session != base+"--frontend" {
			t.Fatalf("session = %q, want %q", out.Session, base+"--frontend")
		}
		wantDir := filepath.Join(cfg.ProjectsBase, base)
		if filepath.Clean(out.WorkingDir) != filepath.Clean(wantDir) {
			t.Fatalf("working_dir = %q, want %q", out.WorkingDir, wantDir)
		}
	})

	t.Run("multiple_labels_share_same_project_directory", func(t *testing.T) {
		cfg := config.Default()
		cfg.ProjectsBase = projectsBase
		base := fmt.Sprintf("labeldirs-%d", time.Now().UnixNano())
		frontendDir := cfg.GetProjectDir(base + "--frontend")
		backendDir := cfg.GetProjectDir(base + "--backend")
		baseDir := cfg.GetProjectDir(base)
		if frontendDir != backendDir || frontendDir != baseDir {
			t.Fatalf("expected all dirs equal, got frontend=%q backend=%q base=%q", frontendDir, backendDir, baseDir)
		}
	})

	t.Run("create_label_uses_labeled_session_name", func(t *testing.T) {
		base := fmt.Sprintf("labelcreate-%d", time.Now().UnixNano())
		stdout, stderr, code := run("--json", "create", base, "--label", "staging", "--panes=1")
		if code != 0 {
			t.Fatalf("create command failed (code=%d): %s", code, stderr)
		}

		var resp struct {
			Session          string `json:"session"`
			WorkingDirectory string `json:"working_directory"`
		}
		if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
			t.Fatalf("parse create JSON: %v\noutput=%s", err, stdout)
		}
		t.Cleanup(func() { _ = tmux.KillSession(resp.Session) })

		if resp.Session != base+"--staging" {
			t.Fatalf("session = %q, want %q", resp.Session, base+"--staging")
		}
		wantDir := filepath.Join(projectsBase, base)
		if filepath.Clean(resp.WorkingDirectory) != filepath.Clean(wantDir) {
			t.Fatalf("working_directory = %q, want %q", resp.WorkingDirectory, wantDir)
		}
	})

	t.Run("quick_label_uses_labeled_session_name", func(t *testing.T) {
		base := fmt.Sprintf("labelquick-%d", time.Now().UnixNano())
		stdout, stderr, code := run("--json", "quick", base, "--label", "dev", "--no-git", "--no-vscode", "--no-claude")
		if code != 0 {
			t.Fatalf("quick command failed (code=%d): %s", code, stderr)
		}

		var resp struct {
			Session          string `json:"session"`
			WorkingDirectory string `json:"working_directory"`
		}
		if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
			t.Fatalf("parse quick JSON: %v\noutput=%s", err, stdout)
		}

		if resp.Session != base+"--dev" {
			t.Fatalf("session = %q, want %q", resp.Session, base+"--dev")
		}
		wantDir := filepath.Join(projectsBase, base)
		if filepath.Clean(resp.WorkingDirectory) != filepath.Clean(wantDir) {
			t.Fatalf("working_directory = %q, want %q", resp.WorkingDirectory, wantDir)
		}
	})

	t.Run("add_label_targets_labeled_session", func(t *testing.T) {
		base := fmt.Sprintf("labeladd-%d", time.Now().UnixNano())
		_, stderr, code := run("add", base, "--label", "frontend", "--cc=1")
		if code == 0 {
			t.Fatalf("expected add to fail for missing session, got code=0")
		}
		expectedSession := fmt.Sprintf("%s--frontend", base)
		if !strings.Contains(stderr, expectedSession) || !strings.Contains(stderr, "not found") {
			t.Fatalf("stderr = %q, want session %q and 'not found'", stderr, expectedSession)
		}
	})

	t.Run("validation_rejects_invalid_labels", func(t *testing.T) {
		tests := []struct {
			label       string
			errContains string
		}{
			{label: "bad!", errContains: "alphanumeric"},
			{label: "my--label", errContains: "separator"},
			{label: strings.Repeat("a", 51), errContains: "50 characters"},
		}
		for _, tt := range tests {
			t.Run(tt.label, func(t *testing.T) {
				base := fmt.Sprintf("proj-%d", time.Now().UnixNano())
				_, stderr, code := run("quick", base, "--label", tt.label, "--no-git", "--no-vscode", "--no-claude")
				if code == 0 {
					t.Fatalf("expected error for label %q, got exit code 0", tt.label)
				}
				if !strings.Contains(stderr, tt.errContains) {
					t.Fatalf("stderr = %q, want containing %q", stderr, tt.errContains)
				}
			})
		}
	})

	t.Run("project_name_validation_rejects_reserved_separator_unconditionally", func(t *testing.T) {
		tests := [][]string{
			{"spawn", "my--project", "--cc=1"},
			{"spawn", "my--project", "--label", "x", "--cc=1"},
			{"create", "my--project"},
			{"quick", "my--project", "--no-git", "--no-vscode", "--no-claude"},
		}
		for _, args := range tests {
			name := strings.Join(args, "_")
			t.Run(name, func(t *testing.T) {
				_, stderr, code := run(args...)
				if code == 0 {
					t.Fatalf("expected error, got exit code 0")
				}
				if !strings.Contains(stderr, "contains '--'") {
					t.Fatalf("stderr = %q, want project-name separator validation", stderr)
				}
			})
		}
	})

	t.Run("robot_spawn_label_dry_run_returns_labeled_session", func(t *testing.T) {
		base := fmt.Sprintf("labelrobot-%d", time.Now().UnixNano())
		stdout, stderr, code := run(
			"--robot-spawn="+base,
			"--spawn-label=frontend",
			"--spawn-cc=1",
			"--dry-run",
		)
		if code != 0 {
			t.Fatalf("robot spawn failed (code=%d): %s", code, stderr)
		}

		var resp robot.SpawnOutput
		if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
			t.Fatalf("parse robot spawn JSON: %v\noutput=%s", err, stdout)
		}
		if !resp.Success {
			t.Fatalf("robot spawn success=false: %s", resp.Error)
		}
		if resp.Session != base+"--frontend" {
			t.Fatalf("session = %q, want %q", resp.Session, base+"--frontend")
		}
	})

	t.Run("rest_agent_spawn_request_label_passthrough", func(t *testing.T) {
		body := `{"cc_count":1,"label":"frontend"}`
		var req serve.AgentSpawnRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("unmarshal AgentSpawnRequest: %v", err)
		}
		if req.Label != "frontend" {
			t.Fatalf("label = %q, want %q", req.Label, "frontend")
		}

		base := fmt.Sprintf("labelrest-%d", time.Now().UnixNano())
		cfg := config.Default()
		cfg.ProjectsBase = projectsBase
		cfg.AgentMail.Enabled = false

		out, err := robot.GetSpawn(robot.SpawnOptions{
			Session: base,
			Label:   req.Label,
			CCCount: req.CCCount,
			DryRun:  true,
		}, cfg)
		if err != nil {
			t.Fatalf("GetSpawn error: %v", err)
		}
		if out == nil || !out.Success {
			t.Fatalf("expected success, got %+v", out)
		}
		if out.Session != base+"--frontend" {
			t.Fatalf("session = %q, want %q", out.Session, base+"--frontend")
		}
	})
}

func runNTM(t *testing.T, root string, args ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping go-run process integration in short mode")
	}
	cmd := exec.Command("go", append([]string{"run", "./cmd/ntm"}, args...)...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	out, errOut, err := cmdOutput(cmd)
	if err == nil {
		return out, errOut, 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, errOut, exitErr.ExitCode()
	}
	t.Fatalf("runNTM failed to start: %v", err)
	return "", "", 1
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root (go.mod)")
		}
		dir = parent
	}
}

func writeLabelTestConfig(t *testing.T, projectsBase string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "label-test.toml")
	content := fmt.Sprintf(`projects_base = %q

[agents]
claude = "cat"
codex = "cat"
gemini = "cat"

[agent_mail]
enabled = false
`, projectsBase)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func cmdOutput(cmd *exec.Cmd) (stdout string, stderr string, err error) {
	var outBuf strings.Builder
	var errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}
