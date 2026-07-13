package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agent"
)

// Client talks to a running Herdr server through the herdr CLI.
//
// It intentionally mirrors the method surface used by internal/tmux.Client for
// the core orchestration path. Methods without a clean Herdr mapping return
// ErrNotSupported so callers can branch explicitly.
//
// Remote SSH execution (tmux.Client.Remote) is not implemented in v1 — Herdr
// has its own remote/session model.
type Client struct {
	// Binary is the herdr executable. Empty → HERDR_BIN_PATH or "herdr".
	Binary string

	// Timeout per CLI call. Zero → 15s.
	Timeout time.Duration

	// RegistryPath overrides the default ~/.ntm/herdr/registry.json.
	RegistryPath string

	// EnterDelay is applied before sending Enter after text (agent TUI safety).
	// Zero → 50ms, matching tmux.DefaultEnterDelay.
	EnterDelay time.Duration

	reg *Registry
}

// DefaultClient is the process-wide local client.
var DefaultClient = NewClient()

// NewClient constructs a local Herdr client. Registry is loaded lazily.
func NewClient() *Client {
	return &Client{}
}

// DefaultEnterDelay matches internal/tmux.DefaultEnterDelay for agent TUIs.
const DefaultEnterDelay = 50 * time.Millisecond

// ShellEnterDelay matches internal/tmux.ShellEnterDelay.
const ShellEnterDelay = 150 * time.Millisecond

func (c *Client) enterDelayOrDefault() time.Duration {
	if c.EnterDelay > 0 {
		return c.EnterDelay
	}
	return DefaultEnterDelay
}

func (c *Client) registry() (*Registry, error) {
	if c.reg != nil {
		return c.reg, nil
	}
	reg, err := LoadRegistry(c.RegistryPath)
	if err != nil {
		return nil, err
	}
	c.reg = reg
	return reg, nil
}

// ---------------------------------------------------------------------------
// Install / availability
// ---------------------------------------------------------------------------

// IsInstalled reports whether the herdr binary is resolvable.
func (c *Client) IsInstalled() bool {
	_, err := c.lookPath()
	return err == nil
}

// IsInstalled reports whether the herdr binary is resolvable (default client).
func IsInstalled() bool { return DefaultClient.IsInstalled() }

// EnsureInstalled fails if herdr is missing. Unlike tmux.EnsureInstalled it
// does not require a running server — call Ping for that.
func (c *Client) EnsureInstalled() error {
	if _, err := c.lookPath(); err != nil {
		return fmt.Errorf("%w: install herdr (https://herdr.dev) or set HERDR_BIN_PATH", ErrUnavailable)
	}
	return nil
}

// EnsureInstalled is the package-level helper.
func EnsureInstalled() error { return DefaultClient.EnsureInstalled() }

// Ping checks that the Herdr server answers workspace list.
func (c *Client) Ping(ctx context.Context) error {
	var out workspaceListResult
	return c.runJSON(ctx, &out, "workspace", "list")
}

// Ping is the package-level helper.
func Ping() error { return DefaultClient.Ping(context.Background()) }

// InHerdr reports whether the current process is running inside a Herdr pane.
func InHerdr() bool {
	return os.Getenv("HERDR_ENV") == "1" || os.Getenv("HERDR_PANE_ID") != ""
}

// ---------------------------------------------------------------------------
// Session / workspace
// ---------------------------------------------------------------------------

// SessionExists reports whether an NTM session name is bound in the registry
// and the corresponding Herdr workspace still exists.
func (c *Client) SessionExists(name string) bool {
	reg, err := c.registry()
	if err != nil {
		return false
	}
	rec, ok := reg.GetSession(name)
	if !ok {
		// Also allow resolving by live workspace label for adopt-style flows.
		ws, err := c.findWorkspaceByLabel(context.Background(), name)
		return err == nil && ws != nil
	}
	var live workspaceListResult
	if err := c.runJSON(context.Background(), &live, "workspace", "list"); err != nil {
		return false
	}
	for _, w := range live.Workspaces {
		if w.WorkspaceID == rec.WorkspaceID {
			return true
		}
	}
	return false
}

// SessionExists is the package-level helper.
func SessionExists(name string) bool { return DefaultClient.SessionExists(name) }

