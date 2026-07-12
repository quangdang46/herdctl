package checkpoint

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// mux* helpers dispatch core session/pane operations to tmux (default) or
// herdr (NTM_BACKEND=herdr). They convert herdr shapes into tmux shapes so the
// rest of checkpoint (typed on tmux.Pane) keeps compiling without mass refactors.
//
// Tmux-only fidelity (raw layouts, respawn-pane, select-pane, move-window) is
// skipped or best-effort under herdr; session/pane recreation still works.

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

func muxKillSession(session string) error {
	if backend.IsHerdr() {
		return herdr.KillSession(session)
	}
	return tmux.KillSession(session)
}

func muxCreateSession(name, directory string) error {
	if backend.IsHerdr() {
		return herdr.CreateSession(name, directory)
	}
	return tmux.CreateSession(name, directory)
}

func muxGetPanes(session string) ([]tmux.Pane, error) {
	if !backend.IsHerdr() {
		return tmux.GetPanes(session)
	}
	panes, err := herdr.GetPanes(session)
	if err != nil {
		return nil, err
	}
	return herdrPanesToTmux(panes), nil
}

func muxSetPaneTitle(paneID, title string) error {
	if backend.IsHerdr() {
		return herdr.SetPaneTitle(paneID, title)
	}
	return tmux.SetPaneTitle(paneID, title)
}

func muxSplitWindow(session, directory string) (string, error) {
	if backend.IsHerdr() {
		return herdr.SplitWindow(session, directory)
	}
	return tmux.SplitWindow(session, directory)
}

func muxCapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error) {
	if backend.IsHerdr() {
		return herdr.CapturePaneOutputContext(ctx, target, lines)
	}
	return tmux.CapturePaneOutputContext(ctx, target, lines)
}

// muxSendBuffer injects multi-line context into a pane. Herdr has no paste
// buffer; SendKeys is the closest equivalent.
func muxSendBuffer(target, content string, enter bool) error {
	if backend.IsHerdr() {
		return herdr.SendKeys(target, content, enter)
	}
	return tmux.SendBuffer(target, content, enter)
}

// muxSanitizePaneCommand validates a restore command. Herdr has no shell-escape
// respawn path; reuse tmux sanitizer (pure string validation) on both backends.
func muxSanitizePaneCommand(cmd string) (string, error) {
	return tmux.SanitizePaneCommand(cmd)
}

// muxGetSessionDir returns the working directory for a session.
func muxGetSessionDir(sessionName string) (string, error) {
	if !backend.IsHerdr() {
		return tmux.DefaultClient.Run("display-message", "-p", "-t", sessionName, "#{pane_current_path}")
	}
	s, err := herdr.GetSession(sessionName)
	if err != nil {
		return "", err
	}
	if s == nil || s.Directory == "" {
		return "", fmt.Errorf("herdr session %q has no directory", sessionName)
	}
	return s.Directory, nil
}

// muxGetSessionLayout returns a layout string for the session.
// Under herdr, layouts are unsupported — return empty (best-effort skip).
func muxGetSessionLayout(sessionName string) (string, error) {
	if backend.IsHerdr() {
		return "", nil
	}
	return tmux.DefaultClient.Run("display-message", "-p", "-t", sessionName, "#{window_layout}")
}

// muxGetSessionWindowLayouts returns per-window layout strings.
// Under herdr, layouts are unsupported — return empty slice.
func muxGetSessionWindowLayouts(sessionName string) ([]WindowLayoutState, error) {
	if backend.IsHerdr() {
		return nil, nil
	}
	out, err := tmux.DefaultClient.Run("list-windows", "-t", sessionName, "-F", "#{window_index}\t#{window_layout}")
	if err != nil {
		return nil, err
	}

	var layouts []WindowLayoutState
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("unexpected window layout format: %q", line)
		}
		windowIndex, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("parsing window index %q: %w", parts[0], err)
		}
		layout := strings.TrimSpace(parts[1])
		if layout == "" {
			continue
		}
		layouts = append(layouts, WindowLayoutState{
			WindowIndex: windowIndex,
			Layout:      layout,
		})
	}
	sortWindowLayouts(layouts)
	return layouts, nil
}

// muxGetSessionActivePaneID returns the currently selected pane id.
// Under herdr, prefer the Active flag from GetPanes; empty means "use fallback".
func muxGetSessionActivePaneID(sessionName string) (string, error) {
	if !backend.IsHerdr() {
		return tmux.DefaultClient.Run("display-message", "-p", "-t", sessionName, "#{pane_id}")
	}
	panes, err := herdr.GetPanes(sessionName)
	if err != nil {
		return "", err
	}
	for _, p := range panes {
		if p.Active {
			return p.ID, nil
		}
	}
	if len(panes) > 0 {
		return panes[0].ID, nil
	}
	return "", fmt.Errorf("herdr session %q has no panes", sessionName)
}

