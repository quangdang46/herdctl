// Package robot provides machine-readable output for AI agents.
// backend_mux.go routes session/pane/capture/send primitives through the
// active NTM backend (tmux default, or herdr when NTM_BACKEND=herdr).
//
// These helpers mirror internal/cli/mux* so robot surfaces stay dual-backend
// without importing the cli package (import cycle). Prefer these over raw
// tmux.* calls in robot handlers that must work under herdr.
//
// Bead: bd-gl28u.4.3
package robot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	dispatchsvc "github.com/Dicklesworthstone/ntm/internal/dispatch"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// backendName returns the active backend label ("herdr" or "tmux").
func backendName() string {
	if backend.IsHerdr() {
		return "herdr"
	}
	return "tmux"
}

// backendIsInstalled reports whether the active backend binary is resolvable.
func backendIsInstalled() bool {
	if backend.IsHerdr() {
		return herdr.IsInstalled()
	}
	return tmux.IsInstalled()
}

// backendSessionExists reports whether the named session exists on the active backend.
func backendSessionExists(name string) bool {
	if backend.IsHerdr() {
		return herdr.SessionExists(name)
	}
	return tmux.SessionExists(name)
}

// backendListSessions lists sessions via the active backend, shaped as tmux.Session.
func backendListSessions() ([]tmux.Session, error) {
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

// backendGetPanes lists panes for a session via the active backend.
func backendGetPanes(session string) ([]tmux.Pane, error) {
	return backendGetPanesContext(context.Background(), session)
}

// backendGetPanesContext is the context-aware variant of backendGetPanes.
func backendGetPanesContext(ctx context.Context, session string) ([]tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.GetPanesContext(ctx, session)
	}
	panes, err := herdr.GetPanesContext(ctx, session)
	if err != nil {
		return nil, err
	}
	return backendHerdrPanesToTmux(panes), nil
}

// backendGetAllPanes returns session → panes for every live session.
func backendGetAllPanes() (map[string][]tmux.Pane, error) {
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
		out[s.Name] = backendHerdrPanesToTmux(panes)
	}
	return out, nil
}

// backendGetPanesWithActivityContext returns panes plus activity timestamps.
// Under herdr, activity times are zero (no native pane-activity clock); callers
// that need idle detection should prefer agent_status / capture classification.
func backendGetPanesWithActivityContext(ctx context.Context, session string) ([]tmux.PaneActivity, error) {
	if !backend.IsHerdr() {
		return tmux.GetPanesWithActivityContext(ctx, session)
	}
	panes, err := backendGetPanesContext(ctx, session)
	if err != nil {
		return nil, err
	}
	out := make([]tmux.PaneActivity, 0, len(panes))
	for _, p := range panes {
		out = append(out, tmux.PaneActivity{Pane: p})
	}
	return out, nil
}

// backendGetPaneActivity returns last activity for a pane. Under herdr this is a
// no-op (zero time, nil error): herdr has no window_activity equivalent, and a
// hard error would fail-closed interrupt readiness polls that already refresh
// via capture.
func backendGetPaneActivity(paneID string) (time.Time, error) {
	if backend.IsHerdr() {
		return time.Time{}, nil
	}
	return tmux.GetPaneActivity(paneID)
}

// backendPaneTarget prefers pane.ID (herdr wN:pM / tmux %N). Falls back to the
// tmux physical form session:win.pane only when the backend is tmux and ID is empty.
// Under herdr an empty ID is an error — session:win.pane is not a valid herdr target.
func backendPaneTarget(session string, pane tmux.Pane) (string, error) {
	if id := strings.TrimSpace(pane.ID); id != "" {
		return id, nil
	}
	if backend.IsHerdr() {
		return "", fmt.Errorf("pane %d in session %q has empty ID", pane.Index, session)
	}
	return fmt.Sprintf("%s:%d.%d", session, pane.WindowIndex, pane.Index), nil
}

// backendCapturePaneOutput captures recent pane text via the active backend.
func backendCapturePaneOutput(target string, lines int) (string, error) {
	return backendCapturePaneOutputContext(context.Background(), target, lines)
}

// backendCapturePaneOutputContext is the context-aware capture helper.
func backendCapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutputContext(ctx, target, lines)
	}
	return tmux.CapturePaneOutputContext(ctx, target, lines)
}

// backendCaptureForStatusDetection captures a short scrollback for state detection.
func backendCaptureForStatusDetection(target string) (string, error) {
	return backendCaptureForStatusDetectionContext(context.Background(), target)
}

// backendCaptureForStatusDetectionContext is the context-aware status capture.
func backendCaptureForStatusDetectionContext(ctx context.Context, target string) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutputContext(ctx, target, 30)
	}
	return tmux.CaptureForStatusDetectionContext(ctx, target)
}

