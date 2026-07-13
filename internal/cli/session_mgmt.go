package cli

import (
	"github.com/spf13/cobra"
)

// newSessionCmd is the parent for live-session lifecycle helpers that map cleanly
// onto both tmux and herdr:
//
//	ntm session list              — list live sessions (muxListSessions)
//	ntm session stop <name>       — close workspace / kill session (muxKillSession)
//	ntm session delete <name>     — stop + drop herdr registry binding
//
// These are thin aliases over list/kill so FEATURES rows stay honest without
// inventing a second kill path. Never shells out to raw tmux when NTM_BACKEND=herdr.
func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Live session list/stop/delete (tmux or herdr)",
		Long: `Manage live sessions (not saved snapshots — see "ntm sessions" for those).

  ntm session list                 List running sessions
  ntm session stop <name>          Stop a session (tmux kill-session / herdr workspace close)
  ntm session delete <name>        Stop and remove registry binding (herdr) or kill (tmux)

On NTM_BACKEND=herdr, stop and delete both go through muxKillSession which closes
the herdr workspace and drops the ~/.ntm/herdr/registry.json binding. No raw tmux.`,
	}
	cmd.AddCommand(newSessionListCmd())
	cmd.AddCommand(newSessionStopCmd())
	cmd.AddCommand(newSessionDeleteCmd())
	return cmd
}

func newSessionListCmd() *cobra.Command {
	var tags []string
	var project string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "l"},
		Short:   "List live sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(tags, project)
		},
	}
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "filter sessions by agent tag")
	cmd.Flags().StringVarP(&project, "project", "p", "", "filter by base project name")
	return cmd
}

func newSessionStopCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "stop <session>",
		Short: "Stop a live session (herdr: workspace close)",
		Long: `Stop a live session.

tmux: kill-session (with confirmation unless --force).
herdr: workspace close + registry drop via muxKillSession.

Examples:
  ntm session stop myproject
  ntm session stop myproject --force
  NTM_BACKEND=herdr ntm session stop myproject --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKill(cmd.OutOrStdout(), args[0], force, nil, false, false)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

func newSessionDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <session>",
		Short: "Delete a live session (herdr: close + registry delete)",
		Long: `Delete a live session.

Equivalent to stop for runtime teardown. On herdr this always closes the
workspace and deletes the registry binding (muxKillSession). Use --force to
skip confirmation.

Examples:
  ntm session delete myproject --force
  NTM_BACKEND=herdr ntm session delete myproject --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKill(cmd.OutOrStdout(), args[0], force, nil, false, false)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}
