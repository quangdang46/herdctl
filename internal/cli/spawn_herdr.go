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
// back to send-keys or sh -c.
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

// herdrShellArgv wraps a full shell command so agent.start can still launch
// templates that need env assignments / pipes (e.g. Codex system prompts).
func herdrShellArgv(cmd string) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	return []string{"sh", "-c", cmd}
}

// herdrPreferredAgentArgv builds a simple argv for common agent types so we can
// use agent.start even when the config template injects hooks/env for tmux.
//
// systemPromptFile / reasoningEffort come from persona PrepareSystemPrompt and
// must not be dropped when the agent CLI can express them as argv. When they
// cannot (Codex env wrapper, Gemini, etc.), return nil so the caller can fall
// back to splitAgentArgv / herdrShellArgv and keep persona prompts intact.
func herdrPreferredAgentArgv(agentType AgentType, model, systemPromptFile, reasoningEffort string) []string {
	model = strings.TrimSpace(model)
	systemPromptFile = strings.TrimSpace(systemPromptFile)
	reasoningEffort = strings.TrimSpace(reasoningEffort)

	switch agentType {
	case AgentTypeClaude:
		argv := []string{"claude", "--dangerously-skip-permissions"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		if reasoningEffort != "" {
			argv = append(argv, "--effort", reasoningEffort)
		}
		if systemPromptFile != "" {
			argv = append(argv, "--system-prompt-file", systemPromptFile)
		}
		return argv
	case AgentTypeCodex:
		// Codex system prompts are injected via CODEX_SYSTEM_PROMPT= env in the
		// tmux template — not expressible as a clean argv for agent.start.
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}
		if model != "" {
			argv = append(argv, "-m", model)
		}
		if reasoningEffort != "" {
			argv = append(argv, "-c", "model_reasoning_effort="+reasoningEffort)
		}
		return argv
	case AgentTypeGemini:
		// Gemini template has no system-prompt / effort CLI flags.
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"gemini", "--yolo"}
		if model != "" {
			// Prefer --model before --yolo to match DefaultAgentTemplates order.
			argv = []string{"gemini", "--model", model, "--yolo"}
		}
		return argv
	case AgentTypeOpencode:
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"opencode"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeAntigravity:
		// Mirrors DefaultAgentTemplates Antigravity: agy --model X --dangerously-skip-permissions
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"agy"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		argv = append(argv, "--dangerously-skip-permissions")
		return argv
	case AgentTypeCursor:
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"cursor"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeWindsurf:
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"windsurf"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeAider:
		if systemPromptFile != "" {
			return nil
		}
		argv := []string{"aider"}
		if model != "" {
			argv = append(argv, "--model", model)
		}
		return argv
	case AgentTypeOllama:
		if systemPromptFile != "" {
			return nil
		}
		// ollama run requires a model positional; match template default when unset.
		if model == "" {
			model = "codellama:latest"
		}
		return []string{"ollama", "run", model}
	default:
		return nil
	}
}

// resolveHerdrAgentArgv picks the best argv for herdr agent.start without
// dropping persona system prompts:
//  1. clean preferred argv when the agent CLI can express all options
//  2. field-split of the rendered template when it is simple
//  3. sh -c of the full template so env/system-prompt wrappers still run
func resolveHerdrAgentArgv(agentType AgentType, model, systemPromptFile, reasoningEffort, safeAgentCmd string) []string {
	if argv := herdrPreferredAgentArgv(agentType, model, systemPromptFile, reasoningEffort); len(argv) > 0 {
		return argv
	}
	if argv := splitAgentArgv(safeAgentCmd); len(argv) > 0 {
		return argv
	}
	return herdrShellArgv(safeAgentCmd)
}

// maybeNoteHerdrBackend prints a one-line notice in human mode.
func maybeNoteHerdrBackend() {
	if backend.IsHerdr() && !IsJSONOutput() {
		output.PrintInfo(fmt.Sprintf("backend=herdr (NTM_BACKEND=%s)", backend.Current()))
	}
}