// ListSessions returns sessions known to the registry that still exist in Herdr.
// Workspaces without a registry binding are omitted (they are not NTM sessions).
func (c *Client) ListSessions() ([]Session, error) {
	reg, err := c.registry()
	if err != nil {
		return nil, err
	}
	var live workspaceListResult
	if err := c.runJSON(context.Background(), &live, "workspace", "list"); err != nil {
		return nil, err
	}
	liveByID := map[string]workspaceInfo{}
	for _, w := range live.Workspaces {
		liveByID[w.WorkspaceID] = w
	}

	var out []Session
	for _, rec := range reg.ListSessions() {
		w, ok := liveByID[rec.WorkspaceID]
		if !ok {
			continue
		}
		out = append(out, Session{
			Name:        rec.Name,
			Directory:   rec.Directory,
			Windows:     w.TabCount,
			Attached:    w.Focused,
			WorkspaceID: w.WorkspaceID,
			Label:       w.Label,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ListSessions is the package-level helper.
func ListSessions() ([]Session, error) { return DefaultClient.ListSessions() }

// CreateSession creates a Herdr workspace labeled with name and records it.
// historyLimit is accepted for API parity with tmux but ignored (Herdr owns scrollback).
func (c *Client) CreateSession(name, directory string) error {
	return c.CreateSessionWithHistoryLimit(name, directory, 0)
}

// CreateSessionWithHistoryLimit creates a workspace; historyLimit is ignored.
func (c *Client) CreateSessionWithHistoryLimit(name, directory string, historyLimit int) error {
	_ = historyLimit
	if err := ValidateSessionName(name); err != nil {
		return err
	}
	if directory == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		directory = wd
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return err
	}

	reg, err := c.registry()
	if err != nil {
		return err
	}
	if rec, ok := reg.GetSession(name); ok {
		if c.SessionExists(name) {
			return fmt.Errorf("%w: session %q already bound to %s", ErrConflict, name, rec.WorkspaceID)
		}
		// Stale registry entry — drop it.
		_ = reg.DeleteSession(name)
	}

	// If a workspace with this label already exists, bind it instead of creating.
	if existing, err := c.findWorkspaceByLabel(context.Background(), name); err == nil && existing != nil {
		rootPane := ""
		rootTab := existing.ActiveTabID
		if panes, err := c.listPanes(context.Background(), existing.WorkspaceID); err == nil && len(panes) > 0 {
			rootPane = panes[0].PaneID
			rootTab = panes[0].TabID
		}
		rec := SessionRecord{
			Name:        name,
			WorkspaceID: existing.WorkspaceID,
			Directory:   directory,
			RootPaneID:  rootPane,
			RootTabID:   rootTab,
			Panes:       map[string]PaneMeta{},
		}
		if rootPane != "" {
			rec.Panes[rootPane] = PaneMeta{
				PaneID:      rootPane,
				Session:     name,
				WorkspaceID: existing.WorkspaceID,
				TabID:       rootTab,
				AgentType:   AgentUser,
				NTMIndex:    0,
				Title:       FormatPaneName(name, string(AgentUser), 0, ""),
				Cwd:         directory,
			}
		}
		return reg.PutSession(rec)
	}

	args := []string{
		"workspace", "create",
		"--cwd", directory,
		"--label", name,
		"--no-focus",
	}
	var created workspaceCreatedResult
	if err := c.runJSON(context.Background(), &created, args...); err != nil {
		return err
	}

	title := FormatPaneName(name, string(AgentUser), 0, "")
	rec := SessionRecord{
		Name:        name,
		WorkspaceID: created.Workspace.WorkspaceID,
		Directory:   directory,
		RootPaneID:  created.RootPane.PaneID,
		RootTabID:   created.Tab.TabID,
		Panes: map[string]PaneMeta{
			created.RootPane.PaneID: {
				PaneID:      created.RootPane.PaneID,
				Session:     name,
				WorkspaceID: created.Workspace.WorkspaceID,
				TabID:       created.Tab.TabID,
				TerminalID:  created.RootPane.TerminalID,
				AgentType:   AgentUser,
				NTMIndex:    0,
				Title:       title,
				Cwd:         directory,
				CreatedAt:   time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	if err := reg.PutSession(rec); err != nil {
		return err
	}
	// Best-effort: surface NTM title as Herdr pane label.
	_ = c.renamePane(context.Background(), created.RootPane.PaneID, title)
	return nil
}

// CreateSession is the package-level helper.
func CreateSession(name, directory string) error {
	return DefaultClient.CreateSession(name, directory)
}

// CreateSessionWithHistoryLimit is the package-level helper.
func CreateSessionWithHistoryLimit(name, directory string, historyLimit int) error {
	return DefaultClient.CreateSessionWithHistoryLimit(name, directory, historyLimit)
}

// KillSession closes the Herdr workspace and drops the registry binding.
func (c *Client) KillSession(session string) error {
	reg, err := c.registry()
	if err != nil {
		return err
	}
	rec, ok := reg.GetSession(session)
	if !ok {
		// Try label lookup for unbound workspaces.
		ws, err := c.findWorkspaceByLabel(context.Background(), session)
		if err != nil || ws == nil {
			return fmt.Errorf("%w: session %q", ErrNotFound, session)
		}
		rec = SessionRecord{Name: session, WorkspaceID: ws.WorkspaceID}
	}
	if err := c.runJSON(context.Background(), &okResult{}, "workspace", "close", rec.WorkspaceID); err != nil {
		// Still drop registry so we do not keep a zombie binding.
		_ = reg.DeleteSession(session)
		return err
	}
	return reg.DeleteSession(session)
}

// KillSession is the package-level helper.
func KillSession(session string) error { return DefaultClient.KillSession(session) }

// GetSession returns a Session with panes filled.
func (c *Client) GetSession(name string) (*Session, error) {
	reg, err := c.registry()
	if err != nil {
		return nil, err
	}
	rec, ok := reg.GetSession(name)
	if !ok {
		return nil, fmt.Errorf("%w: session %q", ErrNotFound, name)
	}
	ws, err := c.getWorkspace(context.Background(), rec.WorkspaceID)
	if err != nil {
		return nil, err
	}
	panes, err := c.GetPanes(name)
	if err != nil {
		return nil, err
	}
	return &Session{
		Name:        name,
		Directory:   rec.Directory,
		Windows:     ws.TabCount,
		Panes:       panes,
		Attached:    ws.Focused,
		WorkspaceID: ws.WorkspaceID,
		Label:       ws.Label,
	}, nil
}

// GetSession is the package-level helper.
func GetSession(name string) (*Session, error) { return DefaultClient.GetSession(name) }

// ---------------------------------------------------------------------------
// Panes
// ---------------------------------------------------------------------------

// GetPanes lists Herdr panes for the session workspace and merges registry meta.
func (c *Client) GetPanes(session string) ([]Pane, error) {
	return c.GetPanesContext(context.Background(), session)
}

// GetPanesContext is the context-aware variant.
func (c *Client) GetPanesContext(ctx context.Context, session string) ([]Pane, error) {
	reg, err := c.registry()
	if err != nil {
		return nil, err
	}
	rec, ok := reg.GetSession(session)
	if !ok {
		return nil, fmt.Errorf("%w: session %q", ErrNotFound, session)
	}
	live, err := c.listPanes(ctx, rec.WorkspaceID)
	if err != nil {
		return nil, err
	}

	// Drop registry panes that no longer exist.
	liveIDs := map[string]paneInfo{}
	for _, p := range live {
		liveIDs[p.PaneID] = p
	}
	for id := range rec.Panes {
		if _, ok := liveIDs[id]; !ok {
			_ = reg.RemovePane(session, id)
			delete(rec.Panes, id)
		}
	}

	out := make([]Pane, 0, len(live))
	for i, p := range live {
		meta, hasMeta := rec.Panes[p.PaneID]
		pane := Pane{
			ID:          p.PaneID,
			Index:       i,
			WindowIndex: tabIndexFromID(p.TabID),
			Title:       p.Label,
			Command:     "",
			Active:      p.Focused,
			WorkspaceID: p.WorkspaceID,
			TabID:       p.TabID,
			TerminalID:  p.TerminalID,
			Cwd:         firstNonEmpty(p.ForegroundCwd, p.Cwd),
			AgentStatus: p.AgentStatus,
			Label:       p.Label,
			Type:        AgentUser,
		}
		if hasMeta {
			pane.Type = meta.AgentType
			pane.NTMIndex = meta.NTMIndex
			pane.Variant = meta.Variant
			pane.Tags = append([]string{}, meta.Tags...)
			pane.Command = meta.Command
			if meta.Title != "" {
				pane.Title = meta.Title
			}
			if pane.Type == "" {
				pane.Type = AgentUser
			}
		} else if p.Label != "" {
			// Best-effort parse of NTM-style labels applied earlier.
			t, idx, variant, tags := ParseAgentFromTitle(p.Label)
			if t != AgentUser || idx > 0 {
				pane.Type = t
				pane.NTMIndex = idx
				pane.Variant = variant
				pane.Tags = tags
				pane.Title = p.Label
			}
		}
		if pane.Title == "" {
			pane.Title = FormatPaneName(session, string(pane.Type), pane.NTMIndex, pane.Variant)
		}
		if p.Scroll != nil {
			pane.Height = p.Scroll.ViewportRows
		}
		out = append(out, pane)
	}
	return out, nil
}

// GetPanes is the package-level helper.
func GetPanes(session string) ([]Pane, error) { return DefaultClient.GetPanes(session) }

// GetPanesContext is the package-level helper.
func GetPanesContext(ctx context.Context, session string) ([]Pane, error) {
	return DefaultClient.GetPanesContext(ctx, session)
}

// KillPane closes a Herdr pane and drops registry metadata when known.
func (c *Client) KillPane(paneID string) error {
	if err := c.runJSON(context.Background(), &okResult{}, "pane", "close", paneID); err != nil {
		return err
	}
	reg, err := c.registry()
	if err != nil {
		return nil
	}
	for _, rec := range reg.ListSessions() {
		if _, ok := rec.Panes[paneID]; ok {
			_ = reg.RemovePane(rec.Name, paneID)
		}
	}
	return nil
}

// KillPane is the package-level helper.
func KillPane(paneID string) error { return DefaultClient.KillPane(paneID) }

// SetPaneTitle stores the title in the registry and renames the Herdr pane label.
func (c *Client) SetPaneTitle(paneID, title string) error {
	reg, err := c.registry()
	if err != nil {
		return err
	}
	t, idx, variant, tags := ParseAgentFromTitle(title)
	for _, rec := range reg.ListSessions() {
		if meta, ok := rec.Panes[paneID]; ok {
			meta.Title = title
			meta.AgentType = t
			if idx > 0 {
				meta.NTMIndex = idx
			}
			meta.Variant = variant
			meta.Tags = tags
			_ = reg.UpsertPane(rec.Name, meta)
			break
		}
	}
	return c.renamePane(context.Background(), paneID, title)
}

// SetPaneTitle is the package-level helper.
func SetPaneTitle(paneID, title string) error { return DefaultClient.SetPaneTitle(paneID, title) }

// ---------------------------------------------------------------------------
// Split / start agent
// ---------------------------------------------------------------------------

// SplitWindow splits the focused/root pane of the session and returns the new pane id.
// Unlike tmux, Herdr split does not start an agent — callers should use StartAgent
// when they need a coding agent process.
func (c *Client) SplitWindow(session, directory string) (string, error) {
	reg, err := c.registry()
	if err != nil {
		return "", err
	}
	rec, ok := reg.GetSession(session)
	if !ok {
		return "", fmt.Errorf("%w: session %q", ErrNotFound, session)
	}
	target := rec.RootPaneID
	if target == "" {
		panes, err := c.listPanes(context.Background(), rec.WorkspaceID)
		if err != nil {
			return "", err
		}
		if len(panes) == 0 {
			return "", fmt.Errorf("%w: no panes in session %q", ErrNotFound, session)
		}
		target = panes[0].PaneID
	}
	args := []string{"pane", "split", target, "--direction", "right", "--no-focus"}
	if directory != "" {
		abs, err := filepath.Abs(directory)
		if err != nil {
			return "", err
		}
		args = append(args, "--cwd", abs)
	}
	// pane split result shape may vary; fall back to pane list diff if needed.
	var beforeIDs map[string]struct{}
	if before, err := c.listPanes(context.Background(), rec.WorkspaceID); err == nil {
		beforeIDs = map[string]struct{}{}
		for _, p := range before {
			beforeIDs[p.PaneID] = struct{}{}
		}
	}
	_ = c.runJSON(context.Background(), &okResult{}, args...)
	after, err := c.listPanes(context.Background(), rec.WorkspaceID)
	if err != nil {
		return "", err
	}
	var newID string
	for _, p := range after {
		if beforeIDs != nil {
			if _, ok := beforeIDs[p.PaneID]; !ok {
				newID = p.PaneID
				break
			}
		}
	}
	if newID == "" && len(after) > 0 {
		newID = after[len(after)-1].PaneID
	}
	if newID == "" {
		return "", fmt.Errorf("split window: could not determine new pane id")
	}
	meta := PaneMeta{
		PaneID:      newID,
		Session:     session,
		WorkspaceID: rec.WorkspaceID,
		AgentType:   AgentUser,
		NTMIndex:    0,
		Title:       FormatPaneName(session, string(AgentUser), 0, ""),
		Cwd:         directory,
	}
	_ = reg.UpsertPane(session, meta)
	return newID, nil
}

// SplitWindow is the package-level helper.
func SplitWindow(session, directory string) (string, error) {
	return DefaultClient.SplitWindow(session, directory)
}

// StartAgentOptions configures agent.start on Herdr.
type StartAgentOptions struct {
	Session   string
	Name      string // Herdr agent name / label
	AgentType AgentType
	Index     int // NTM index; 0 → auto-assign
	Variant   string
	Tags      []string
	Cwd       string
	Argv      []string
	Focus     bool
	Split     string // "right", "down", or empty for default
}

// StartAgent launches an agent process via `herdr agent start` and records metadata.
func (c *Client) StartAgent(ctx context.Context, opts StartAgentOptions) (Pane, error) {
	if err := ValidateSessionName(opts.Session); err != nil {
		return Pane{}, err
	}
	reg, err := c.registry()
	if err != nil {
		return Pane{}, err
	}
	rec, ok := reg.GetSession(opts.Session)
	if !ok {
		return Pane{}, fmt.Errorf("%w: session %q", ErrNotFound, opts.Session)
	}
	if opts.AgentType == "" {
		opts.AgentType = AgentUnknown
	}
	if opts.Index <= 0 {
		opts.Index = NextNTMIndex(rec.Panes, opts.AgentType)
	}
	if opts.Name == "" {
		opts.Name = fmt.Sprintf("%s-%s_%d", opts.Session, opts.AgentType.Canonical(), opts.Index)
	}
	if opts.Cwd == "" {
		opts.Cwd = rec.Directory
	}
	if len(opts.Argv) == 0 {
		return Pane{}, fmt.Errorf("StartAgent: argv required")
	}

	args := []string{
		"agent", "start", opts.Name,
		"--workspace", rec.WorkspaceID,
		"--cwd", opts.Cwd,
	}
	if opts.Focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	if opts.Split == "right" || opts.Split == "down" {
		args = append(args, "--split", opts.Split)
	}
	args = append(args, "--")
	args = append(args, opts.Argv...)

	var started agentStartedResult
	if err := c.runJSON(ctx, &started, args...); err != nil {
		return Pane{}, err
	}

	title := FormatPaneName(opts.Session, string(opts.AgentType.Canonical()), opts.Index, opts.Variant)
	if tagStr := FormatTags(opts.Tags); tagStr != "" {
		title += tagStr
	}
	meta := PaneMeta{
		PaneID:      started.Agent.PaneID,
		Session:     opts.Session,
		WorkspaceID: started.Agent.WorkspaceID,
		TabID:       started.Agent.TabID,
		TerminalID:  started.Agent.TerminalID,
		AgentType:   opts.AgentType.Canonical(),
		NTMIndex:    opts.Index,
		Variant:     opts.Variant,
		Tags:        append([]string{}, opts.Tags...),
		Title:       title,
		Command:     strings.Join(opts.Argv, " "),
		Cwd:         opts.Cwd,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := reg.UpsertPane(opts.Session, meta); err != nil {
		return Pane{}, err
	}
	_ = c.renamePane(ctx, started.Agent.PaneID, title)

	return Pane{
		ID:          started.Agent.PaneID,
		NTMIndex:    opts.Index,
		Title:       title,
		Type:        opts.AgentType.Canonical(),
		Variant:     opts.Variant,
		Tags:        append([]string{}, opts.Tags...),
		Command:     meta.Command,
		WorkspaceID: started.Agent.WorkspaceID,
		TabID:       started.Agent.TabID,
		TerminalID:  started.Agent.TerminalID,
		Cwd:         opts.Cwd,
		Label:       opts.Name,
		AgentStatus: started.Agent.AgentStatus,
	}, nil
}

// StartAgent is the package-level helper.
func StartAgent(ctx context.Context, opts StartAgentOptions) (Pane, error) {
	return DefaultClient.StartAgent(ctx, opts)
}

// ---------------------------------------------------------------------------
// Send / capture
// ---------------------------------------------------------------------------

// SendKeys sends text to a pane. When enter is true it uses `pane run` for a
// single atomic submit when possible; otherwise send-text + send-keys enter.
//
// Legacy tmux control sequences like "C-c" are translated to Herdr key combos.
func (c *Client) SendKeys(target, keys string, enter bool) error {
	return c.SendKeysWithDelay(target, keys, enter, c.enterDelayOrDefault())
}

// SendKeysWithDelay sends keys, sleeping enterDelay before Enter when needed.
func (c *Client) SendKeysWithDelay(target, keys string, enter bool, enterDelay time.Duration) error {
	if translated, ok := translateNamedKey(keys); ok {
		return c.runJSON(context.Background(), &okResult{}, "pane", "send-keys", target, translated)
	}
	// Always stage text with send-text. Avoid `pane run` for long/complex shell
	// commands (spawn injects env + hooks + quoted paths); some Herdr builds
	// return empty stdout for those and the CLI envelope decoder fails closed.
	if keys != "" {
		if err := c.runJSON(context.Background(), &okResult{}, "pane", "send-text", target, keys); err != nil {
			return err
		}
	}
	if !enter {
		return nil
	}
	if enterDelay > 0 {
		time.Sleep(enterDelay)
	}
	return c.runJSON(context.Background(), &okResult{}, "pane", "send-keys", target, "enter")
}

// SendKeys is the package-level helper.
func SendKeys(target, keys string, enter bool) error {
	return DefaultClient.SendKeys(target, keys, enter)
}

// SendKeysWithDelay is the package-level helper.
func SendKeysWithDelay(target, keys string, enter bool, enterDelay time.Duration) error {
	return DefaultClient.SendKeysWithDelay(target, keys, enter, enterDelay)
}

// SendKeysForAgent sends keys with agent-aware enter delay defaults.
func (c *Client) SendKeysForAgent(target, keys string, enter bool, agentType AgentType) error {
	delay := c.enterDelayOrDefault()
	if agentType == AgentUser {
		delay = ShellEnterDelay
	}
	return c.SendKeysWithDelay(target, keys, enter, delay)
}

// SendKeysForAgent is the package-level helper.
func SendKeysForAgent(target, keys string, enter bool, agentType AgentType) error {
	return DefaultClient.SendKeysForAgent(target, keys, enter, agentType)
}

// SendKeysForAgentWithDelay is the explicit-delay variant.
func (c *Client) SendKeysForAgentWithDelay(target, keys string, enter bool, enterDelay time.Duration, agentType AgentType) error {
	_ = agentType
	return c.SendKeysWithDelay(target, keys, enter, enterDelay)
}

// SendKeysForAgentWithDelay is the package-level helper.
func SendKeysForAgentWithDelay(target, keys string, enter bool, enterDelay time.Duration, agentType AgentType) error {
	return DefaultClient.SendKeysForAgentWithDelay(target, keys, enter, enterDelay, agentType)
}

// AgentSend uses `herdr agent send` (preferred for agent panes with identity).
func (c *Client) AgentSend(target, text string) error {
	return c.runJSON(context.Background(), &okResult{}, "agent", "send", target, text)
}

// AgentSend is the package-level helper.
func AgentSend(target, text string) error { return DefaultClient.AgentSend(target, text) }

// Agent is the NTM-facing view of a herdr agent entry.
type Agent struct {
	Name        string `json:"name"`
	Agent       string `json:"agent,omitempty"` // detected/reported agent label (e.g. "claude")
	PaneID      string `json:"pane_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	TabID       string `json:"tab_id,omitempty"`
	TerminalID  string `json:"terminal_id,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Focused     bool   `json:"focused"`
	AgentStatus string `json:"agent_status,omitempty"`
	Label       string `json:"label,omitempty"` // same as Name when set
}

func agentFromInfo(a agentInfo) Agent {
	return Agent{
		Name:        a.Name,
		Agent:       firstNonEmpty(a.Agent, a.Name),
		PaneID:      a.PaneID,
		WorkspaceID: a.WorkspaceID,
		TabID:       a.TabID,
		TerminalID:  a.TerminalID,
		Cwd:         firstNonEmpty(a.ForegroundCwd, a.Cwd),
		Focused:     a.Focused,
		AgentStatus: strings.ToLower(strings.TrimSpace(a.AgentStatus)),
		Label:       a.Name,
	}
}

// ListAgents returns all agents known to the herdr server (global list).
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var out agentListResult
	if err := c.runJSON(ctx, &out, "agent", "list"); err != nil {
		return nil, err
	}
	agents := make([]Agent, 0, len(out.Agents))
	for _, a := range out.Agents {
		agents = append(agents, agentFromInfo(a))
	}
	return agents, nil
}

// ListAgents is the package-level helper.
func ListAgents(ctx context.Context) ([]Agent, error) {
	return DefaultClient.ListAgents(ctx)
}

// ListAgentsForSession filters ListAgents to agents bound to the given NTM session.
// Matching uses workspace_id via the registry, then falls back to name prefix
// "<session>-" used by StartAgent naming.
func (c *Client) ListAgentsForSession(ctx context.Context, session string) ([]Agent, error) {
	session = strings.TrimSpace(session)
	all, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	if session == "" {
		return all, nil
	}
	var workspaceID string
	if reg, rerr := c.registry(); rerr == nil {
		if rec, ok := reg.GetSession(session); ok {
			workspaceID = rec.WorkspaceID
		}
	}
	prefix := session + "-"
	out := make([]Agent, 0, len(all))
	for _, a := range all {
		if workspaceID != "" && a.WorkspaceID == workspaceID {
			out = append(out, a)
			continue
		}
		if strings.HasPrefix(a.Name, prefix) {
			out = append(out, a)
		}
	}
	return out, nil
}

// ListAgentsForSession is the package-level helper.
func ListAgentsForSession(ctx context.Context, session string) ([]Agent, error) {
	return DefaultClient.ListAgentsForSession(ctx, session)
}

// GetAgent returns full agent info for a target (name, pane id, or terminal id).
func (c *Client) GetAgent(ctx context.Context, target string) (Agent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return Agent{}, fmt.Errorf("%w: empty agent target", ErrNotFound)
	}
	var out agentGetResult
	if err := c.runJSON(ctx, &out, "agent", "get", target); err != nil {
		return Agent{}, err
	}
	return agentFromInfo(out.Agent), nil
}

// GetAgent is the package-level helper.
func GetAgent(ctx context.Context, target string) (Agent, error) {
	return DefaultClient.GetAgent(ctx, target)
}

// ReadAgent captures recent/visible text via `herdr agent read`.
// source is "recent" (default), "visible", or "recent-unwrapped".
func (c *Client) ReadAgent(ctx context.Context, target string, lines int, source string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("%w: empty agent target", ErrNotFound)
	}
	if lines <= 0 {
		lines = 50
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "recent"
	}
	args := []string{
		"agent", "read", target,
		"--source", source,
		"--lines", strconv.Itoa(lines),
	}
	var out paneReadResult
	if err := c.runJSON(ctx, &out, args...); err != nil {
		// Fall back to flexible agentReadResult shape.
		var aout agentReadResult
		if err2 := c.runJSON(ctx, &aout, args...); err2 != nil {
			return "", err
		}
		if aout.Read != nil && aout.Read.Text != "" {
			return aout.Read.Text, nil
		}
		return aout.Text, nil
	}
	return out.Read.Text, nil
}

// ReadAgent is the package-level helper.
func ReadAgent(ctx context.Context, target string, lines int, source string) (string, error) {
	return DefaultClient.ReadAgent(ctx, target, lines, source)
}

// RenameAgent renames a herdr agent. Pass clear=true to clear the name
// (herdr agent rename <target> --clear).
func (c *Client) RenameAgent(ctx context.Context, target, name string, clear bool) (Agent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return Agent{}, fmt.Errorf("%w: empty agent target", ErrNotFound)
	}
	args := []string{"agent", "rename", target}
	if clear {
		args = append(args, "--clear")
	} else {
		name = strings.TrimSpace(name)
		if name == "" {
			return Agent{}, fmt.Errorf("rename requires a new name (or --clear)")
		}
		args = append(args, name)
	}
	var out agentGetResult
	if err := c.runJSON(ctx, &out, args...); err != nil {
		return Agent{}, err
	}
	return agentFromInfo(out.Agent), nil
}

// RenameAgent is the package-level helper.
func RenameAgent(ctx context.Context, target, name string, clear bool) (Agent, error) {
	return DefaultClient.RenameAgent(ctx, target, name, clear)
}

// FocusAgent focuses a herdr agent pane (herdr agent focus <target>).
func (c *Client) FocusAgent(ctx context.Context, target string) (Agent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return Agent{}, fmt.Errorf("%w: empty agent target", ErrNotFound)
	}
	var out agentGetResult
	if err := c.runJSON(ctx, &out, "agent", "focus", target); err != nil {
		return Agent{}, err
	}
	return agentFromInfo(out.Agent), nil
}

// FocusAgent is the package-level helper.
func FocusAgent(ctx context.Context, target string) (Agent, error) {
	return DefaultClient.FocusAgent(ctx, target)
}

// ExplainAgent returns detection-explain evidence from `herdr agent explain --json`.
// The result is an opaque JSON-compatible map (rule matches, evidence, state).
func (c *Client) ExplainAgent(ctx context.Context, target string) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("%w: empty agent target", ErrNotFound)
	}
	// Prefer the CLI envelope path first.
	var out map[string]any
	if err := c.runJSON(ctx, &out, "agent", "explain", target, "--json"); err == nil && out != nil {
		return out, nil
	}
	// Some herdr builds emit bare JSON (no {"id","result"} envelope) for explain.
	return c.runRawJSON(ctx, "agent", "explain", target, "--json")
}

