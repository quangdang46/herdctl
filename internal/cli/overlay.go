package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

func newOverlayCmd() *cobra.Command {
	var overlayKey string
	var attentionCursor int64

	cmd := &cobra.Command{
		Use:   "overlay [session-name]",
		Short: "Open dashboard as a floating overlay above agent panes",
		Long: `Open the NTM dashboard in a tmux popup that floats over your agent panes.

The overlay lets you monitor agents without leaving their terminal output.
Press Escape to dismiss the overlay and interact with panes directly.
Press Enter/z on a pane to dismiss the overlay AND zoom into that pane.

Use 'ntm bind --overlay' to set up F12 as a toggle key.

If no session is specified:
- Inside tmux: uses the current session
- Outside tmux: shows an error (overlay requires tmux)

Examples:
  ntm overlay myproject     # Open dashboard overlay for myproject
  ntm overlay               # Auto-detect session (must be inside tmux)
  ntm bind --overlay        # Set up F12 toggle key`,
		Aliases: []string{"ov", "hud"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tmux.InTmux() {
				return fmt.Errorf("overlay requires tmux — run from inside a tmux session")
			}

			var session string
			if len(args) > 0 {
				session = args[0]
			}

			res, err := ResolveSession(session, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if res.Session == "" {
				return nil
			}
			session = res.Session

			if !tmux.SessionExists(session) {
				return fmt.Errorf("session '%s' not found", session)
			}

			return launchOverlayPopup(session, overlayKey, attentionCursor, res.Inferred)
		},
	}

	cmd.Flags().StringVar(&overlayKey, "bind-key", "", "Also set up this key as a toggle (e.g., F12)")
	cmd.Flags().Int64Var(&attentionCursor, "attention-cursor", 0, "Pre-focus the overlay attention panel on this event cursor")
	cmd.ValidArgsFunction = completeSessionArgs

	return cmd
}

// launchOverlayPopup opens the NTM dashboard inside a tmux display-popup.
//
// inferred reports whether the relaunch came from a session that ntm inferred
// (e.g. plain `ntm dash` for the current tmux session, or the F12 overlay key
// which always targets #{session_name}). It is threaded into the inner command
// so the popup keeps lenient, current-session project-dir resolution instead of
// flipping to the strict explicit-session path.
func launchOverlayPopup(session, bindKey string, attentionCursor int64, inferred bool) error {
	t := theme.Current()

	// Auto-setup: if the overlay key isn't bound yet, set it up on first use.
	// Uses the explicit --bind-key if provided, otherwise defaults to F12.
	overlayKey := "F12"
	if bindKey != "" {
		overlayKey = bindKey
	}
	if !isOverlayKeyBound(overlayKey) {
		if err := setupOverlayBinding(overlayKey); err != nil {
			fmt.Fprintf(os.Stderr, "%s⚠%s Could not auto-setup %s binding: %v\n",
				colorize(t.Warning), colorize(t.Text), overlayKey, err)
		}
	}

	// Build the ntm command to run inside the popup.
	// display-popup passes the command to /bin/sh -c, so quote paths
	// to handle spaces. Tmux session names can't contain single quotes.
	ntmBin, err := os.Executable()
	if err != nil {
		ntmBin = "ntm"
	}
	innerCmd := overlayPopupInnerCommand(ntmBin, session, attentionCursor, inferred)

	// display-popup -E tears the popup down the instant the inner process
	// exits, so any error the inner ntm prints is painted and erased in the
	// same frame and the parent only ever sees a bare "exit status 1".
	// Redirect the inner stderr to a temp file so we can surface the real,
	// actionable error (e.g. "getting project root failed") on failure.
	var errFile string
	if f, ferr := os.CreateTemp("", "ntm-overlay-stderr-*.log"); ferr == nil {
		errFile = f.Name()
		_ = f.Close()
		defer os.Remove(errFile)
		innerCmd += " 2>" + shellSingleQuote(errFile)
	}

	// Launch the popup — this blocks until the popup is dismissed
	tmuxArgs := []string{
		"display-popup",
		"-E",        // close popup when command exits
		"-w", "95%", // 95% of terminal width
		"-h", "95%", // 95% of terminal height
		innerCmd,
	}

	cmd := exec.Command(tmux.BinaryPath(), tmuxArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	captured := ""
	if errFile != "" {
		if data, rerr := os.ReadFile(errFile); rerr == nil {
			captured = strings.TrimSpace(string(data))
		}
	}

	if runErr != nil {
		if captured != "" {
			// Strip the inner cobra "Error: " prefix so the top-level handler
			// doesn't print a doubled "Error: ... Error: ...".
			return fmt.Errorf("dashboard overlay failed: %s", strings.TrimPrefix(captured, "Error: "))
		}
		return fmt.Errorf("dashboard overlay failed: %w", runErr)
	}

	// On success, re-emit any inner warnings (e.g. "project directory does not
	// exist") that were redirected away from the popup so the overlay path stays
	// as informative as the non-overlay dashboard.
	if captured != "" {
		fmt.Fprintln(os.Stderr, captured)
	}
	return nil
}

func overlayPopupInnerCommand(ntmBin, session string, attentionCursor int64, inferred bool) string {
	innerCmd := fmt.Sprintf("NTM_POPUP=1 '%s' dashboard --popup", ntmBin)
	if inferred {
		// Preserve the lenient current-session resolution across the relaunch:
		// without this marker the explicit session arg appended below would
		// route project-dir resolution down the strict, fail-closed path and
		// `ntm dash` would refuse to open for any unregistered tmux session.
		innerCmd += " --inferred"
	}
	if attentionCursor > 0 {
		innerCmd += fmt.Sprintf(" --attention-cursor %d", attentionCursor)
	}
	return innerCmd + fmt.Sprintf(" '%s'", session)
}

// shellSingleQuote single-quotes s for safe use inside the /bin/sh -c command
// line that tmux display-popup runs, escaping any embedded single quotes.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
