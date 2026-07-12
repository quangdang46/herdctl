package herdr

import (
	"github.com/Dicklesworthstone/ntm/internal/agent"
)

// AgentType reuses NTM agent type aliases so call sites can share constants.
type AgentType = agent.AgentType

const (
	AgentClaude      = agent.AgentTypeClaudeCode
	AgentCodex       = agent.AgentTypeCodex
	AgentGemini      = agent.AgentTypeGemini
	AgentAntigravity = agent.AgentTypeAntigravity
	AgentCursor      = agent.AgentTypeCursor
	AgentWindsurf    = agent.AgentTypeWindsurf
	AgentAider       = agent.AgentTypeAider
	AgentOpencode    = agent.AgentTypeOpencode
	AgentOllama      = agent.AgentTypeOllama
	AgentUser        = agent.AgentTypeUser
	AgentUnknown     = agent.AgentTypeUnknown
)

// Session is the NTM-shaped session view backed by a Herdr workspace.
//
// Name is the NTM session label (workspace label). Directory is the cwd of the
// root/first pane when known. Windows maps roughly to Herdr tab_count.
type Session struct {
	Name      string
	Directory string
	Windows   int
	Panes     []Pane
	Attached  bool
	Created   string

	// Herdr-specific fields (ignored by tmux-shaped callers).
	WorkspaceID string
	Label       string
}

// Pane is the NTM-shaped pane view backed by a Herdr pane.
//
// ID is the Herdr public pane id (e.g. "w1:p2"). Index/NTMIndex preserve the
// NTM logical numbering used by send/assign filters. Title is synthesized from
// registry metadata when present so existing title parsers keep working.
type Pane struct {
	ID          string
	Index       int
	WindowIndex int
	NTMIndex    int
	Title       string
	Type        AgentType
	Variant     string
	Tags        []string
	Command     string
	Width       int
	Height      int
	Active      bool
	PID         int

	// Herdr-specific fields.
	WorkspaceID string
	TabID       string
	TerminalID  string
	Cwd         string
	AgentStatus string
	Label       string
}

// PaneMeta is durable adapter metadata for one Herdr pane.
// Stored under the session registry so GetPanes can reconstruct NTM fields
// without relying on tmux pane titles.
type PaneMeta struct {
	PaneID      string    `json:"pane_id"`
	Session     string    `json:"session"`
	WorkspaceID string    `json:"workspace_id"`
	TabID       string    `json:"tab_id,omitempty"`
	TerminalID  string    `json:"terminal_id,omitempty"`
	AgentType   AgentType `json:"agent_type"`
	NTMIndex    int       `json:"ntm_index"`
	Variant     string    `json:"variant,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Title       string    `json:"title,omitempty"`
	Command     string    `json:"command,omitempty"`
	Cwd         string    `json:"cwd,omitempty"`
	CreatedAt   string    `json:"created_at,omitempty"`
}

// SessionRecord maps an NTM session name onto a Herdr workspace.
type SessionRecord struct {
	Name        string              `json:"name"`
	WorkspaceID string              `json:"workspace_id"`
	Directory   string              `json:"directory,omitempty"`
	RootPaneID  string              `json:"root_pane_id,omitempty"`
	RootTabID   string              `json:"root_tab_id,omitempty"`
	Panes       map[string]PaneMeta `json:"panes"` // pane_id → meta
	UpdatedAt   string              `json:"updated_at,omitempty"`
}

// RegistryFile is the on-disk root document.
type RegistryFile struct {
	Version  int                      `json:"version"`
	Sessions map[string]SessionRecord `json:"sessions"` // session name → record
}