// ExplainAgent is the package-level helper.
func ExplainAgent(ctx context.Context, target string) (map[string]any, error) {
	return DefaultClient.ExplainAgent(ctx, target)
}

// runRawJSON executes herdr and unmarshals stdout as a bare JSON object
// (no CLI envelope). Used for commands like agent explain --json that may
// print the payload directly.
func (c *Client) runRawJSON(ctx context.Context, args ...string) (map[string]any, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), c.timeout())
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.binary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderr.String())
	if err != nil {
		if parsed := tryParseCLIError(outStr, errStr); parsed != nil {
			return nil, &CommandError{Args: args, Status: exitStatus(err), Stdout: outStr, Stderr: errStr, Err: parsed}
		}
		return nil, &CommandError{Args: args, Status: exitStatus(err), Stdout: outStr, Stderr: errStr, Err: err}
	}
	if outStr == "" {
		return nil, fmt.Errorf("%w: empty stdout for herdr %v", ErrUnavailable, args)
	}
	// Strip a possible CLI envelope.
	var envelope cliResponse
	if jerr := json.Unmarshal([]byte(outStr), &envelope); jerr == nil && len(envelope.Result) > 0 {
		var m map[string]any
		if jerr := json.Unmarshal(envelope.Result, &m); jerr == nil {
			return m, nil
		}
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(outStr), &m); jerr != nil {
		return nil, fmt.Errorf("decode herdr raw json for %v: %w\nstdout=%s", args, jerr, outStr)
	}
	return m, nil
}