// backendCaptureForFullContext captures a large scrollback budget for analysis.
func backendCaptureForFullContext(target string) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutput(target, tmux.LinesFullContext)
	}
	return tmux.CaptureForFullContext(target)
}

// backendSendKeys sends keys via the active backend.
func backendSendKeys(target, keys string, enter bool) error {
	if backend.IsHerdr() {
		return herdr.SendKeys(target, keys, enter)
	}
	return tmux.SendKeys(target, keys, enter)
}

// backendSendInterrupt sends Ctrl+C via the active backend.
func backendSendInterrupt(target string) error {
	if backend.IsHerdr() {
		return herdr.SendInterrupt(target)
	}
	return tmux.SendInterrupt(target)
}

// backendSendKeysForAgent sends keys with agent-aware enter defaults.
func backendSendKeysForAgent(target, keys string, enter bool, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
		return herdr.SendKeysForAgent(target, keys, enter, herdr.AgentType(agentType))
	}
	return tmux.SendKeysForAgent(target, keys, enter, agentType)
}

// backendSendKeysForAgentWithDelay sends keys with a configurable enter delay.
func backendSendKeysForAgentWithDelay(target, keys string, enter bool, enterDelay time.Duration, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
		return herdr.SendKeysForAgentWithDelay(target, keys, enter, enterDelay, herdr.AgentType(agentType))
	}
	return tmux.SendKeysForAgentWithDelay(target, keys, enter, enterDelay, agentType)
}

