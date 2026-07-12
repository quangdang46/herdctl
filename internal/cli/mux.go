package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// mux* helpers dispatch core session/pane operations to tmux (default) or
// herdr (NTM_BACKEND=herdr). They convert herdr shapes into tmux shapes so the
// rest of the CLI (typed on tmux.Pane / tmux.Session) keeps compiling without
// mass refactors.
//
// Rule: never call these from code paths that must stay tmux-only until parity
// is proven. Prefer opt-in at the top of spawn/send/list.

func muxEnsureInstalled() error {
	if backend.IsHerdr() {
		return herdr.EnsureInstalled()
	}
	return tmux.EnsureInstalled()
}

func muxValidateSessionName(name string) error {
	if backend.IsHerdr() {
		return herdr.ValidateSessionName(name)
	}
	return tmux.ValidateSessionName(name)
}

func muxSessionExists(name string) bool {
	if backend.IsHerdr() {
		return herdr.SessionExists(name)
	}
	return tmux.SessionExists(name)
}

func muxListSessions() ([]tmux.Session, error) {
	if !backend.IsHerdr() {
		return tmux.ListSessions()
	}
	sessions, err := herdr.ListSessions()
	if err != nil {
		return nil, err
	}
	out := make([]tmux.Session, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, tmux.Session{
			Name:      s.Name,
			Directory: s.Directory,
			Windows:   s.Windows,
			Attached:  s.Attached,
			Created:   s.Created,
		})
	}
	return out, nil
}

func muxGetPanes(session string) ([]tmux.Pane, error) {
	return muxGetPanesContext(context.Background(), session)
}

func muxGetPanesContext(ctx context.Context, session string) ([]tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.GetPanesContext(ctx, session)
	}
	panes, err := herdr.GetPanesContext(ctx, session)
	if err != nil {
		return nil, err
	}
	return herdrPanesToTmux(panes), nil
}

func muxGetAllPanes() (map[string][]tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.GetAllPanes()
	}
	sessions, err := herdr.ListSessions()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]tmux.Pane, len(sessions))
	for _, s := range sessions {
		panes, err := herdr.GetPanes(s.Name)
		if err != nil {
			return nil, err
		}
		out[s.Name] = herdrPanesToTmux(panes)
	}
	return out, nil
}

func muxCreateSessionWithHistoryLimit(name, directory string, historyLimit int) error {
	if backend.IsHerdr() {
		return herdr.CreateSessionWithHistoryLimit(name, directory, historyLimit)
	}
	return tmux.CreateSessionWithHistoryLimit(name, directory, historyLimit)
}

func muxSplitWindow(session, directory string) (string, error) {
	if backend.IsHerdr() {
		return herdr.SplitWindow(session, directory)
	}
	return tmux.SplitWindow(session, directory)
}

func muxSetPaneTitle(paneID, title string) error {
	if backend.IsHerdr() {
		return herdr.SetPaneTitle(paneID, title)
	}
	return tmux.SetPaneTitle(paneID, title)
}

func muxSendKeys(target, keys string, enter bool) error {
	if backend.IsHerdr() {
		return herdr.SendKeys(target, keys, enter)
	}
	return tmux.SendKeys(target, keys, enter)
}

func muxSendKeysWithDelay(target, keys string, enter bool, enterDelay time.Duration) error {
	if backend.IsHerdr() {
		return herdr.SendKeysWithDelay(target, keys, enter, enterDelay)
	}
	return tmux.SendKeysWithDelay(target, keys, enter, enterDelay)
}

func muxSendKeysForAgent(target, keys string, enter bool, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
		return herdr.SendKeysForAgent(target, keys, enter, herdr.AgentType(agentType))
	}
	return tmux.SendKeysForAgent(target, keys, enter, agentType)
}

func muxSendInterrupt(target string) error {
	if backend.IsHerdr() {
		return herdr.SendInterrupt(target)
	}
	return tmux.SendInterrupt(target)
}

func muxCapturePaneOutput(target string, lines int) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutput(target, lines)
	}
	return tmux.CapturePaneOutput(target, lines)
}

