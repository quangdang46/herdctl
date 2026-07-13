package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/backend"
	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/herdr"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func newRespawnCmd() *cobra.Command {
	var force bool
	var panes string
	var agentType string
	var all bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:     "respawn <session>",
		Aliases: []string{"restart"},
		Short:   "Kill and restart worker agents in a session",
		Long: `Kill and restart worker agents in a session.

On tmux this uses respawn-pane -k then relaunches the agent CLI by keystroke.
On herdr (NTM_BACKEND=herdr) it kills each pane and starts a fresh agent via
agent.start using registry metadata (type/index/variant/cwd/command).

By default, only agent panes are restarted (not the user pane at index 0).
Use --all to include all panes, or --panes to target specific indices.

Examples:
  ntm respawn myproject              # Restart all agent panes (prompts for confirmation)
  ntm respawn myproject --force      # No confirmation
  ntm respawn myproject --panes=1,2  # Restart only panes 1 and 2
  ntm respawn myproject --type=cc    # Restart only Claude agents
  ntm respawn myproject --all        # Include user pane (index 0)
  ntm respawn myproject --dry-run    # Preview which panes would be restarted`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRespawn(args[0], force, panes, agentType, all, dryRun)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")
	cmd.Flags().StringVarP(&panes, "panes", "p", "", "comma-separated pane indices to restart (e.g., 1,2,3)")
	cmd.Flags().StringVarP(&agentType, "type", "t", "", "filter by agent type (cc, claude, cod, codex, gmi, gemini, agy, antigravity)")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include all panes (including user pane)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview which panes would be restarted")

	return cmd
}