// SendInterrupt sends Ctrl+C to a pane.
func (c *Client) SendInterrupt(target string) error {
	return c.runJSON(context.Background(), &okResult{}, "pane", "send-keys", target, "ctrl+c")
}

// SendInterrupt is the package-level helper.
func SendInterrupt(target string) error { return DefaultClient.SendInterrupt(target) }

// SendEOF sends Ctrl+D to a pane.
func (c *Client) SendEOF(target string) error {
	return c.runJSON(context.Background(), &okResult{}, "pane", "send-keys", target, "ctrl+d")
}

// SendEOF is the package-level helper.
func SendEOF(target string) error { return DefaultClient.SendEOF(target) }

// CapturePaneOutput reads recent pane text (NTM capture-pane equivalent).
func (c *Client) CapturePaneOutput(target string, lines int) (string, error) {
	return c.CapturePaneOutputContext(context.Background(), target, lines)
}

// CapturePaneOutputContext is the context-aware variant.
func (c *Client) CapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error) {
	if lines <= 0 {
		lines = 50
	}
	args := []string{
		"pane", "read", target,
		"--source", "recent",
		"--lines", strconv.Itoa(lines),
	}
	var out paneReadResult
	if err := c.runJSON(ctx, &out, args...); err != nil {
		// Fallback: agent read
		var aout agentReadResult
		if err2 := c.runJSON(ctx, &aout, "agent", "read", target, "--source", "recent", "--lines", strconv.Itoa(lines)); err2 != nil {
			return "", err
		}
		if aout.Read != nil && aout.Read.Text != "" {
			return aout.Read.Text, nil
		}
		return aout.Text, nil
	}
	return out.Read.Text, nil
}

