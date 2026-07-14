package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/kernel"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// SessionViewInput is the kernel input for sessions.view.
type SessionViewInput struct {
	Session string `json:"session"`
}

func init() {
	kernel.MustRegister(kernel.Command{
		Name:        "sessions.view",
		Description: "View all panes (tmux: tile; herdr: unzoom + TUI guidance)",
		Category:    "sessions",
		Input: &kernel.SchemaRef{
			Name: "SessionViewInput",
			Ref:  "cli.SessionViewInput",
		},
		Output: &kernel.SchemaRef{
			Name: "SuccessResponse",
			Ref:  "output.SuccessResponse",
		},
		REST: &kernel.RESTBinding{
			Method: "POST",
			Path:   "/sessions/{sessionId}/view",
		},
		Examples: []kernel.Example{
			{
				Name:        "view",
				Description: "View all panes (tmux: tile; herdr: unzoom + TUI guidance)",
				Command:     "herdctl view myproject",
			},
		},
		SafetyLevel: kernel.SafetySafe,
		Idempotent:  true,
	})
	kernel.MustRegisterHandler("sessions.view", func(ctx context.Context, input any) (any, error) {
		opts := SessionViewInput{}
		switch value := input.(type) {
		case SessionViewInput:
			opts = value
		case *SessionViewInput:
			if value != nil {
				opts = *value
			}
		}
		if strings.TrimSpace(opts.Session) == "" {
			return nil, fmt.Errorf("session is required")
		}
		return buildViewResponse(opts.Session)
	})
}

func newViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "view [session-name]",
		Aliases: []string{"v", "tile"},
		Short:   "View all panes (tmux: tile+attach; herdr: unzoom+TUI guidance)",
		Long: `View all panes in a session.

tmux backend:
  1. Unzoom any zoomed panes
  2. Apply tiled layout to all windows (select-layout tiled)
  3. Attach/switch to the session

herdr backend:
  1. Unzoom all panes (pane zoom --off) — best-effort; already-unzoomed is OK
  2. Print honest no-tiled guidance (herdr has no select-layout tiled)
  3. Print herdr TUI attach guidance (exit 0; attach is client-side)

If no session is specified:
- If inside tmux/herdr, operates on the current session
- Otherwise, shows a session selector

Examples:
  herdctl view myproject
  herdctl view                 # Select session or use current
  herdctl tile myproject       # Alias
  NTM_BACKEND=herdr herdctl view myproject`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var session string
			if len(args) > 0 {
				session = args[0]
			}
			return runView(cmd.OutOrStdout(), session)
		},
	}

	cmd.ValidArgsFunction = completeSessionArgs

	return cmd
}

func runView(w io.Writer, session string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}

	t := theme.Current()

	res, err := ResolveSession(session, w)
	if err != nil {
		return err
	}
	if res.Session == "" {
		return nil
	}
	res.ExplainIfInferred(os.Stderr)
	session = res.Session

	if !muxSessionExists(session) {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(fmt.Sprintf("session '%s' not found", session)))
		}
		cliErr := output.SessionNotFoundError(session)
		output.PrintCLIError(cliErr)
		return cliErr
	}

	// Herdr: unzoom all panes (no select-layout tiled), then print attach guidance.
	// Do not call tmux.AttachOrSwitch or fail closed on missing tile API.
	if backend.IsHerdr() {
		unzoomErr := muxUnzoomAllPanes(session)
		msg := fmt.Sprintf("unzoomed panes in '%s' (herdr has no select-layout tiled; open herdr TUI to rearrange)", session)
		if unzoomErr != nil {
			msg = fmt.Sprintf("view '%s': unzoom attempted with errors: %v; open herdr TUI to tile/focus panes", session, unzoomErr)
		}
		if IsJSONOutput() {
			if unzoomErr != nil {
				// Partial success: session exists, layout API incomplete.
				return output.PrintJSON(output.NewSuccess(msg))
			}
			return output.PrintJSON(output.NewSuccess(msg))
		}
		if unzoomErr != nil {
			fmt.Printf("%s~%s %s\n", colorize(t.Warning), colorize(t.Text), msg)
		} else {
			fmt.Printf("%s✓%s Unzoomed panes in '%s'\n",
				colorize(t.Success), colorize(t.Text), session)
			fmt.Printf("%s  herdr has no select-layout tiled — open the herdr TUI to rearrange panes%s\n",
				colorize(t.Subtext), colorize(t.Text))
		}
		return printHerdrAttachGuidance(session)
	}

	result, err := kernel.Run(context.Background(), "sessions.view", SessionViewInput{Session: session})
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}

	if IsJSONOutput() {
		resp, err := coerceSuccessResponse(result, "sessions.view")
		if err != nil {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return output.PrintJSON(resp)
	}

	fmt.Printf("%s✓%s Tiled layout applied to '%s'\n",
		colorize(t.Success), colorize(t.Text), session)

	// Attach or switch to session
	return tmux.AttachOrSwitch(session)
}

func coerceSuccessResponse(result any, command string) (output.SuccessResponse, error) {
	switch value := result.(type) {
	case output.SuccessResponse:
		return value, nil
	case *output.SuccessResponse:
		if value != nil {
			return *value, nil
		}
		return output.SuccessResponse{}, fmt.Errorf("%s returned nil response", command)
	default:
		return output.SuccessResponse{}, fmt.Errorf("%s returned unexpected type %T", command, result)
	}
}

func buildViewResponse(session string) (output.SuccessResponse, error) {
	if err := muxEnsureInstalled(); err != nil {
		return output.SuccessResponse{}, err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return output.SuccessResponse{}, err
	}
	resolvedSession, err := normalizeExplicitLiveSessionName(session, true)
	if err != nil {
		return output.SuccessResponse{}, err
	}
	session = resolvedSession
	if !muxSessionExists(session) {
		return output.SuccessResponse{}, fmt.Errorf("session '%s' not found", session)
	}
	if backend.IsHerdr() {
		// Unzoom is the herdr-native substitute for "view all panes".
		// ApplyTiledLayout always returns ErrNotSupported after best-effort unzoom.
		if err := muxUnzoomAllPanes(session); err != nil {
			return output.NewSuccess(fmt.Sprintf(
				"view '%s': unzoom attempted with errors: %v; open herdr TUI to tile panes",
				session, err,
			)), nil
		}
		return output.NewSuccess(fmt.Sprintf(
			"unzoomed panes in '%s' (herdr has no select-layout tiled; open herdr TUI to rearrange)",
			session,
		)), nil
	}
	if err := muxApplyTiledLayout(session); err != nil {
		// Herdr path handled above; keep this for any future dual-backend edge.
		if backend.IsHerdr() && errors.Is(err, herdr.ErrNotSupported) {
			return output.NewSuccess(fmt.Sprintf(
				"unzoomed panes in '%s' (herdr has no select-layout tiled; open herdr TUI to rearrange)",
				session,
			)), nil
		}
		return output.SuccessResponse{}, fmt.Errorf("failed to apply layout: %w", err)
	}
	return output.NewSuccess(fmt.Sprintf("tiled layout applied to '%s'", session)), nil
}