func runRespawn(session string, force bool, panesFlag string, agentType string, all bool, dryRun bool) error {
	if err := muxEnsureInstalled(); err != nil {
		return err
	}
	if err := muxRequireHerdrServer(); err != nil {
		return err
	}

	res, err := ResolveSession(session, nil)
	if err != nil {
		return err
	}
	if res.Session == "" {
		return fmt.Errorf("session is required")
	}
	session = res.Session

	if !muxSessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	// Parse pane filter
	var paneFilter []string
	if panesFlag != "" {
		paneFilter = strings.Split(panesFlag, ",")
		for i := range paneFilter {
			paneFilter[i] = strings.TrimSpace(paneFilter[i])
		}
	}

	// Get panes to determine targets
	panes, err := muxGetPanes(session)
	if err != nil {
		return fmt.Errorf("failed to get panes: %w", err)
	}

	// Build filter map
	paneFilterMap := make(map[string]bool)
	for _, p := range paneFilter {
		paneFilterMap[p] = true
	}
	targetPanes := selectRespawnTargets(panes, paneFilterMap, agentType, all)

	if len(targetPanes) == 0 {
		fmt.Println("No panes matched the filter criteria.")
		return nil
	}

	// Dry-run mode
	if dryRun {
		fmt.Printf("Would restart %d pane(s) in session '%s':\n", len(targetPanes), session)
		for _, pane := range targetPanes {
			agentType := respawnPaneAgentType(pane)
			fmt.Printf("  - Pane %d: %s (%s)\n", pane.Index, pane.ID, agentType)
		}
		return nil
	}

	// Confirmation
	if !force {
		title := fmt.Sprintf("Restart %d pane(s)?", len(targetPanes))
		desc := fmt.Sprintf("Agents in session '%s' will be killed and relaunched.", session)
		if !confirmHuh(title, desc) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Herdr: kill pane + StartAgent with registry meta (no tmux respawn-pane).
	if backend.IsHerdr() {
		return runRespawnHerdr(session, targetPanes)
	}

	// Restart targets via the shared robot engine, which relaunches agent
	// CLIs after the respawn and ready-gates them (#187) — respawn-pane -k
	// alone only restores the pane's default command (the login shell).
	fmt.Printf("Restarting %d pane(s) (relaunching agent CLIs)...\n", len(targetPanes))
	out, err := robot.GetRestartPane(robot.RestartPaneOptions{
		Session: session,
		Panes:   paneFilter,
		Type:    agentType,
		All:     all,
	})
	if err != nil {
		return err
	}

	// Report results
	if len(out.Restarted) > 0 {
		fmt.Printf("Restarted panes: %s\n", strings.Join(out.Restarted, ", "))
		for _, paneKey := range out.Restarted {
			if relaunched, ok := out.AgentRelaunched[paneKey]; ok {
				status := "agent relaunched"
				if !relaunched {
					status = "agent relaunch FAILED (pane left at a shell)"
				}
				fmt.Printf("  - Pane %s: %s\n", paneKey, status)
			}
		}
	}
	if len(out.Failed) > 0 {
		fmt.Printf("Failed to restart:\n")
		for _, f := range out.Failed {
			fmt.Printf("  - %s: %s\n", f.Pane, f.Reason)
		}
		return fmt.Errorf("%d pane(s) failed to restart cleanly", len(out.Failed))
	}

	return nil
}

// runRespawnHerdr kills each target pane and starts a fresh agent via
// herdr agent.start, preserving NTM index/type/variant from registry meta.
func runRespawnHerdr(session string, targetPanes []tmux.Pane) error {
	fmt.Printf("Restarting %d pane(s) via herdr (kill + agent.start)...\n", len(targetPanes))

	dir, dirErr := resolveExplicitProjectDirForSession(session)
	if dirErr != nil {
		// Fall back to empty cwd; StartAgent will use registry session directory.
		dir = ""
	}

	cwdForCfg, _ := os.Getwd()
	cfg, cfgErr := config.LoadMerged(cwdForCfg, config.DefaultPath())
	if cfgErr != nil {
		cfg = config.Default()
	}

	var restarted []string
	var failed []string

	for _, pane := range targetPanes {
		paneKey := fmt.Sprintf("%d", pane.Index)
		if pane.ID != "" {
			paneKey = pane.ID
		}

		resolvedType := respawnPaneAgentType(pane)
		if resolvedType == "user" || resolvedType == "unknown" || resolvedType == "" {
			// No agent CLI to relaunch: just kill the pane (best-effort).
			if err := muxKillPane(pane.ID); err != nil {
				failed = append(failed, fmt.Sprintf("%s: kill: %v", paneKey, err))
				fmt.Printf("  - Pane %s: kill FAILED: %v\n", paneKey, err)
			} else {
				restarted = append(restarted, paneKey)
				fmt.Printf("  - Pane %s: killed (no agent type to relaunch)\n", paneKey)
			}
			continue
		}

		// Capture meta before kill (KillPane drops registry entry).
		agentType := AgentType(pane.Type.Canonical())
		if agentType == "" || string(agentType) == "unknown" {
			agentType = AgentType(resolvedType)
			// Map robot long names to short AgentType when needed.
			switch normalizeAgentType(resolvedType) {
			case "claude":
				agentType = AgentTypeClaude
			case "codex":
				agentType = AgentTypeCodex
			case "gemini":
				agentType = AgentTypeGemini
			case "antigravity":
				agentType = AgentTypeAntigravity
			case "cursor":
				agentType = AgentTypeCursor
			case "windsurf":
				agentType = AgentTypeWindsurf
			case "aider":
				agentType = AgentTypeAider
			case "oc":
				agentType = AgentTypeOpencode
			case "ollama":
				agentType = AgentTypeOllama
			}
		}
		ntmIndex := pane.NTMIndex
		if ntmIndex <= 0 {
			ntmIndex = pane.Index
		}
		variant := pane.Variant
		cwd := dir
		// Prefer command stored in registry meta (surfaced on pane.Command).
		cmdStr := strings.TrimSpace(pane.Command)

		argv := resolveHerdrAgentArgv(agentType, variant, "", "", cmdStr)
		if len(argv) == 0 {
			// Build from config template as last resort.
			tmpl := respawnAgentTemplate(cfg, agentType)
			if tmpl != "" {
				rendered, err := config.GenerateAgentCommand(tmpl, config.AgentTemplateVars{
					AgentType:   string(agentType),
					SessionName: session,
					PaneIndex:   ntmIndex,
					ProjectDir:  cwd,
				})
				if err == nil {
					argv = resolveHerdrAgentArgv(agentType, variant, "", "", rendered)
				}
			}
		}
		if len(argv) == 0 {
			argv = herdrPreferredAgentArgv(agentType, variant, "", "")
		}
		if len(argv) == 0 {
			failed = append(failed, fmt.Sprintf("%s: could not resolve agent argv for type %s", paneKey, agentType))
			fmt.Printf("  - Pane %s: FAILED (no argv for %s)\n", paneKey, agentType)
			continue
		}

		title := pane.Title
		if strings.TrimSpace(title) == "" {
			title = muxFormatPaneName(session, string(agentType), ntmIndex, variant)
		}

		if err := muxKillPane(pane.ID); err != nil {
			failed = append(failed, fmt.Sprintf("%s: kill: %v", paneKey, err))
			fmt.Printf("  - Pane %s: kill FAILED: %v\n", paneKey, err)
			continue
		}

		// Brief settle so herdr releases the closed pane before agent.start.
		time.Sleep(200 * time.Millisecond)

		launchAgent := FlatAgent{
			Type:  agentType,
			Index: ntmIndex,
			Model: variant,
		}
		// Prefer herdrLaunchAgent so naming/title/registry match spawn/add.
		// If NTM index is known, StartAgent keeps it via herdrLaunchAgent → StartAgent.
		launched, launchErr := herdrLaunchAgent(session, cwd, launchAgent, argv, title)
		if launchErr != nil {
			// Fallback: direct StartAgent with explicit index.
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			p, err2 := herdr.StartAgent(ctx, herdr.StartAgentOptions{
				Session:   session,
				Name:      fmt.Sprintf("%s-%s_%d", session, agentType, ntmIndex),
				AgentType: herdr.AgentType(agentType),
				Index:     ntmIndex,
				Variant:   variant,
				Cwd:       cwd,
				Argv:      argv,
				Focus:     false,
				Split:     "right",
			})
			cancel()
			if err2 != nil {
				failed = append(failed, fmt.Sprintf("%s: start: %v (after kill; original: %v)", paneKey, err2, launchErr))
				fmt.Printf("  - Pane %s: relaunch FAILED: %v\n", paneKey, err2)
				continue
			}
			if title != "" {
				_ = herdr.SetPaneTitle(p.ID, title)
			}
			launched = tmux.Pane{ID: p.ID, Index: p.Index, NTMIndex: p.NTMIndex, Title: title}
		}

		restarted = append(restarted, launched.ID)
		fmt.Printf("  - Pane %s → %s: agent relaunched (%s_%d)\n", paneKey, launched.ID, agentType, ntmIndex)
	}

	if len(restarted) > 0 {
		fmt.Printf("Restarted %d pane(s)\n", len(restarted))
	}
	if len(failed) > 0 {
		fmt.Printf("Failed to restart:\n")
		for _, f := range failed {
			fmt.Printf("  - %s\n", f)
		}
		return fmt.Errorf("%d pane(s) failed to restart cleanly", len(failed))
	}
	return nil
}

func respawnAgentTemplate(cfg *config.Config, agentType AgentType) string {
	if cfg == nil {
		return ""
	}
	switch agentType {
	case AgentTypeClaude:
		return cfg.Agents.Claude
	case AgentTypeCodex:
		return cfg.Agents.Codex
	case AgentTypeGemini:
		return cfg.Agents.Gemini
	case AgentTypeAntigravity:
		return cfg.Agents.Antigravity
	case AgentTypeCursor:
		return cfg.Agents.Cursor
	case AgentTypeWindsurf:
		return cfg.Agents.Windsurf
	case AgentTypeAider:
		return cfg.Agents.Aider
	case AgentTypeOpencode:
		return opencodeCommandOrDefault(cfg.Agents.Opencode)
	case AgentTypeOllama:
		return cfg.Agents.Ollama
	default:
		return ""
	}
}

func selectRespawnTargets(panes []tmux.Pane, paneFilterMap map[string]bool, agentType string, all bool) []tmux.Pane {
	hasPaneFilter := len(paneFilterMap) > 0
	targetType := normalizeAgentType(agentType)

	var targetPanes []tmux.Pane
	for _, pane := range panes {
		paneKey := fmt.Sprintf("%d", pane.Index)

		if hasPaneFilter && !paneFilterMap[paneKey] && !paneFilterMap[pane.ID] {
			continue
		}

		currentType := respawnPaneAgentType(pane)
		if targetType != "" && targetType != currentType {
			continue
		}

		// By default only restart agent panes. Explicit pane filters and --all opt out.
		if !all && !hasPaneFilter && targetType == "" {
			if pane.Index == 0 && currentType == "unknown" {
				continue
			}
			if currentType == "user" {
				continue
			}
		}

		targetPanes = append(targetPanes, pane)
	}

	return targetPanes
}

func respawnPaneAgentType(pane tmux.Pane) string {
	if resolved := normalizeAgentType(string(pane.Type)); resolved != "" && resolved != "unknown" {
		return resolved
	}
	return normalizeAgentType(robot.DetectAgentType(pane.Title))
}

// normalizeAgentType normalizes agent type aliases to canonical form.
func normalizeAgentType(t string) string {
	return robot.ResolveAgentType(t)
}