func muxKillSession(session string) error {
	if backend.IsHerdr() {
		return herdr.KillSession(session)
	}
	return tmux.KillSession(session)
}

func muxKillPane(paneID string) error {
	if backend.IsHerdr() {
		return herdr.KillPane(paneID)
	}
	return tmux.KillPane(paneID)
}

func muxZoomPane(session string, paneIndex int) error {
	if backend.IsHerdr() {
		return herdr.ZoomPane(session, paneIndex)
	}
	return tmux.ZoomPane(session, paneIndex)
}

func muxFormatPaneName(session, agentType string, index int, variant string) string {
	// Pure string helper — identical implementation, no backend needed.
	// Prefer tmux package to avoid drift for the default path.
	if backend.IsHerdr() {
		return herdr.FormatPaneName(session, agentType, index, variant)
	}
	return tmux.FormatPaneName(session, agentType, index, variant)
}

func muxBackendLabel() string {
	return backend.Current().String()
}

// herdrPaneNumericWinPane parses a Herdr pane ID ("w6:p2") into numeric
// window/pane indices so tmux selector grammar (W.P / %N / N) accepts them.
func herdrPaneNumericWinPane(herdrID string) (win, pane int) {
	if strings.Count(herdrID, ":") != 1 {
		return 0, 0
	}
	left, right, ok := strings.Cut(herdrID, ":")
	if !ok {
		return 0, 0
	}
	// "w6" → 6
	win, _ = strconv.Atoi(strings.TrimLeft(left, "wW"))
	// "p2" → 2
	pane, _ = strconv.Atoi(strings.TrimLeft(right, "pP"))
	return win, pane
}

func herdrPanesToTmux(in []herdr.Pane) []tmux.Pane {
	out := make([]tmux.Pane, 0, len(in))
	for i, p := range in {
		win, paneIdx := herdrPaneNumericWinPane(p.ID)
		if win == 0 && paneIdx == 0 {
			win, paneIdx = p.WindowIndex, i
		}
		out = append(out, tmux.Pane{
			ID:          p.ID,
			Index:       paneIdx,
			WindowIndex: win,
			NTMIndex:    p.NTMIndex,
			Title:       p.Title,
			Type:        tmux.AgentType(p.Type),
			Variant:     p.Variant,
			Tags:        append([]string{}, p.Tags...),
			Command:     p.Command,
			Width:       p.Width,
			Height:      p.Height,
			Active:      p.Active,
			PID:         p.PID,
		})
	}
	return out
}

// muxRequireHerdrServer pings herdr when selected so failures are early/clear.
func muxRequireHerdrServer() error {
	if !backend.IsHerdr() {
		return nil
	}
	if err := herdr.EnsureInstalled(); err != nil {
		return err
	}
	if err := herdr.Ping(); err != nil {
		return fmt.Errorf("herdr backend selected but server is not reachable (start `herdr` first): %w", err)
	}
	return nil
}

func muxCaptureForStatusDetection(target string) (string, error) {
	if backend.IsHerdr() {
		// Herdr has no separate status-capture budget; recent lines are enough.
		return herdr.CapturePaneOutput(target, 30)
	}
	return tmux.CaptureForStatusDetection(target)
}

func muxCapturePaneVisible(target string) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneVisible(target)
	}
	return tmux.CapturePaneVisible(target)
}

func muxSendKeysForAgentDoubleEnter(target, keys string, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
		// Approximate tmux double-enter submit for agent TUIs.
		if err := herdr.SendKeysForAgent(target, keys, false, herdr.AgentType(agentType)); err != nil {
			return err
		}
		time.Sleep(tmux.DoubleEnterFirstDelay)
		if err := herdr.SendKeys(target, "", true); err != nil {
			return err
		}
		time.Sleep(tmux.DoubleEnterSecondDelay)
		return herdr.SendKeys(target, "", true)
	}
	return tmux.SendKeysForAgentDoubleEnter(target, keys, agentType)
}

