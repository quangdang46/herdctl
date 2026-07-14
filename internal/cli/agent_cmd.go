package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

// newAgentCmd is the parent for live agent operations that map onto herdr
// agent.* (and tmux pane equivalents where applicable):
//
//	herdctl agent list [--session NAME]
//	herdctl agent get <target>
//	herdctl agent read <target> [--lines N] [--source recent|visible]
//	herdctl agent send <target> <text>
//	herdctl agent rename <target> <name>|--clear
//	herdctl agent focus <target>
//	herdctl agent wait <target> --status idle|working|blocked|done|unknown [--timeout MS]
//	herdctl agent explain <target>
//
// Distinct from `herdctl agents` (capability profiles — file/config based, backend-agnostic).
func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Live agent list/get/read/send/rename/focus/wait/explain",
		Long: `Operate on live agents (tmux panes or herdr agent identities).

Subcommands:
  list      List agents (optionally filtered to a session)
  get       Show agent details
  read      Capture recent/visible agent output
  send      Send text to an agent (herdr: agent send; tmux: send-keys)
  rename    Rename an agent (herdr: agent rename; tmux: pane title)
  focus     Focus an agent pane
  wait      Wait until agent reaches a status (herdr-native; tmux polls)
  explain   Detection explain (herdr-only)

Targets accept agent names (e.g. demo-cc_1), pane ids (wN:pM / %N), or titles.

Examples:
  herdctl agent list
  herdctl agent list --session demo
  herdctl agent get demo-cc_1
  herdctl agent read demo-cc_1 --lines 30
  herdctl agent send demo-cc_1 "summarize blockers"
  herdctl agent rename demo-cc_1 lead-cc
  herdctl agent focus demo-cc_1
  herdctl agent wait demo-cc_1 --status idle --timeout 30000
  NTM_BACKEND=herdr herdctl agent explain demo-cc_1

See also: herdctl agents (capability profiles).`,
	}
	cmd.AddCommand(newAgentListCmd())
	cmd.AddCommand(newAgentGetCmd())
	cmd.AddCommand(newAgentReadCmd())
	cmd.AddCommand(newAgentSendCmd())
	cmd.AddCommand(newAgentRenameCmd())
	cmd.AddCommand(newAgentFocusCmd())
	cmd.AddCommand(newAgentWaitCmd())
	cmd.AddCommand(newAgentExplainCmd())
	return cmd
}

func newAgentListCmd() *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "l"},
		Short:   "List live agents",
		Long: `List live agents known to the active backend.

herdr: herdr agent list (optionally filtered by --session via workspace/name).
tmux:  synthesizes rows from pane titles in --session (required).

Examples:
  herdctl agent list
  herdctl agent list --session demo
  herdctl agent list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentList(cmd.OutOrStdout(), session)
		},
	}
	cmd.Flags().StringVarP(&session, "session", "s", "", "filter agents to this session")
	return cmd
}

func newAgentGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <target>",
		Short: "Show details for one agent",
		Long: `Show details for one agent by name, pane id, or title.

Examples:
  herdctl agent get demo-cc_1
  herdctl agent get wM:p2
  herdctl agent get demo-cc_1 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentGet(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

func newAgentReadCmd() *cobra.Command {
	var lines int
	var source string
	cmd := &cobra.Command{
		Use:   "read <target>",
		Short: "Capture agent output",
		Long: `Capture recent or visible text from an agent pane.

Sources:
  recent             recent scrollback (default)
  visible            currently visible viewport
  recent-unwrapped   herdr-only unwrapped recent lines

Examples:
  herdctl agent read demo-cc_1
  herdctl agent read demo-cc_1 --lines 80
  herdctl agent read demo-cc_1 --source visible`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentRead(cmd.OutOrStdout(), args[0], lines, source)
		},
	}
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "number of recent lines")
	cmd.Flags().StringVar(&source, "source", "recent", "recent|visible|recent-unwrapped")
	return cmd
}

func newAgentSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <target> <text>",
		Short: "Send text to an agent",
		Long: `Send text to an agent.

herdr: uses herdr agent send (literal text; no automatic Enter).
tmux:  uses send-keys with Enter.

Examples:
  herdctl agent send demo-cc_1 "status update"
  herdctl agent send wM:p2 "continue"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentSend(cmd.OutOrStdout(), args[0], args[1])
		},
	}
	return cmd
}

func newAgentRenameCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "rename <target> [name]",
		Short: "Rename an agent",
		Long: `Rename a live agent.

herdr: herdr agent rename <target> <name>|--clear
tmux:  sets the pane title

Examples:
  herdctl agent rename demo-cc_1 lead-cc
  herdctl agent rename demo-cc_1 --clear`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 2 {
				name = args[1]
			}
			if !clear && strings.TrimSpace(name) == "" {
				return fmt.Errorf("rename requires a new name or --clear")
			}
			return runAgentRename(cmd.OutOrStdout(), args[0], name, clear)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the agent name / title")
	return cmd
}

func newAgentFocusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "focus <target>",
		Short: "Focus an agent pane",
		Long: `Focus an agent pane in the active backend.

herdr: herdr agent focus
tmux:  select-pane -t <id>

Examples:
  herdctl agent focus demo-cc_1
  herdctl agent focus wM:p2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentFocus(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

func newAgentWaitCmd() *cobra.Command {
	var status string
	var timeoutMS int
	cmd := &cobra.Command{
		Use:   "wait <target>",
		Short: "Wait until an agent reaches a status",
		Long: `Block until the target agent reports the desired status.

Statuses: idle, working, blocked, done, unknown

herdr: herdr agent wait (native).
tmux:  polls capture/classification via muxGetAgentStatus fallback.

Examples:
  herdctl agent wait demo-cc_1 --status idle
  herdctl agent wait demo-cc_1 --status working --timeout 60000`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentWait(cmd.OutOrStdout(), args[0], status, timeoutMS)
		},
	}
	cmd.Flags().StringVar(&status, "status", "idle", "desired status: idle|working|blocked|done|unknown")
	cmd.Flags().IntVar(&timeoutMS, "timeout", 30000, "timeout in milliseconds (<=0 uses backend default on herdr)")
	return cmd
}

func newAgentExplainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <target>",
		Short: "Explain agent status detection (herdr)",
		Long: `Print herdr detection-rule evidence for why an agent is idle/working/blocked.

This is herdr-only (detection manifests live in herdr). On tmux the command
returns a clear error.

Examples:
  NTM_BACKEND=herdr herdctl agent explain demo-cc_1
  NTM_BACKEND=herdr herdctl agent explain demo-cc_1 --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentExplain(cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// runners
// ---------------------------------------------------------------------------

func runAgentList(w io.Writer, session string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	// On tmux, if session empty try resolve current / require flag.
	if !backend.IsHerdr() && strings.TrimSpace(session) == "" {
		if cur := muxGetCurrentSession(); cur != "" {
			session = cur
		} else {
			return fmt.Errorf("tmux backend: agent list requires --session (or run inside a tmux session)")
		}
	}
	agents, err := muxListAgents(session)
	if err != nil {
		return err
	}
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].WorkspaceID != agents[j].WorkspaceID {
			return agents[i].WorkspaceID < agents[j].WorkspaceID
		}
		return agents[i].Name < agents[j].Name
	})
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"backend": muxBackendLabel(),
			"session": session,
			"count":   len(agents),
			"agents":  agents,
		})
	}
	if len(agents) == 0 {
		fmt.Fprintln(w, "No agents found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tAGENT\tSTATUS\tFOCUSED\tPANE\tCWD")
	fmt.Fprintln(tw, "----\t-----\t------\t-------\t----\t---")
	for _, a := range agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%s\t%s\n",
			a.Name, a.Agent, emptyDash(a.AgentStatus), a.Focused, a.PaneID, a.Cwd)
	}
	return tw.Flush()
}

func runAgentGet(w io.Writer, target string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	ag, err := muxGetAgent(target)
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"backend": muxBackendLabel(),
			"agent":   ag,
		})
	}
	fmt.Fprintf(w, "Name:     %s\n", ag.Name)
	fmt.Fprintf(w, "Agent:    %s\n", emptyDash(ag.Agent))
	fmt.Fprintf(w, "Status:   %s\n", emptyDash(ag.AgentStatus))
	fmt.Fprintf(w, "Focused:  %v\n", ag.Focused)
	fmt.Fprintf(w, "Pane:     %s\n", emptyDash(ag.PaneID))
	fmt.Fprintf(w, "Workspace:%s\n", emptyDash(ag.WorkspaceID))
	fmt.Fprintf(w, "Tab:      %s\n", emptyDash(ag.TabID))
	fmt.Fprintf(w, "Terminal: %s\n", emptyDash(ag.TerminalID))
	fmt.Fprintf(w, "Cwd:      %s\n", emptyDash(ag.Cwd))
	return nil
}

func runAgentRead(w io.Writer, target string, lines int, source string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	text, err := muxReadAgent(target, lines, source)
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"backend": muxBackendLabel(),
			"target":  target,
			"source":  source,
			"lines":   lines,
			"text":    text,
		})
	}
	fmt.Fprint(w, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(w)
	}
	return nil
}

func runAgentSend(w io.Writer, target, text string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	if err := muxAgentSend(target, text); err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"backend": muxBackendLabel(),
			"target":  target,
		})
	}
	fmt.Fprintf(w, "sent to %s\n", target)
	return nil
}

func runAgentRename(w io.Writer, target, name string, clear bool) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	ag, err := muxRenameAgent(target, name, clear)
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"backend": muxBackendLabel(),
			"agent":   ag,
		})
	}
	if clear {
		fmt.Fprintf(w, "cleared name for %s (pane %s)\n", target, ag.PaneID)
	} else {
		fmt.Fprintf(w, "renamed %s → %s (pane %s)\n", target, ag.Name, ag.PaneID)
	}
	return nil
}

func runAgentFocus(w io.Writer, target string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	ag, err := muxFocusAgent(target)
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"success": true,
			"backend": muxBackendLabel(),
			"agent":   ag,
		})
	}
	fmt.Fprintf(w, "focused %s (pane %s)\n", emptyDash(ag.Name), ag.PaneID)
	return nil
}

func runAgentWait(w io.Writer, target, status string, timeoutMS int) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case herdr.AgentStatusIdle, herdr.AgentStatusWorking, herdr.AgentStatusBlocked,
		herdr.AgentStatusDone, herdr.AgentStatusUnknown:
		// ok
	default:
		return fmt.Errorf("invalid status %q: must be idle|working|blocked|done|unknown", status)
	}

	start := time.Now()
	if backend.IsHerdr() {
		if err := muxWaitAgentStatus(target, status, timeoutMS); err != nil {
			if IsJSONOutput() {
				return output.PrintJSON(output.NewError(err.Error()))
			}
			return err
		}
	} else {
		// tmux poll fallback via capture status is not native; use short poll
		// of muxGetAgentStatus (always empty on tmux) → fail with guidance.
		if err := muxWaitAgentStatus(target, status, timeoutMS); err != nil {
			// Fall back to herdctl wait-style guidance.
			return fmt.Errorf("%w; use `herdctl wait <session> --until=%s` for tmux polling", err, status)
		}
	}
	elapsed := time.Since(start)
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"success":    true,
			"backend":    muxBackendLabel(),
			"target":     target,
			"status":     status,
			"waited_ms":  elapsed.Milliseconds(),
			"timeout_ms": timeoutMS,
		})
	}
	fmt.Fprintf(w, "agent %s is %s (waited %s)\n", target, status, elapsed.Round(time.Millisecond))
	return nil
}

func runAgentExplain(w io.Writer, target string) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}
	data, err := muxExplainAgent(target)
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}
	// Always JSON-shaped evidence; pretty-print for humans unless --json
	// (IsJSONOutput already wants compact/stable JSON via output.PrintJSON).
	if IsJSONOutput() {
		return output.PrintJSON(map[string]any{
			"backend": muxBackendLabel(),
			"target":  target,
			"explain": data,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	return nil
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