// CapturePaneOutput is the package-level helper.
func CapturePaneOutput(target string, lines int) (string, error) {
	return DefaultClient.CapturePaneOutput(target, lines)
}

// CapturePaneOutputContext is the package-level helper.
func CapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error) {
	return DefaultClient.CapturePaneOutputContext(ctx, target, lines)
}

// CapturePaneVisible reads the visible viewport.
func (c *Client) CapturePaneVisible(target string) (string, error) {
	var out paneReadResult
	if err := c.runJSON(context.Background(), &out, "pane", "read", target, "--source", "visible"); err != nil {
		return "", err
	}
	return out.Read.Text, nil
}

// CapturePaneVisible is the package-level helper.
func CapturePaneVisible(target string) (string, error) {
	return DefaultClient.CapturePaneVisible(target)
}

// ---------------------------------------------------------------------------
// Agent status / wait
// ---------------------------------------------------------------------------

// AgentStatus constants mirror herdr's reported agent_status values.
const (
	AgentStatusIdle    = "idle"
	AgentStatusWorking = "working"
	AgentStatusBlocked = "blocked"
	AgentStatusDone    = "done"
	AgentStatusUnknown = "unknown"
)

// GetAgentStatus returns the current herdr agent_status for a pane/agent target.
// Empty string means unknown / not reported.
func (c *Client) GetAgentStatus(target string) (string, error) {
	return c.GetAgentStatusContext(context.Background(), target)
}

