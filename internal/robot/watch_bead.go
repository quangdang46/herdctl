// Package robot provides machine-readable output for AI agents.
// watch_bead.go implements the --robot-watch-bead command.
package robot

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
)

// WatchBeadOptions configures the --robot-watch-bead operation.
type WatchBeadOptions struct {
	Session     string
	BeadID      string
	PaneIndices []int
	Lines       int
	Interval    time.Duration
}

// BeadMention describes a bead mention found in pane output.
type BeadMention struct {
	Pane      int       `json:"pane"`
	AgentType string    `json:"agent_type"`
	Line      string    `json:"line"`
	LineNum   int       `json:"line_num"`
	Timestamp time.Time `json:"timestamp"`
}

// WatchBeadOutput is the response for --robot-watch-bead=SESSION.
type WatchBeadOutput struct {
	RobotResponse
	Session      string        `json:"session"`
	BeadID       string        `json:"bead_id"`
	CheckedAt    time.Time     `json:"checked_at"`
	Interval     string        `json:"interval"`
	PanesScanned int           `json:"panes_scanned"`
	Mentions     []BeadMention `json:"mentions"`
	BeadStatus   string        `json:"bead_status"`
	StatusError  string        `json:"status_error,omitempty"`
}

type beadMentionMatch struct {
	Line    string
	LineNum int
}

// PrintWatchBead prints a bead mention snapshot and current bead status.
// This is a thin wrapper around GetWatchBead() for CLI output.
func PrintWatchBead(opts WatchBeadOptions) error {
	output, err := GetWatchBead(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// GetWatchBead captures recent pane output and returns bead mention matches.
func GetWatchBead(opts WatchBeadOptions) (*WatchBeadOutput, error) {
	if opts.Lines <= 0 {
		opts.Lines = 200
	}
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}

	output := &WatchBeadOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		BeadID:        strings.TrimSpace(opts.BeadID),
		CheckedAt:     time.Now().UTC(),
		Interval:      opts.Interval.String(),
		Mentions:      []BeadMention{},
		BeadStatus:    "unknown",
	}

	if output.BeadID == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("bead ID is required"),
			ErrCodeInvalidFlag,
			"Provide --bead=<id> with --robot-watch-bead",
		)
		return output, nil
	}
	if !backendSessionExists(opts.Session) {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session '%s' not found", opts.Session),
			ErrCodeSessionNotFound,
			"Use 'herdctl list' to see available sessions",
		)
		return output, nil
	}

	mentionRE, err := compileBeadMentionPattern(output.BeadID)
	if err != nil {
		output.RobotResponse = NewErrorResponse(
			err,
			ErrCodeInvalidFlag,
			"Use a non-empty bead identifier",
		)
		return output, nil
	}

	panes, err := backendGetPanes(opts.Session)
	if err != nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("failed to list panes: %w", err),
			ErrCodeInternalError,
			"Check tmux session state",
		)
		return output, nil
	}

	filter := make(map[int]bool, len(opts.PaneIndices))
	for _, idx := range opts.PaneIndices {
		filter[idx] = true
	}

	for _, pane := range panes {
		if len(filter) > 0 && !filter[pane.Index] {
			continue
		}

		agentType := detectAgentTypeFromPane(pane)
		if agentType == "user" {
			continue
		}

		captured, captureErr := backendCapturePaneOutput(pane.ID, opts.Lines)
		if captureErr != nil {
			continue
		}

		lines := splitLines(stripANSI(captured))
		matches := findBeadMentionMatches(lines, mentionRE)
		for _, match := range matches {
			output.Mentions = append(output.Mentions, BeadMention{
				Pane:      pane.Index,
				AgentType: agentType,
				Line:      match.Line,
				LineNum:   match.LineNum,
				Timestamp: output.CheckedAt,
			})
		}
		output.PanesScanned++
	}

	sort.Slice(output.Mentions, func(i, j int) bool {
		if output.Mentions[i].Pane != output.Mentions[j].Pane {
			return output.Mentions[i].Pane < output.Mentions[j].Pane
		}
		return output.Mentions[i].LineNum < output.Mentions[j].LineNum
	})

	status, statusErr := bv.GetBeadStatus("", output.BeadID)
	if statusErr != nil {
		output.StatusError = statusErr.Error()
	} else {
		output.BeadStatus = status
	}

	return output, nil
}

func compileBeadMentionPattern(beadID string) (*regexp.Regexp, error) {
	id := strings.TrimSpace(beadID)
	if id == "" {
		return nil, fmt.Errorf("bead ID is required")
	}
	return regexp.Compile(`(?i)\b` + regexp.QuoteMeta(id) + `\b`)
}

func findBeadMentionMatches(lines []string, mentionRE *regexp.Regexp) []beadMentionMatch {
	matches := make([]beadMentionMatch, 0)
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if mentionRE.MatchString(trimmed) {
			matches = append(matches, beadMentionMatch{
				Line:    trimmed,
				LineNum: idx + 1,
			})
		}
	}
	return matches
}