// muxCreatePane creates an additional pane in session. Under herdr this is a
// SplitWindow (tabs/windows are not 1:1 with tmux). Under tmux, windowTarget
// and newWindow control new-window vs split-window.
func muxCreatePane(sessionName string, windowIndex int, workDir string, newWindow bool) (string, error) {
	if backend.IsHerdr() {
		return herdr.SplitWindow(sessionName, workDir)
	}
	windowTarget := fmt.Sprintf("%s:%d", sessionName, windowIndex)
	if newWindow {
		return tmux.DefaultClient.Run(
			"new-window",
			"-P",
			"-F",
			"#{pane_id}",
			"-t",
			windowTarget,
			"-c",
			workDir,
		)
	}
	return tmux.DefaultClient.Run(
		"split-window",
		"-t",
		windowTarget,
		"-c",
		workDir,
		"-P",
		"-F",
		"#{pane_id}",
	)
}

// muxRespawnPane relaunches a pane into agentCmd. Under herdr there is no
// respawn-pane equivalent that targets an existing pane id, so we type the
// command into the restored placeholder pane via SendKeys. (StartAgent would
// create an extra pane and break positional restore mapping.)
func muxRespawnPane(paneID, workDir, safeCommand, agentCmd string, _ PaneState, _ string) error {
	if !backend.IsHerdr() {
		return tmux.DefaultClient.RunSilent("respawn-pane", "-k", "-c", workDir, "-t", paneID, safeCommand)
	}
	cmd := strings.TrimSpace(agentCmd)
	if cmd == "" {
		cmd = safeCommand
	}
	return herdr.SendKeys(paneID, cmd, true)
}

// muxWaitForPaneCommand is a no-op under herdr (no pane_current_command).
func muxCurrentPaneCommand(paneID string) (string, error) {
	if backend.IsHerdr() {
		// Best-effort: agent status is not the process name. Return empty so
		// wait loops that compare to expected command skip strict checks.
		status, err := herdr.GetAgentStatus(paneID)
		if err != nil {
			return "", err
		}
		return status, nil
	}
	output, err := tmux.DefaultClient.Run("display-message", "-p", "-t", paneID, "#{pane_current_command}")
	if err != nil {
		return "", fmt.Errorf("getting pane current command: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// muxMoveInitialWindow is tmux-only; herdr has no window renumbering.
func muxMoveInitialWindow(sessionName string, targetWindowIndex int) error {
	if backend.IsHerdr() {
		return nil
	}
	if targetWindowIndex < 0 {
		return nil
	}
	currentWindowIndex, err := tmux.GetFirstWindow(sessionName)
	if err != nil {
		return fmt.Errorf("getting initial window index: %w", err)
	}
	if currentWindowIndex == targetWindowIndex {
		return nil
	}
	source := fmt.Sprintf("%s:%d", sessionName, currentWindowIndex)
	target := fmt.Sprintf("%s:%d", sessionName, targetWindowIndex)
	if err := tmux.DefaultClient.RunSilent("move-window", "-s", source, "-t", target); err != nil {
		return fmt.Errorf("moving initial window from %s to %s: %w", source, target, err)
	}
	return nil
}

// muxSelectPane focuses a pane. Unsupported under herdr (no-op).
func muxSelectPane(paneID string) error {
	if backend.IsHerdr() {
		return nil
	}
	return tmux.DefaultClient.RunSilent("select-pane", "-t", paneID)
}

// muxApplyLayout applies a layout string. Unsupported under herdr (no-op).
func muxApplyLayout(sessionName, layout string) error {
	if backend.IsHerdr() {
		_ = herdr.ApplyTiledLayout(sessionName) // best-effort; may be ErrNotSupported
		return nil
	}
	if layout == "" {
		layout = "tiled"
	}
	output, err := tmux.DefaultClient.Run("list-windows", "-t", sessionName, "-F", "#{window_index}")
	if err != nil {
		return err
	}
	for _, win := range strings.Split(strings.TrimSpace(output), "\n") {
		if win == "" {
			continue
		}
		target := fmt.Sprintf("%s:%s", sessionName, win)
		if err := tmux.DefaultClient.RunSilent("select-layout", "-t", target, layout); err != nil {
			return err
		}
	}
	return nil
}

// muxApplyWindowLayouts applies per-window layouts. Unsupported under herdr.
func muxApplyWindowLayouts(sessionName string, windowLayouts []WindowLayoutState) error {
	if backend.IsHerdr() {
		_ = herdr.ApplyTiledLayout(sessionName)
		return nil
	}
	for _, windowLayout := range cloneWindowLayouts(windowLayouts) {
		layout := strings.TrimSpace(windowLayout.Layout)
		if layout == "" {
			layout = "tiled"
		}
		target := fmt.Sprintf("%s:%d", sessionName, windowLayout.WindowIndex)
		if err := tmux.DefaultClient.RunSilent("select-layout", "-t", target, layout); err != nil {
			return err
		}
	}
	return nil
}

// muxIsHerdr reports whether the herdr backend is selected.
func muxIsHerdr() bool { return backend.IsHerdr() }

func herdrPanesToTmux(in []herdr.Pane) []tmux.Pane {
	out := make([]tmux.Pane, 0, len(in))
	for i, p := range in {
		win, paneIdx := herdrPaneNumericWinPane(p.ID)
		if win == 0 && paneIdx == 0 {
			win, paneIdx = p.WindowIndex, p.Index
		}
		if paneIdx == 0 && p.Index == 0 {
			paneIdx = i
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

func herdrPaneNumericWinPane(herdrID string) (win, pane int) {
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