// GetAgentStatusContext is the context-aware variant.
func (c *Client) GetAgentStatusContext(ctx context.Context, target string) (string, error) {
	var out agentGetResult
	if err := c.runJSON(ctx, &out, "agent", "get", target); err != nil {
		return "", err
	}
	status := strings.TrimSpace(out.Agent.AgentStatus)
	if status == "" {
		return AgentStatusUnknown, nil
	}
	return strings.ToLower(status), nil
}

// GetAgentStatus is the package-level helper.
func GetAgentStatus(target string) (string, error) {
	return DefaultClient.GetAgentStatus(target)
}

// GetAgentStatusContext is the package-level helper.
func GetAgentStatusContext(ctx context.Context, target string) (string, error) {
	return DefaultClient.GetAgentStatusContext(ctx, target)
}

// WaitAgentStatus blocks until the target reports the desired agent_status
// (idle|working|blocked|done|unknown) or the timeout elapses.
//
// timeoutMS <= 0 uses herdr's default timeout. Uses `herdr agent wait`, which
// returns immediately when the pane is already in the desired status.
func (c *Client) WaitAgentStatus(target, status string, timeoutMS int) error {
	return c.WaitAgentStatusContext(context.Background(), target, status, timeoutMS)
}

// WaitAgentStatusContext is the context-aware variant. The process context
// bounds the overall wait; timeoutMS is also passed to herdr for its own
// deadline so both layers agree.
func (c *Client) WaitAgentStatusContext(ctx context.Context, target, status string, timeoutMS int) error {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case AgentStatusIdle, AgentStatusWorking, AgentStatusBlocked, AgentStatusDone, AgentStatusUnknown:
		// ok
	default:
		return fmt.Errorf("invalid agent status %q: must be idle|working|blocked|done|unknown", status)
	}

	args := []string{"agent", "wait", target, "--status", status}
	if timeoutMS > 0 {
		args = append(args, "--timeout", strconv.Itoa(timeoutMS))
	}

	// Give runJSON a deadline that covers the herdr timeout plus a small
	// cushion so context cancellation and herdr's own timeout stay in sync.
	if timeoutMS > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond+2*time.Second)
			defer cancel()
		}
	}

	var out agentWaitResult
	if err := c.runJSON(ctx, &out, args...); err != nil {
		// Prefer the wait agent-status form as a fallback (same semantics).
		args2 := []string{"wait", "agent-status", target, "--status", status}
		if timeoutMS > 0 {
			args2 = append(args2, "--timeout", strconv.Itoa(timeoutMS))
		}
		if err2 := c.runJSON(ctx, &out, args2...); err2 != nil {
			return err
		}
	}
	return nil
}

