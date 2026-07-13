package session

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

type ResolveExplicitSessionNameErrorKind string

const (
	ResolveExplicitSessionNameErrorNoSessions ResolveExplicitSessionNameErrorKind = "no_sessions"
	ResolveExplicitSessionNameErrorNotFound   ResolveExplicitSessionNameErrorKind = "not_found"
	ResolveExplicitSessionNameErrorAmbiguous  ResolveExplicitSessionNameErrorKind = "ambiguous"
)

type ResolveExplicitSessionNameError struct {
	Kind      ResolveExplicitSessionNameErrorKind
	Input     string
	Matches   []string // ambiguous
	Available []string // not_found
}

func (e *ResolveExplicitSessionNameError) Error() string {
	switch e.Kind {
	case ResolveExplicitSessionNameErrorNoSessions:
		return fmt.Sprintf("session %q not found (no sessions running)", e.Input)
	case ResolveExplicitSessionNameErrorAmbiguous:
		return fmt.Sprintf(
			"session %q matches multiple sessions: %s (please be more specific)",
			e.Input,
			strings.Join(e.Matches, ", "),
		)
	default:
		return fmt.Sprintf("session %q not found (available: %s)", e.Input, strings.Join(e.Available, ", "))
	}
}

func ResolveExplicitSessionName(input string, sessions []tmux.Session, allowPrefix bool) (string, string, error) {
	names := sessionNames(sessions)
	if len(names) == 0 {
		return "", "", &ResolveExplicitSessionNameError{
			Kind:  ResolveExplicitSessionNameErrorNoSessions,
			Input: input,
		}
	}

	for _, name := range names {
		if name == input {
			return name, "exact match", nil
		}
	}

	if !allowPrefix {
		return "", "", &ResolveExplicitSessionNameError{
			Kind:      ResolveExplicitSessionNameErrorNotFound,
			Input:     input,
			Available: names,
		}
	}

	var matches []string
	for _, name := range names {
		if strings.HasPrefix(name, input) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)

	if len(matches) == 1 {
		return matches[0], "prefix match", nil
	}

	if len(matches) > 1 {
		return "", "", &ResolveExplicitSessionNameError{
			Kind:    ResolveExplicitSessionNameErrorAmbiguous,
			Input:   input,
			Matches: matches,
		}
	}

	return "", "", &ResolveExplicitSessionNameError{
		Kind:      ResolveExplicitSessionNameErrorNotFound,
		Input:     input,
		Available: names,
	}
}

func sessionNames(sessions []tmux.Session) []string {
	names := make([]string, 0, len(sessions))
	for _, s := range sessions {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	return names
}
