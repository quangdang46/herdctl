package herdr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/agent"
)

// sessionNameRegex matches NTM session name rules (tmux-compatible subset).
var sessionNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// paneNameRegex matches the NTM pane title convention used across the codebase.
// session__type_index[_variant][tags]
var paneNameRegex = regexp.MustCompile(`^.+__([\w-]+)_(\d+)(?:_([A-Za-z0-9._/@:+-]+))?(?:\[([^\]]*)\])?$`)

// ValidateSessionName mirrors tmux.ValidateSessionName rules.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty session name", ErrInvalidName)
	}
	if !sessionNameRegex.MatchString(name) {
		return fmt.Errorf("%w: %q (allowed: a-z A-Z 0-9 _ -)", ErrInvalidName, name)
	}
	return nil
}

// FormatPaneName formats a pane title according to NTM convention.
// Kept identical to tmux.FormatPaneName so registry titles stay parseable.
func FormatPaneName(session string, agentType string, index int, variant string) string {
	normalizedType := strings.TrimSpace(agentType)
	if canonical := agent.AgentType(normalizedType).Canonical(); canonical.IsValid() {
		normalizedType = string(canonical)
	}
	base := fmt.Sprintf("%s__%s_%d", session, normalizedType, index)
	if variant != "" {
		return fmt.Sprintf("%s_%s", base, variant)
	}
	return base
}

// FormatTags formats tags as a bracket-enclosed string for pane titles.
func FormatTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "[" + strings.Join(tags, ",") + "]"
}

// ParseAgentFromTitle extracts agent type, index, variant, and tags from a title.
func ParseAgentFromTitle(title string) (AgentType, int, string, []string) {
	matches := paneNameRegex.FindStringSubmatch(title)
	if matches == nil {
		return AgentUser, 0, "", nil
	}
	agentType := AgentType(matches[1])
	idx, _ := strconv.Atoi(matches[2])
	variant := matches[3]
	var tags []string
	if len(matches) >= 5 {
		tags = parseTags(matches[4])
	}
	if agentType != "" {
		return agentType, idx, variant, tags
	}
	return AgentUser, 0, "", nil
}

func parseTags(tagStr string) []string {
	if tagStr == "" {
		return nil
	}
	parts := strings.Split(tagStr, ",")
	var tags []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			tags = append(tags, p)
		}
	}
	return tags
}

// NextNTMIndex returns max(existing NTMIndex)+1 for a session's panes of type t.
// If none exist, returns 1.
func NextNTMIndex(panes map[string]PaneMeta, t AgentType) int {
	max := 0
	want := string(t.Canonical())
	for _, p := range panes {
		if string(p.AgentType.Canonical()) != want {
			continue
		}
		if p.NTMIndex > max {
			max = p.NTMIndex
		}
	}
	return max + 1
}