// WaitAgentStatus is the package-level helper.
func WaitAgentStatus(target, status string, timeoutMS int) error {
	return DefaultClient.WaitAgentStatus(target, status, timeoutMS)
}

// WaitAgentStatusContext is the package-level helper.
func WaitAgentStatusContext(ctx context.Context, target, status string, timeoutMS int) error {
	return DefaultClient.WaitAgentStatusContext(ctx, target, status, timeoutMS)
}

// ---------------------------------------------------------------------------
// Layout / zoom / focus
// ---------------------------------------------------------------------------

// ZoomPane zooms a pane inside its tab.
func (c *Client) ZoomPane(session string, paneIndex int) error {
	panes, err := c.GetPanes(session)
	if err != nil {
		return err
	}
	if paneIndex < 0 || paneIndex >= len(panes) {
		// Treat paneIndex as NTMIndex fallback.
		for _, p := range panes {
			if p.NTMIndex == paneIndex || p.Index == paneIndex {
				return c.runJSON(context.Background(), &okResult{}, "pane", "zoom", p.ID, "--on")
			}
		}
		return fmt.Errorf("%w: pane index %d in session %q", ErrNotFound, paneIndex, session)
	}
	return c.runJSON(context.Background(), &okResult{}, "pane", "zoom", panes[paneIndex].ID, "--on")
}

// ZoomPane is the package-level helper.
func ZoomPane(session string, paneIndex int) error {
	return DefaultClient.ZoomPane(session, paneIndex)
}

// ZoomPaneID zooms by Herdr pane id.
func (c *Client) ZoomPaneID(paneID string) error {
	return c.runJSON(context.Background(), &okResult{}, "pane", "zoom", paneID, "--on")
}

// UnzoomPaneID clears zoom on a Herdr pane (`pane zoom --off`).
func (c *Client) UnzoomPaneID(paneID string) error {
	if strings.TrimSpace(paneID) == "" {
		return fmt.Errorf("%w: empty pane id", ErrNotFound)
	}
	return c.runJSON(context.Background(), &okResult{}, "pane", "zoom", paneID, "--off")
}

// UnzoomPaneID is the package-level helper.
func UnzoomPaneID(paneID string) error { return DefaultClient.UnzoomPaneID(paneID) }

// UnzoomAllPanes clears zoom on every pane in the session. Herdr has no
// `select-layout tiled` equivalent; unzoom is the closest operator action for
// "show all panes again" after a zoomed view.
func (c *Client) UnzoomAllPanes(session string) error {
	panes, err := c.GetPanes(session)
	if err != nil {
		return err
	}
	var firstErr error
	for _, p := range panes {
		if p.ID == "" {
			continue
		}
		if err := c.UnzoomPaneID(p.ID); err != nil && firstErr == nil {
			// --off on an already-unzoomed pane is typically a no-op success;
			// keep the first real error for the caller.
			firstErr = err
		}
	}
	return firstErr
}