func muxPasteKeys(target, content string, enter bool) error {
	if backend.IsHerdr() {
		return herdr.SendKeys(target, content, enter)
	}
	return tmux.PasteKeys(target, content, enter)
}

// muxCapturePaneOutputContext is the context-aware capture helper.
func muxCapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutputContext(ctx, target, lines)
	}
	return tmux.CapturePaneOutputContext(ctx, target, lines)
}

// muxGetAgentStatus returns the backend-reported agent status when available.
// On tmux there is no native agent-status field — returns "" so callers fall
// back to scrollback classification.
func muxGetAgentStatus(target string) (string, error) {
	if backend.IsHerdr() {
		return herdr.GetAgentStatus(target)
	}
	return "", nil
}

// muxWaitAgentStatus blocks until the pane reports the desired agent status.
// Herdr uses `herdr agent wait`; tmux has no native equivalent and returns
// ErrNotSupported so callers can poll via capture/classification instead.
func muxWaitAgentStatus(target, status string, timeoutMS int) error {
	if backend.IsHerdr() {
		return herdr.WaitAgentStatus(target, status, timeoutMS)
	}
	return fmt.Errorf("tmux backend: native agent-status wait not supported (use poll+capture)")
}

// muxWaitAgentStatusContext is the context-aware variant.
func muxWaitAgentStatusContext(ctx context.Context, target, status string, timeoutMS int) error {
	if backend.IsHerdr() {
		return herdr.WaitAgentStatusContext(ctx, target, status, timeoutMS)
	}
	return fmt.Errorf("tmux backend: native agent-status wait not supported (use poll+capture)")
}

// herdrStatusToWaitCondition maps herdr agent_status values onto ntm wait
// conditions so the herdr-native wait path can short-circuit when possible.
func herdrStatusMatchesCondition(agentStatus string, condition WaitCondition) bool {
	switch strings.ToLower(strings.TrimSpace(agentStatus)) {
	case herdr.AgentStatusIdle, herdr.AgentStatusDone:
		return condition == ConditionIdle || condition == ConditionComplete || condition == ConditionHealthy
	case herdr.AgentStatusWorking:
		return condition == ConditionGenerating || condition == ConditionHealthy
	case herdr.AgentStatusBlocked:
		// blocked is not healthy and not idle/generating
		return false
	case herdr.AgentStatusUnknown:
		return false
	default:
		return false
	}
}

// mapWaitConditionToHerdrStatus picks the herdr --status value that best
// matches a single ntm wait condition. Empty means "no native mapping".
func mapWaitConditionToHerdrStatus(condition WaitCondition) string {
	switch condition {
	case ConditionIdle, ConditionComplete:
		return herdr.AgentStatusIdle
	case ConditionGenerating:
		return herdr.AgentStatusWorking
	default:
		// healthy / composed conditions need multi-state awareness
		return ""
	}
}


// muxInTmux reports whether the process is nested inside a live terminal
// multiplexer session that can host the assign-watch overlay.
// Herdr has no tmux-style overlay binding surface, so this is always false
// under NTM_BACKEND=herdr.
func muxInTmux() bool {
	if backend.IsHerdr() {
		return false
	}
	return tmux.InTmux()
}

// muxGetCurrentSession returns the currently-focused backend session name,
// or "" when none can be resolved.
func muxGetCurrentSession() string {
	if backend.IsHerdr() {
		return herdr.GetCurrentSession()
	}
	return tmux.GetCurrentSession()
}

// muxSendKeysForAgentWithDelay routes agent-aware key delivery through the
// active backend. Herdr ignores agent-type paste heuristics and uses plain
// send-keys-with-delay; tmux keeps its agent-specific path.
func muxSendKeysForAgentWithDelay(target, keys string, enter bool, enterDelay time.Duration, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
		return herdr.SendKeysForAgentWithDelay(target, keys, enter, enterDelay, herdr.AgentType(agentType))
	}
	return tmux.SendKeysForAgentWithDelay(target, keys, enter, enterDelay, agentType)
}