// backendSendKeysForAgentDoubleEnter approximates double-enter submit for agent TUIs.
func backendSendKeysForAgentDoubleEnter(target, keys string, agentType tmux.AgentType) error {
	if backend.IsHerdr() {
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

// backendDispatchDeliverer is a backend-aware dispatchsvc.Deliverer for robot
// send/ack/bulk-assign/spawn paths. Mirrors cli.muxDispatchDeliverer.
func backendDispatchDeliverer(session string) dispatchsvc.Deliverer {
	return dispatchsvc.DelivererFunc(func(_ context.Context, delivery dispatchsvc.Delivery) error {
		target := delivery.Target.Ref.ID
		if target == "" {
			if delivery.Target.Pane.ID != "" {
				target = delivery.Target.Pane.ID
			} else {
				sess := session
				if sess == "" {
					sess = delivery.Session
				}
				target = fmt.Sprintf("%s:%s", sess, delivery.Target.Ref.Physical())
			}
		}
		switch delivery.Protocol {
		case dispatchsvc.ProtocolStageOnly:
			return backendSendKeysForAgentWithDelay(target, delivery.Message, false, 0, delivery.Target.AgentType)
		case dispatchsvc.ProtocolSingleEnter:
			return backendSendKeysForAgentWithDelay(target, delivery.Message, true, delivery.EnterDelay, delivery.Target.AgentType)
		case dispatchsvc.ProtocolDoubleEnter:
			return backendSendKeysForAgentDoubleEnter(target, delivery.Message, delivery.Target.AgentType)
		default:
			return fmt.Errorf("unsupported delivery protocol %q", delivery.Protocol)
		}
	})
}

// backendHerdrPanesToTmux converts herdr panes into tmux.Pane shapes used by
// the rest of the robot stack (selectors, dispatch, observers).
func backendHerdrPanesToTmux(in []herdr.Pane) []tmux.Pane {
	out := make([]tmux.Pane, 0, len(in))
	for i, p := range in {
		win, paneIdx := backendHerdrPaneNumericWinPane(p.ID)
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

func backendHerdrPaneNumericWinPane(herdrID string) (win, pane int) {
	if strings.Count(herdrID, ":") != 1 {
		return 0, 0
	}
	left, right, ok := strings.Cut(herdrID, ":")
	if !ok {
		return 0, 0
	}
	win, _ = strconv.Atoi(strings.TrimLeft(left, "wW"))
	pane, _ = strconv.Atoi(strings.TrimLeft(right, "pP"))
	return win, pane
}

// backendValidateSessionName validates a session name on the active backend.
func backendValidateSessionName(name string) error {
	if backend.IsHerdr() {
		return herdr.ValidateSessionName(name)
	}
	return tmux.ValidateSessionName(name)
}

// backendCreateSessionWithHistoryLimit creates a session/workspace on the active backend.
// historyLimit is honored on tmux; herdr ignores it (no scrollback knob).
func backendCreateSessionWithHistoryLimit(name, directory string, historyLimit int) error {
	if backend.IsHerdr() {
		return herdr.CreateSessionWithHistoryLimit(name, directory, historyLimit)
	}
	return tmux.CreateSessionWithHistoryLimit(name, directory, historyLimit)
}

// backendSplitWindow creates a new pane/split in the session.
func backendSplitWindow(session, directory string) (string, error) {
	if backend.IsHerdr() {
		return herdr.SplitWindow(session, directory)
	}
	return tmux.SplitWindow(session, directory)
}

// backendSetPaneTitle sets the pane title/label on the active backend.
func backendSetPaneTitle(paneID, title string) error {
	if backend.IsHerdr() {
		return herdr.SetPaneTitle(paneID, title)
	}
	return tmux.SetPaneTitle(paneID, title)
}

// backendApplyTiledLayout applies a tiled layout when supported.
// Herdr best-effort unzooms then may return ErrNotSupported.
func backendApplyTiledLayout(session string) error {
	if backend.IsHerdr() {
		return herdr.ApplyTiledLayout(session)
	}
	return tmux.ApplyTiledLayout(session)
}

// backendRequireReady fails closed when herdr is selected but unreachable.
func backendRequireReady() error {
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

// backendHerdrAgentStatuses returns pane_id → agent_status for a session when
// herdr is active. Returns nil on tmux (no native agent_status field).
func backendHerdrAgentStatuses(session string) map[string]string {
	if !backend.IsHerdr() {
		return nil
	}
	panes, err := herdr.GetPanes(session)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(panes))
	for _, p := range panes {
		if s := strings.TrimSpace(p.AgentStatus); s != "" {
			out[p.ID] = strings.ToLower(s)
		}
	}
	return out
}

// backendMapHerdrAgentState maps herdr agent_status onto robot state strings
// used by snapshot/assign (idle|working|blocked|unknown).
func backendMapHerdrAgentState(agentStatus string) string {
	switch strings.ToLower(strings.TrimSpace(agentStatus)) {
	case herdr.AgentStatusIdle, herdr.AgentStatusDone:
		return "idle"
	case herdr.AgentStatusWorking:
		return "working"
	case herdr.AgentStatusBlocked:
		return "blocked"
	case herdr.AgentStatusUnknown, "":
		return "unknown"
	default:
		return "unknown"
	}
}

// backendStartAgent launches one agent via herdr agent.start.
// Only valid when NTM_BACKEND=herdr.
func backendStartAgent(ctx context.Context, session, name string, agentType tmux.AgentType, index int, variant, cwd string, argv []string, title string) (tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.Pane{}, fmt.Errorf("backendStartAgent called without herdr backend")
	}
	if len(argv) == 0 {
		return tmux.Pane{}, fmt.Errorf("backendStartAgent: empty argv")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
	}
	p, err := herdr.StartAgent(ctx, herdr.StartAgentOptions{
		Session:   session,
		Name:      name,
		AgentType: herdr.AgentType(agentType),
		Index:     index,
		Variant:   variant,
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
	win, paneIdx := backendHerdrPaneNumericWinPane(p.ID)
	if win == 0 && paneIdx == 0 {
		win, paneIdx = p.WindowIndex, p.Index
	}
	return tmux.Pane{
		ID:          p.ID,
		Index:       paneIdx,
		WindowIndex: win,
		NTMIndex:    p.NTMIndex,
		Title:       firstNonEmptyString(p.Title, title),
		Type:        tmux.AgentType(p.Type),
		Variant:     p.Variant,
		Command:     p.Command,
		Active:      p.Active,
		PID:         p.PID,
	}, nil
}

// backendPreferredAgentArgv builds a simple argv for common agent types so
// robot spawn can use herdr agent.start without shell templates.
func backendPreferredAgentArgv(agentType string) []string {
	switch tmux.AgentType(agentType).Canonical() {
	case tmux.AgentClaude:
		return []string{"claude", "--dangerously-skip-permissions"}
	case tmux.AgentCodex:
		return []string{"codex", "--dangerously-bypass-approvals-and-sandbox"}
	case tmux.AgentGemini:
		return []string{"gemini", "--yolo"}
	case tmux.AgentAntigravity:
		return []string{"agy", "--dangerously-skip-permissions"}
	case tmux.AgentCursor:
		return []string{"cursor"}
	case tmux.AgentWindsurf:
		return []string{"windsurf"}
	case tmux.AgentAider:
		return []string{"aider"}
	case tmux.AgentOpencode:
		return []string{"opencode"}
	case tmux.AgentOllama:
		return []string{"ollama", "run", "codellama:latest"}
	default:
		short := agentTypeShort(agentType)
		if short == "" || short == "user" {
			return nil
		}
		return []string{short}
	}
}

// backendMissingHint returns a human-readable install hint for the active backend.
func backendMissingHint() string {
	if backend.IsHerdr() {
		return "Install herdr and start the server (herdr status)"
	}
	return "Install tmux to enable this robot surface"
}

// backendMissingError returns a stable dependency-missing error for the active backend.
func backendMissingError() error {
	if backend.IsHerdr() {
		return fmt.Errorf("herdr is not installed")
	}
	return fmt.Errorf("tmux is not installed")
}