// UnzoomAllPanes is the package-level helper.
func UnzoomAllPanes(session string) error { return DefaultClient.UnzoomAllPanes(session) }

// ApplyTiledLayout is not a direct Herdr primitive (no select-layout tiled).
// Best-effort: unzoom every pane when the session resolves, then always return
// ErrNotSupported so callers can print herdr TUI tiling guidance. Use
// UnzoomAllPanes when unzoom-only success is enough.
func (c *Client) ApplyTiledLayout(session string) error {
	// Best-effort unzoom; ignore resolution failures so unit tests and offline
	// callers still get a stable ErrNotSupported for the missing tile API.
	_ = c.UnzoomAllPanes(session)
	return notSupported("ApplyTiledLayout", "unzoomed panes when possible; herdr has no select-layout tiled — open the herdr TUI for manual tiling")
}

// ApplyTiledLayout is the package-level helper.
func ApplyTiledLayout(session string) error { return DefaultClient.ApplyTiledLayout(session) }

// AttachOrSwitch is intentionally unsupported: Herdr uses a client/server TUI.
// Operators run `herdr` (or `herdr session attach`) themselves.
func (c *Client) AttachOrSwitch(session string) error {
	return notSupported("AttachOrSwitch", "run `herdr` client / `herdr session attach` instead of tmux attach")
}

// AttachOrSwitch is the package-level helper.
func AttachOrSwitch(session string) error { return DefaultClient.AttachOrSwitch(session) }

// ---------------------------------------------------------------------------
// Unsupported tmux-only surfaces (explicit stubs)
// ---------------------------------------------------------------------------

// SetPaneBorderStyle is not available through Herdr public API.
func (c *Client) SetPaneBorderStyle(target, color string) error {
	return notSupported("SetPaneBorderStyle", "herdr owns chrome styling")
}

// ResetPaneBorderStyle is not available through Herdr public API.
func (c *Client) ResetPaneBorderStyle(target string) error {
	return notSupported("ResetPaneBorderStyle", "herdr owns chrome styling")
}

// DisplayMessage maps best-effort onto herdr notification show.
func (c *Client) DisplayMessage(session, msg string, durationMs int) error {
	_ = session
	_ = durationMs
	title := "herdctl"
	return c.runJSON(context.Background(), &okResult{},
		"notification", "show", title, "--body", msg)
}

// DisplayMessage is the package-level helper.
func DisplayMessage(session, msg string, durationMs int) error {
	return DefaultClient.DisplayMessage(session, msg, durationMs)
}

// GetCurrentSession returns the registry session bound to the focused workspace.
func (c *Client) GetCurrentSession() string {
	var live workspaceListResult
	if err := c.runJSON(context.Background(), &live, "workspace", "list"); err != nil {
		return ""
	}
	reg, err := c.registry()
	if err != nil {
		return ""
	}
	for _, w := range live.Workspaces {
		if !w.Focused {
			continue
		}
		if rec, ok := reg.FindSessionByWorkspace(w.WorkspaceID); ok {
			return rec.Name
		}
		// Fall back to label if it is a valid session name.
		if ValidateSessionName(w.Label) == nil {
			return w.Label
		}
	}
	return ""
}

// GetCurrentSession is the package-level helper.
func GetCurrentSession() string { return DefaultClient.GetCurrentSession() }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (c *Client) findWorkspaceByLabel(ctx context.Context, label string) (*workspaceInfo, error) {
	var live workspaceListResult
	if err := c.runJSON(ctx, &live, "workspace", "list"); err != nil {
		return nil, err
	}
	for i := range live.Workspaces {
		if live.Workspaces[i].Label == label {
			return &live.Workspaces[i], nil
		}
	}
	return nil, nil
}

func (c *Client) getWorkspace(ctx context.Context, id string) (*workspaceInfo, error) {
	var out workspaceInfoResult
	if err := c.runJSON(ctx, &out, "workspace", "get", id); err != nil {
		return nil, err
	}
	return &out.Workspace, nil
}

func (c *Client) listPanes(ctx context.Context, workspaceID string) ([]paneInfo, error) {
	args := []string{"pane", "list"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	var out paneListResult
	if err := c.runJSON(ctx, &out, args...); err != nil {
		return nil, err
	}
	return out.Panes, nil
}

func (c *Client) renamePane(ctx context.Context, paneID, label string) error {
	if label == "" {
		return nil
	}
	// Prefer pane rename; agent rename requires agent identity.
	return c.runJSON(ctx, &okResult{}, "pane", "rename", paneID, label)
}

func tabIndexFromID(tabID string) int {
	// tab ids look like "w1:t2" → 2
	if tabID == "" {
		return 0
	}
	parts := strings.Split(tabID, ":t")
	if len(parts) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[1])
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// translateNamedKey maps legacy tmux-style keys onto Herdr key-combo tokens.
func translateNamedKey(keys string) (string, bool) {
	switch strings.TrimSpace(keys) {
	case "C-c", "c-c", "C-C", "^C":
		return "ctrl+c", true
	case "C-d", "c-d", "C-D", "^D":
		return "ctrl+d", true
	case "C-z", "c-z":
		return "ctrl+z", true
	case "Enter", "C-m", "c-m":
		return "enter", true
	case "Escape", "Esc", "C-[":
		return "esc", true
	case "BSpace", "Backspace":
		return "backspace", true
	case "Tab", "C-i":
		return "tab", true
	default:
		// Single herdr-style combo already.
		if strings.Contains(keys, "+") && !strings.Contains(keys, " ") {
			return keys, true
		}
		return "", false
	}
}

// CanonicalAgentType normalizes free-form agent type strings.
func CanonicalAgentType(v string) AgentType {
	return agent.AgentType(v).Canonical()
}
