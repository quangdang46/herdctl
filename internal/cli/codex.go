package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/codex"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// newCodexCmd creates the "codex" command group. It hosts Codex-specific
// pane-control subcommands. The foundational subcommand is "palette-state",
// which reads and classifies the state of a Codex pane's slash-command / goal
// palette (NTM issue #166). Other Codex pane-control subcommands (#165, #167-169)
// build on this classifier.
func newCodexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codex",
		Short: "Codex agent pane-control commands",
		Long: `Commands for inspecting and controlling Codex (cod) agent panes.

Codex presents transient TUI overlays — a slash-command palette, a goal/plan
prime indicator, and modal dialogs. These commands let agents and tooling read
that overlay state before driving the pane with keystrokes.

Examples:
  ntm codex palette-state --session myproject --pane 1
  ntm codex palette-state --session myproject --pane 1 --json`,
	}

	cmd.AddCommand(newCodexPaletteStateCmd())

	return cmd
}

// PaletteStateResult is the JSON output for "ntm codex palette-state".
//
// It embeds the standard robot envelope (success/timestamp/version/error) and
// adds the classifier result plus capture provenance so agents can both trust
// and reproduce the classification.
type PaletteStateResult struct {
	robot.RobotResponse

	// State is the classified Codex pane state (see internal/codex.PaletteState).
	State string `json:"state"`

	// Session is the resolved tmux session name.
	Session string `json:"session"`

	// Pane is the pane index that was inspected.
	Pane int `json:"pane"`

	// ProvenanceHash is the sha256 (hex) of the exact captured content that was
	// classified. It lets a caller verify the classification was made over the
	// same bytes they captured.
	ProvenanceHash string `json:"provenance_hash"`

	// CapturedLines is the number of lines in the captured content.
	CapturedLines int `json:"captured_lines"`

	// MarkersMatched lists the marker substrings that selected the state.
	// Always present (empty array, not null) so agents can iterate safely.
	MarkersMatched []string `json:"markers_matched"`

	// TimestampSource documents how the response timestamp was derived. For this
	// command the classification is point-in-time over a fresh capture, so the
	// timestamp reflects the local wall clock at capture time.
	TimestampSource string `json:"timestamp_source"`
}

func newCodexPaletteStateCmd() *cobra.Command {
	var (
		session    string
		pane       int
		jsonOutput bool
		lines      int
	)

	cmd := &cobra.Command{
		Use:   "palette-state",
		Short: "Classify the state of a Codex pane's slash/goal palette",
		Long: `Capture a Codex agent pane and classify its command/goal palette state.

The pane content is captured, hashed (sha256 for provenance), and matched
against a centralized table of Codex TUI markers to produce one of:

  idle                 Quiescent Codex prompt, no overlay open
  slash_palette_open   The "/" slash-command palette is open
  goal_palette_primed  A goal/plan prompt is primed but not submitted
  dialog_open          A modal dialog (approval/confirmation) is open
  unknown              No known marker matched

This is the foundational read used by the other Codex pane-control commands.

Examples:
  ntm codex palette-state --session myproject --pane 1
  ntm codex palette-state --session myproject --pane 1 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := tmux.EnsureInstalled(); err != nil {
				return err
			}

			emitJSON := jsonOutput || IsJSONOutput()

			if session == "" {
				return robot.RobotError(
					fmt.Errorf("--session is required"),
					robot.ErrCodeInvalidFlag,
					"Pass --session <name>; use 'ntm list' to see available sessions",
				)
			}
			if pane < 0 {
				return robot.RobotError(
					fmt.Errorf("--pane must be >= 0, got %d", pane),
					robot.ErrCodeInvalidFlag,
					"Pass a non-negative pane index, e.g. --pane 1",
				)
			}

			if !tmux.SessionExists(session) {
				return robot.RobotError(
					fmt.Errorf("session '%s' not found", session),
					robot.ErrCodeSessionNotFound,
					"Use 'ntm list' to see available sessions",
				)
			}

			panes, err := tmux.GetPanes(session)
			if err != nil {
				return robot.RobotError(err, robot.ErrCodeInternalError, "Could not enumerate panes for the session")
			}

			var target *tmux.Pane
			for i := range panes {
				if panes[i].Index == pane {
					target = &panes[i]
					break
				}
			}
			if target == nil {
				return robot.RobotError(
					fmt.Errorf("pane %d not found in session '%s'", pane, session),
					robot.ErrCodePaneNotFound,
					"Use 'ntm status <session>' to see available pane indices",
				)
			}

			content, err := tmux.CapturePaneOutput(target.ID, lines)
			if err != nil {
				return robot.RobotError(err, robot.ErrCodeInternalError, "Failed to capture pane content")
			}

			// Provenance: hash the exact bytes we classify.
			sum := sha256.Sum256([]byte(content))
			provenanceHash := hex.EncodeToString(sum[:])

			classification := codex.Classify(content)

			markers := classification.MarkersMatched
			if markers == nil {
				markers = []string{}
			}

			result := PaletteStateResult{
				RobotResponse:   robot.NewRobotResponse(true),
				State:           classification.State.String(),
				Session:         session,
				Pane:            pane,
				ProvenanceHash:  provenanceHash,
				CapturedLines:   countCapturedLines(content),
				MarkersMatched:  markers,
				TimestampSource: "capture_walltime",
			}

			if emitJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Human-readable output (dual-mode, matching other commands).
			fmt.Printf("Codex Palette State\n")
			fmt.Printf("===================\n\n")
			fmt.Printf("Session: %s\n", result.Session)
			fmt.Printf("Pane:    %d\n", result.Pane)
			fmt.Printf("State:   %s\n", result.State)
			fmt.Printf("Captured: %d lines (sha256 %s)\n", result.CapturedLines, result.ProvenanceHash[:16])
			if len(result.MarkersMatched) > 0 {
				fmt.Printf("Markers: %s\n", strings.Join(result.MarkersMatched, ", "))
			} else {
				fmt.Printf("Markers: (none)\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "Target tmux session name (required)")
	cmd.Flags().IntVar(&pane, "pane", 0, "Target pane index within the session")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().IntVar(&lines, "lines", tmux.LinesFullContext, "Number of pane lines to capture for classification")

	return cmd
}

// countCapturedLines counts the lines in captured pane content, ignoring a
// single trailing newline so a final blank line is not over-counted.
func countCapturedLines(content string) int {
	if content == "" {
		return 0
	}
	trimmed := strings.TrimSuffix(content, "\n")
	return strings.Count(trimmed, "\n") + 1
}
