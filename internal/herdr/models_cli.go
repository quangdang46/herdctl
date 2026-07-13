package herdr

// Typed fragments of Herdr CLI JSON results used by the adapter.
// Field names match live herdr 0.7.x output (snake_case).

type workspaceListResult struct {
	Type       string          `json:"type"`
	Workspaces []workspaceInfo `json:"workspaces"`
}

type workspaceInfo struct {
	WorkspaceID  string `json:"workspace_id"`
	Number       int    `json:"number"`
	Label        string `json:"label"`
	Focused      bool   `json:"focused"`
	PaneCount    int    `json:"pane_count"`
	TabCount     int    `json:"tab_count"`
	ActiveTabID  string `json:"active_tab_id"`
	AgentStatus  string `json:"agent_status"`
}

type workspaceCreatedResult struct {
	Type      string        `json:"type"`
	Workspace workspaceInfo `json:"workspace"`
	Tab       tabInfo       `json:"tab"`
	RootPane  paneInfo      `json:"root_pane"`
}

type workspaceInfoResult struct {
	Type      string        `json:"type"`
	Workspace workspaceInfo `json:"workspace"`
}

type tabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	AgentStatus string `json:"agent_status"`
}

type paneListResult struct {
	Type  string     `json:"type"`
	Panes []paneInfo `json:"panes"`
}

type paneInfo struct {
	PaneID         string `json:"pane_id"`
	WorkspaceID    string `json:"workspace_id"`
	TabID          string `json:"tab_id"`
	TerminalID     string `json:"terminal_id"`
	Cwd            string `json:"cwd"`
	ForegroundCwd  string `json:"foreground_cwd"`
	Focused        bool   `json:"focused"`
	AgentStatus    string `json:"agent_status"`
	Label          string `json:"label"`
	Revision       int    `json:"revision"`
	Scroll         *scrollInfo `json:"scroll"`
}

type scrollInfo struct {
	OffsetFromBottom    int `json:"offset_from_bottom"`
	MaxOffsetFromBottom int `json:"max_offset_from_bottom"`
	ViewportRows        int `json:"viewport_rows"`
}

type agentListResult struct {
	Type   string      `json:"type"`
	Agents []agentInfo `json:"agents"`
}

type agentInfo struct {
	Name          string `json:"name"`
	// Agent is the detected/reported agent label from herdr (e.g. "claude").
	Agent         string `json:"agent,omitempty"`
	PaneID        string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	TerminalID    string `json:"terminal_id"`
	Cwd           string `json:"cwd"`
	Focused       bool   `json:"focused"`
	AgentStatus   string `json:"agent_status"`
	ForegroundCwd string `json:"foreground_cwd"`
	Revision      int    `json:"revision"`
}

// agentExplainResult is a flexible container for `herdr agent explain --json`.
// Herdr may emit either a bare object or a CLI envelope; callers treat the
// decoded map as opaque explain evidence.
type agentExplainResult map[string]any

type agentStartedResult struct {
	Type  string    `json:"type"`
	Agent agentInfo `json:"agent"`
	Argv  []string  `json:"argv"`
}

type paneReadResult struct {
	Type string `json:"type"`
	Read struct {
		Text   string `json:"text"`
		Source string `json:"source"`
		Lines  int    `json:"lines"`
	} `json:"read"`
}

type agentReadResult struct {
	Type string `json:"type"`
	// agent.read embeds text similarly; keep flexible via raw helpers if needed.
	Text string `json:"text"`
	Read *struct {
		Text string `json:"text"`
	} `json:"read"`
}

type okResult struct {
	Type string `json:"type"`
}

// agentGetResult is the result of `herdr agent get <target>`.
type agentGetResult struct {
	Type  string    `json:"type"`
	Agent agentInfo `json:"agent"`
}

// agentWaitResult is the result of `herdr agent wait <target> --status ...`
// when the target is already in (or reaches) the desired status. Herdr may
// return either a full agent_info or a thin status event depending on path.
type agentWaitResult struct {
	Type  string     `json:"type"`
	Agent *agentInfo `json:"agent,omitempty"`
	// Event-shaped payloads (herdr wait agent-status) land here when decoded
	// as the raw result object; we only need success/failure from runJSON.
	Data *struct {
		PaneID      string `json:"pane_id"`
		AgentStatus string `json:"agent_status"`
	} `json:"data,omitempty"`
}

type paneSplitResult struct {
	Type string   `json:"type"`
	Pane paneInfo `json:"pane"`
}

type paneProcessInfoResult struct {
	Type string `json:"type"`
	// process_info shape varies; PID extracted best-effort in client code.
	ProcessInfo *struct {
		ShellPID int `json:"shell_pid"`
	} `json:"process_info"`
}
