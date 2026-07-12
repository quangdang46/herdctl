// Package backend selects the session substrate for NTM/herdctl.
//
// Default remains tmux so existing behavior is unchanged. Opt into Herdr with:
//
//	export NTM_BACKEND=herdr
//	# aliases also accepted:
//	export HERDCTL_BACKEND=herdr
//	export NTM_MUX=herdr
//
// Values: "tmux" (default), "herdr".
//
// Do not delete or modify internal/tmux until Herdr parity is complete.
package backend

import (
	"os"
	"strings"
)

// Name identifies a session backend.
type Name string

const (
	// Tmux is the historical NTM backend (default).
	Tmux Name = "tmux"
	// Herdr is the experimental Herdr workspace/pane backend.
	Herdr Name = "herdr"
)

// Current returns the configured backend. Unknown values fall back to tmux
// so misconfiguration never silently breaks the default path.
func Current() Name {
	for _, key := range []string{"NTM_BACKEND", "HERDCTL_BACKEND", "NTM_MUX"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		switch strings.ToLower(raw) {
		case "herdr", "herd", "h":
			return Herdr
		case "tmux", "t":
			return Tmux
		}
	}
	return Tmux
}

// IsHerdr reports whether the Herdr backend is selected.
func IsHerdr() bool { return Current() == Herdr }

// IsTmux reports whether the tmux backend is selected (default).
func IsTmux() bool { return Current() == Tmux }

// String returns the backend name.
func (n Name) String() string { return string(n) }
