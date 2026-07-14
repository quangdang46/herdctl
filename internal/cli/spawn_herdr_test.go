package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestHerdrPreferredAgentArgv_ClaudeSystemPromptAndEffort(t *testing.T) {
	got := herdrPreferredAgentArgv(AgentTypeClaude, "sonnet", "/tmp/sys.txt", "high")
	want := []string{
		"claude", "--dangerously-skip-permissions",
		"--model", "sonnet",
		"--effort", "high",
		"--system-prompt-file", "/tmp/sys.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestHerdrPreferredAgentArgv_CodexWithSystemPromptFallsBack(t *testing.T) {
	// Codex system prompts require env wrapper; preferred argv must return nil
	// so resolveHerdrAgentArgv can keep the rendered template via sh -c.
	if got := herdrPreferredAgentArgv(AgentTypeCodex, "gpt-5", "/tmp/sys.txt", "high"); got != nil {
		t.Fatalf("expected nil preferred argv for codex+system-prompt, got %#v", got)
	}
}

func TestHerdrPreferredAgentArgv_CodexWithoutSystemPrompt(t *testing.T) {
	got := herdrPreferredAgentArgv(AgentTypeCodex, "gpt-5", "", "high")
	want := []string{
		"codex", "--dangerously-bypass-approvals-and-sandbox",
		"-m", "gpt-5",
		"-c", "model_reasoning_effort=high",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestResolveHerdrAgentArgv_PreservesCodexSystemPromptViaShell(t *testing.T) {
	// Simulated rendered template (env wrapper + codex). Preferred argv is nil,
	// splitAgentArgv rejects env assignments, so we fall through to sh -c.
	cmd := `CODEX_SYSTEM_PROMPT="$(cat /tmp/sys.txt)" codex --dangerously-bypass-approvals-and-sandbox -m gpt-5`
	got := resolveHerdrAgentArgv(AgentTypeCodex, "gpt-5", "/tmp/sys.txt", "high", cmd)
	if len(got) != 3 || got[0] != "sh" || got[1] != "-c" {
		t.Fatalf("expected sh -c fallback, got %#v", got)
	}
	if !strings.Contains(got[2], "CODEX_SYSTEM_PROMPT") || !strings.Contains(got[2], "/tmp/sys.txt") {
		t.Fatalf("shell command lost system prompt: %q", got[2])
	}
}

func TestResolveHerdrAgentArgv_ClaudePrefersCleanArgv(t *testing.T) {
	cmd := `claude --dangerously-skip-permissions --model sonnet --system-prompt-file /tmp/sys.txt`
	got := resolveHerdrAgentArgv(AgentTypeClaude, "sonnet", "/tmp/sys.txt", "", cmd)
	want := []string{
		"claude", "--dangerously-skip-permissions",
		"--model", "sonnet",
		"--system-prompt-file", "/tmp/sys.txt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestHerdrShellArgv(t *testing.T) {
	if got := herdrShellArgv(""); got != nil {
		t.Fatalf("empty → nil, got %#v", got)
	}
	got := herdrShellArgv("echo hi")
	if !reflect.DeepEqual(got, []string{"sh", "-c", "echo hi"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestHerdrPreferredAgentArgv_GeminiModelOrder(t *testing.T) {
	got := herdrPreferredAgentArgv(AgentTypeGemini, "gemini-2.5-pro", "", "")
	want := []string{"gemini", "--model", "gemini-2.5-pro", "--yolo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestSessionNameFromPanes(t *testing.T) {
	if got := sessionNameFromPanes(nil); got != "" {
		t.Fatalf("nil panes → empty, got %q", got)
	}
	panes := []tmux.Pane{
		{Title: "myproj__cc_1"},
		{Title: "myproj__cod_1"},
	}
	if got := sessionNameFromPanes(panes); got != "myproj" {
		t.Fatalf("got %q, want myproj", got)
	}
	if got := sessionNameFromPanes([]tmux.Pane{{Title: "no-separator"}}); got != "" {
		t.Fatalf("no separator → empty, got %q", got)
	}
}

func TestCollectReadyAgentPanes_SkipsUserAndUnknown(t *testing.T) {
	panes := []tmux.Pane{
		{ID: "p0", Title: "s__user_0", Type: tmux.AgentUser},
		{ID: "p1", Title: "s__cc_1", Type: tmux.AgentClaude},
	}
	// Capture always reports idle-looking content for claude.
	capture := func(id string) (string, error) {
		if id == "p1" {
			return "❯ ", nil
		}
		return "", nil
	}
	ready, count := collectReadyAgentPanes(panes, capture)
	if count != 1 {
		t.Fatalf("agentCount=%d, want 1", count)
	}
	// Without herdr backend, readiness depends on determineAgentState.
	// Just ensure user pane is never counted.
	for _, r := range ready {
		if r.ID == "p0" {
			t.Fatalf("user pane should not be ready")
		}
	}
}

func TestMuxAppendTags(t *testing.T) {
	base := muxFormatPaneName("proj", "cc", 1, "opus")
	if got := muxAppendTags(base, nil); got != base {
		t.Fatalf("nil tags: got %q want %q", got, base)
	}
	if got := muxAppendTags(base, []string{}); got != base {
		t.Fatalf("empty tags: got %q want %q", got, base)
	}
	got := muxAppendTags(base, []string{"frontend", "ui"})
	want := base + "[frontend,ui]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSendTagFilterUsesPaneTags(t *testing.T) {
	// Mirrors herdr muxGetPanes → tmux.Pane.Tags merge path used by send --tag.
	panes := []tmux.Pane{
		{ID: "w1:p1", Type: tmux.AgentClaude, Title: "proj__cc_1[frontend]", Tags: []string{"frontend"}},
		{ID: "w1:p2", Type: tmux.AgentCodex, Title: "proj__cod_1[backend]", Tags: []string{"backend"}},
		{ID: "w1:p3", Type: tmux.AgentGemini, Title: "proj__gmi_1", Tags: nil},
	}
	opts := SendOptions{Tags: []string{"frontend"}}
	filtered := filterPanesForBatch(panes, opts)
	if len(filtered) != 1 || filtered[0].ID != "w1:p1" {
		t.Fatalf("frontend filter: got %#v", filtered)
	}
	opts = SendOptions{Tags: []string{"backend", "frontend"}}
	filtered = filterPanesForBatch(panes, opts)
	if len(filtered) != 2 {
		t.Fatalf("OR tags: got %d panes, want 2", len(filtered))
	}
	opts = SendOptions{Tags: []string{"missing"}}
	filtered = filterPanesForBatch(panes, opts)
	if len(filtered) != 0 {
		t.Fatalf("no match: got %#v", filtered)
	}
}
