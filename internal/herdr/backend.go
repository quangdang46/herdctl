package herdr

import "context"

// Backend is the subset of the tmux package surface required for core
// orchestration (spawn / send / status / kill). Both a future tmux-adapter
// wrapper and this herdr package can satisfy it once call sites switch.
//
// Until wiring lands, production code continues to call internal/tmux directly.
// This interface exists so the migration path is explicit and testable.
type Backend interface {
	IsInstalled() bool
	EnsureInstalled() error

	ValidateSessionName(name string) error
	SessionExists(name string) bool
	ListSessions() ([]Session, error)
	CreateSession(name, directory string) error
	KillSession(session string) error
	GetSession(name string) (*Session, error)
	GetCurrentSession() string

	GetPanes(session string) ([]Pane, error)
	GetPanesContext(ctx context.Context, session string) ([]Pane, error)
	KillPane(paneID string) error
	SetPaneTitle(paneID, title string) error

	SplitWindow(session, directory string) (string, error)
	StartAgent(ctx context.Context, opts StartAgentOptions) (Pane, error)

	SendKeys(target, keys string, enter bool) error
	SendKeysWithDelay(target, keys string, enter bool, enterDelay interface{ /* time.Duration */ }) error
	SendInterrupt(target string) error
	CapturePaneOutput(target string, lines int) (string, error)
	CapturePaneOutputContext(ctx context.Context, target string, lines int) (string, error)

	ZoomPane(session string, paneIndex int) error
	ApplyTiledLayout(session string) error
	AttachOrSwitch(session string) error
}

// Compile-time check that *Client exposes the core methods used above.
// (SendKeysWithDelay signature uses time.Duration in the real client; the
// interface above is documentation-oriented and not asserted via var _.)
var (
	_ = (*Client).SessionExists
	_ = (*Client).CreateSession
	_ = (*Client).GetPanes
	_ = (*Client).SendKeys
	_ = (*Client).CapturePaneOutput
	_ = (*Client).StartAgent
	_ = (*Client).KillSession
)
