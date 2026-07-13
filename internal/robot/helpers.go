package robot

import (
	"os"

	"github.com/Dicklesworthstone/ntm/internal/session"
)

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// resolveSessionName resolves a session name against live tmux sessions.
// It tries exact match first, then prefix match. If no match is found, the
// original name is returned unchanged so callers preserve their existing
// "session not found" behavior.
//
// This mirrors the resolution logic in cli.normalizeExplicitLiveSessionName
// so that robot surface functions (GetTail, GetActivity, etc.) handle labeled
// session names (e.g. "myproject--frontend") consistently with list/status,
// even when called from the REST API or other non-CLI entry points.
func resolveSessionName(name string) string {
	sessions, err := backendListSessions()
	if err != nil || len(sessions) == 0 {
		return name
	}
	resolved, _, err := session.ResolveExplicitSessionName(name, sessions, true)
	if err != nil {
		return name
	}
	return resolved
}

// capitalize returns the string with its first letter uppercased.
// For simple ASCII strings; use golang.org/x/text/cases for full Unicode support.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	// For ASCII lowercase letters, convert to uppercase
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
