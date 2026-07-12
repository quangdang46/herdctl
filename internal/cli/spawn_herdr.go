package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// herdrLaunchAgent starts one agent pane through `herdr agent start` instead of
// split+send-keys. Prefer this on NTM_BACKEND=herdr because long shell payloads
// (hooks/env) are fragile over pane send-text.
func herdrLaunchAgent(session, cwd string, agent FlatAgent, argv []string, title string) (tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.Pane{}, fmt.Errorf("herdrLaunchAgent called without herdr backend")
	}
	if len(argv) == 0 {
		return tmux.Pane{}, fmt.Errorf("herdrLaunchAgent: empty argv")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p, err := herdr.StartAgent(ctx, herdr.StartAgentOptions{
		Session:   session,
		Name:      fmt.Sprintf("%s-%s_%d", session, agent.Type, agent.Index),
		AgentType: herdr.AgentType(agent.Type),
		Index:     agent.Index,
		Variant:   agent.Model,
		Cwd:       cwd,
		Argv:      argv,
		Focus:     false,
		Split:     "right",
	})
	if err != nil {
		return tmux.Pane{}, err
	}
	if title != "" {
		_ = herdr.SetPaneTitle(p.ID, title)
	}
	return tmux.Pane{
		ID:       p.ID,
		Index:    p.Index,
		NTMIndex: p.NTMIndex,
		Title:    firstNonEmptyTitle(p.Title, title),
		Type:     tmux.AgentType(p.Type),
		Variant:  p.Variant,
		Command:  p.Command,
		Active:   p.Active,
	}, nil
}

func firstNonEmptyTitle(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// splitAgentArgv best-effort splits a shell command into argv for agent.start.
// For complex templates (env assignments, pipes) returns nil so caller can fall
// back to send-keys.
func splitAgentArgv(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	// Reject shell features that agent.start cannot express as argv.
	for _, bad := range []string{"&&", "||", "|", ";", "`", "$(", "\n", "\r"} {
		if strings.Contains(cmd, bad) {
			return nil
		}
	}
	// Reject leading env assignments: FOO=bar cmd
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil
	}
	if strings.Contains(fields[0], "=") {
		return nil
	}
	return fields
}

// herdrPreferredAgentArgv builds a simple argv for common agent types so we can
// use agent.start even when the config template injects hooks/env for tmux.
func herdrPreferredAgentArgv(agentType AgentType, model string) []string {
	model = strings.TrimSpace(model)
	switch agentType {
	case AgentTypeClaude:
		argv := []string{"claude", "--dangerously-skip-permissions"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeCodex:
		argv := []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}
		if model != "" {
			argv = append(argv, "-m", model)
		}
		return argv
	case AgentTypeGemini:
		argv := []string{"gemini", "--yolo"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeOpencode:
		argv := []string{"opencode"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeAntigravity:
		// Mirrors DefaultAgentTemplates Antigravity: agy --model X --dangerously-skip-permissions
		argv := []string{"agy"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		argv = append(argv, "--dangerously-skip-permissions")
		return argv
	case AgentTypeCursor:
		argv := []string{"cursor"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeWindsurf:
		argv := []string{"windsurf"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeAider:
		argv := []string{"aider"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeOllama:
		// ollama run requires a model positional; match template default when unset.
		if model == "" {
			model = "codellama:latest"
		}
		return []string{"ollama", "run", model}
	default:
		return nil
	}
}

// maybeNoteHerdrBackend prints a one-line notice in human mode.
func maybeNoteHerdrBackend() {
	if backend.IsHerdr() && !IsJSONOutput() {
		output.PrintInfo(fmt.Sprintf("backend=herdr (NTM_BACKEND=%s)", backend.Current()))
	}
}

