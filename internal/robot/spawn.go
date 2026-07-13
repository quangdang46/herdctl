package robot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/audit"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/config"
	dispatchsvc "github.com/Dicklesworthstone/ntm/internal/dispatch"
	"github.com/Dicklesworthstone/ntm/internal/handoff"
	"github.com/Dicklesworthstone/ntm/internal/pressure"
	"github.com/Dicklesworthstone/ntm/internal/recovery"
	statuspkg "github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// Pre-compiled prompt patterns for isAgentReady (anchored to end of lines or output).
var promptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\$\s*$`), // Empty Shell prompt
	regexp.MustCompile(`(?m)^%\s*$`),  // Empty Zsh prompt
	regexp.MustCompile(`❯\s*$`),       // Modern prompts (U+276F)
	regexp.MustCompile(`›\s*$`),       // Codex prompt (U+203A) empty
	regexp.MustCompile(`(?m)^›`),      // Codex prompt with hint text
	regexp.MustCompile(`>\s*$`),       // Simple prompt at end of output
	regexp.MustCompile(`(?m)^>\s*$`),  // Simple prompt on its own line
}

// SpawnOptions configures the robot-spawn operation.
type SpawnOptions struct {
	Session            string
	Label              string   // Session label — constructs "{Session}--{Label}" if set
	CCCount            int      // Claude agents
	CodCount           int      // Codex agents
	GmiCount           int      // Gemini agents
	AgyCount           int      // Antigravity agents
	Preset             string   // Recipe/preset name
	NoUserPane         bool     // Don't create user pane
	WorkingDir         string   // Override working directory
	WaitReady          bool     // Wait for agents to be ready
	ReadyTimeout       int      // Timeout in seconds for ready detection
	DryRun             bool     // Preview mode: show what would happen without executing
	Safety             bool     // Fail if session already exists
	AssignWork         bool     // Enable orchestrator work assignment mode
	AssignStrategy     string   // Assignment strategy: top-n, diverse, dependency-aware, skill-matched
	CustomNames        []string // Custom agent names (used in order, then NATO alphabet)
	RequireReservation bool
	ReservationPaths   []string
	AssignmentDeps     *SpawnAssignmentDependencies
}

// SpawnAssignmentDependencies exposes assignment side-effect ports for focused
// tests while production uses the durable Beads, ledger, and dispatch services.
type SpawnAssignmentDependencies struct {
	FetchTriage       func(dir string) (*bv.TriageResponse, error)
	ListPanes         func(session string) ([]tmux.Pane, error)
	LoadStore         func(session string) (*assignment.AssignmentStore, error)
	ClaimBead         func(context.Context, string, string, string) (bv.BeadClaimResult, error)
	GetBeadStatus     func(string, string) (string, error)
	NewIdempotencyKey func() (string, error)
	ReservationPort   assignment.ReservationPort
	ResolveAgentName  func(context.Context, string, string, string, string) (string, error)
	ObserveSession    func(context.Context, string) (statuspkg.SessionObservation, error)
	DispatchDeliverer dispatchsvc.Deliverer
	DispatchPacer     dispatchsvc.Pacer
}

// SpawnOutput is the structured output for --robot-spawn.
type SpawnOutput struct {
	RobotResponse
	Session        string                   `json:"session"`
	CreatedAt      string                   `json:"created_at"`
	PresetUsed     string                   `json:"preset_used,omitempty"`
	WorkingDir     string                   `json:"working_dir"`
	Agents         []SpawnedAgent           `json:"agents"`
	Layout         string                   `json:"layout"`
	TotalStartupMs int64                    `json:"total_startup_ms"`
	Error          string                   `json:"error,omitempty"`
	DryRun         bool                     `json:"dry_run,omitempty"`
	WouldCreate    []SpawnedAgent           `json:"would_create,omitempty"`
	Mode           string                   `json:"mode,omitempty"`            // "orchestrator" when AssignWork is enabled
	Assignments    []SpawnAssignment        `json:"assignments,omitempty"`     // Work assignments when AssignWork is enabled
	AssignStrategy string                   `json:"assign_strategy,omitempty"` // Strategy used for assignments
	Recovery       *SpawnRecovery           `json:"recovery,omitempty"`        // Session recovery context from handoff
	Admission      *pressure.SpawnAdmission `json:"admission,omitempty"`       // Pre-spawn resource-pressure admission result
}

// SpawnRecovery contains session recovery context loaded from handoff.
type SpawnRecovery struct {
	HandoffPath  string `json:"handoff_path,omitempty"`  // Path to handoff file
	HandoffAge   string `json:"handoff_age,omitempty"`   // Human-readable age
	Goal         string `json:"goal,omitempty"`          // What previous session achieved
	Now          string `json:"now,omitempty"`           // What this session should do
	Status       string `json:"status,omitempty"`        // Previous session status
	Outcome      string `json:"outcome,omitempty"`       // Previous session outcome
	InjectedText string `json:"injected_text,omitempty"` // Formatted text injected into agents
}

// SpawnAssignment represents a work assignment to a spawned agent.
type SpawnAssignment struct {
	Pane              string `json:"pane"`        // Pane reference (e.g., "0.1")
	AgentType         string `json:"agent_type"`  // claude, codex, gemini
	BeadID            string `json:"bead_id"`     // Assigned bead ID
	BeadTitle         string `json:"bead_title"`  // Bead title for context
	Priority          string `json:"priority"`    // Bead priority (P0-P4)
	Claimed           bool   `json:"claimed"`     // Whether bead was successfully claimed (marked in_progress)
	PromptSent        bool   `json:"prompt_sent"` // Whether the work prompt was sent to the agent
	ClaimActor        string `json:"claim_actor,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	DispatchReceiptID string `json:"dispatch_receipt_id,omitempty"`
	ReservationIDs    []int  `json:"reservation_ids,omitempty"`
	ClaimError        string `json:"claim_error,omitempty"`  // Error during claim, if any
	PromptError       string `json:"prompt_error,omitempty"` // Error sending prompt, if any
}

// SpawnedAgent represents an agent created during spawn.
type SpawnedAgent struct {
	Pane      string `json:"pane"`
	Name      string `json:"name,omitempty"`
	Type      string `json:"type"`
	Variant   string `json:"variant,omitempty"`
	Title     string `json:"title"`
	Ready     bool   `json:"ready"`
	StartupMs int64  `json:"startup_ms"`
	Error     string `json:"error,omitempty"`
}

func collectSpawnAdmissionInput(opts SpawnOptions, cfg *config.Config, totalAgents, totalPanes int) pressure.SpawnAdmissionInput {
	input := pressure.SpawnAdmissionInput{
		Session:         opts.Session,
		RequestedAgents: totalAgents,
		RequestedPanes:  totalPanes,
	}

	if cfg == nil || cfg.SpawnPacing.Enabled {
		input.LargeSpawnThreshold = pressure.DefaultBudget().MaxPipelineFanout
		if cfg != nil {
			if cfg.SpawnPacing.MaxConcurrentSpawns > 0 {
				input.LargeSpawnThreshold = cfg.SpawnPacing.MaxConcurrentSpawns
			}
			input.MaxAgents = spawnAdmissionAgentLimit(cfg)
		}
		input.Pressure = collectSystemPressureSnapshot()
	}

	panesBySession, err := backendGetAllPanes()
	if err != nil {
		return input
	}
	input.RunningSessions = len(panesBySession)
	for session, panes := range panesBySession {
		if session == opts.Session {
			input.SessionPanes = len(panes)
		}
		input.CurrentPanes += len(panes)
		for _, pane := range panes {
			if isSpawnAdmissionAgentPane(pane) {
				input.RunningAgents++
			}
		}
	}
	return input
}

func spawnAdmissionAgentLimit(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	caps := cfg.SpawnPacing.AgentCaps
	total := 0
	for _, cap := range []int{caps.ClaudeMaxConcurrent, caps.CodexMaxConcurrent, caps.GeminiMaxConcurrent} {
		if cap > 0 {
			total += cap
		}
	}
	return total
}

func collectSystemPressureSnapshot() pressure.Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	g := pressure.New(pressure.Config{
		Mode:      pressure.ModeEnforce,
		Providers: []pressure.Provider{pressure.NewSystemProvider()},
	})
	return g.Refresh(ctx)
}

func isSpawnAdmissionAgentPane(pane tmux.Pane) bool {
	agentType := pane.Type.Canonical()
	return agentType != "" && agentType != tmux.AgentUser
}

// GetSpawn creates a session with agents and returns structured output.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSpawn(opts SpawnOptions, cfg *config.Config) (*SpawnOutput, error) {
	startTime := time.Now()
	correlationID := audit.NewCorrelationID()
	auditStart := time.Now()
	auditWorkingDir := ""
	auditSessionCreated := false
	auditPanesAdded := 0

	// Validate project name unconditionally: "--" is reserved for labels.
	if err := config.ValidateProjectName(opts.Session); err != nil {
		errOutput := &SpawnOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInvalidFlag, "Project names cannot contain '--' (reserved as label separator)"),
			Session:       opts.Session,
			Error:         err.Error(),
		}
		return errOutput, nil
	}

	// Apply goal label to session name (bd-1933u)
	if opts.Label != "" {
		if err := config.ValidateLabel(opts.Label); err != nil {
			errOutput := &SpawnOutput{
				RobotResponse: NewErrorResponse(fmt.Errorf("invalid label: %w", err), ErrCodeInvalidFlag, "Use a valid label (alphanumeric, dash, underscore)"),
				Session:       opts.Session,
				Error:         fmt.Sprintf("invalid label: %v", err),
			}
			return errOutput, nil
		}
		opts.Session = config.FormatSessionName(opts.Session, opts.Label)
	}

	output := &SpawnOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		CreatedAt:     startTime.UTC().Format(time.RFC3339),
		PresetUsed:    opts.Preset,
		Agents:        []SpawnedAgent{},
		Layout:        "tiled",
	}
	_ = audit.LogEvent(opts.Session, audit.EventTypeSpawn, audit.ActorSystem, "robot.spawn", map[string]interface{}{
		"phase":           "start",
		"session":         opts.Session,
		"total_agents":    opts.CCCount + opts.CodCount + opts.GmiCount + opts.AgyCount,
		"preset":          opts.Preset,
		"no_user_pane":    opts.NoUserPane,
		"dry_run":         opts.DryRun,
		"safety":          opts.Safety,
		"assign_work":     opts.AssignWork,
		"assign_strategy": opts.AssignStrategy,
		"correlation_id":  correlationID,
	}, nil)
	defer func() {
		agentsLaunched := 0
		if output != nil {
			agentsLaunched = len(output.Agents)
		}
		success := output != nil && output.Success
		payload := map[string]interface{}{
			"phase":           "finish",
			"session":         opts.Session,
			"total_agents":    opts.CCCount + opts.CodCount + opts.GmiCount + opts.AgyCount,
			"preset":          opts.Preset,
			"no_user_pane":    opts.NoUserPane,
			"dry_run":         opts.DryRun,
			"safety":          opts.Safety,
			"assign_work":     opts.AssignWork,
			"assign_strategy": opts.AssignStrategy,
			"session_created": auditSessionCreated,
			"panes_added":     auditPanesAdded,
			"agents_launched": agentsLaunched,
			"success":         success,
			"duration_ms":     time.Since(auditStart).Milliseconds(),
			"working_dir":     auditWorkingDir,
			"correlation_id":  correlationID,
		}
		if output != nil && output.Error != "" {
			payload["error"] = output.Error
		}
		_ = audit.LogEvent(opts.Session, audit.EventTypeSpawn, audit.ActorSystem, "robot.spawn", payload, nil)
	}()

	// Validate session name
	if err := backendValidateSessionName(opts.Session); err != nil {
		output.Error = fmt.Sprintf("invalid session name: %v", err)
		output.RobotResponse = NewErrorResponse(err, ErrCodeInvalidFlag, "Use a valid session name (a-z A-Z 0-9 _ -)")
		return output, nil
	}

	// Check backend availability (tmux default / herdr when NTM_BACKEND=herdr)
	if !backendIsInstalled() {
		output.Error = backendMissingError().Error()
		output.RobotResponse = NewErrorResponse(backendMissingError(), ErrCodeDependencyMissing, backendMissingHint())
		return output, nil
	}
	if err := backendRequireReady(); err != nil {
		output.Error = err.Error()
		output.RobotResponse = NewErrorResponse(err, ErrCodeDependencyMissing, backendMissingHint())
		return output, nil
	}

	// Safety check: fail if session already exists (when --spawn-safety is enabled)
	if opts.Safety && backendSessionExists(opts.Session) {
		output.Error = fmt.Sprintf("session '%s' already exists (--spawn-safety mode prevents reuse; use 'ntm kill %s' first)", opts.Session, opts.Session)
		output.RobotResponse = NewErrorResponse(fmt.Errorf("%s", output.Error), ErrCodeInvalidFlag, "Choose a new session name or disable --spawn-safety")
		return output, nil
	}

	// Get working directory
	dir := opts.WorkingDir
	if dir == "" && cfg != nil {
		dir = cfg.GetProjectDir(opts.Session)
	}
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			output.Error = fmt.Sprintf("could not determine working directory: %v", err)
			output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check working directory permissions")
			return output, nil
		}
	}
	output.WorkingDir = dir
	auditWorkingDir = dir

	// Load handoff context for session recovery (non-fatal if not found)
	spawnRecovery, handoffCtx := loadLatestHandoff(dir, opts.Session)
	if spawnRecovery != nil {
		output.Recovery = spawnRecovery
	}
	// handoffCtx is available for use in work prompts below
	_ = handoffCtx // silence unused warning when not in orchestrator mode

	totalAgents := opts.CCCount + opts.CodCount + opts.GmiCount + opts.AgyCount
	if totalAgents == 0 {
		output.Error = "no agents specified (use cc, cod, gmi, or agy counts)"
		output.RobotResponse = NewErrorResponse(fmt.Errorf("%s", output.Error), ErrCodeInvalidFlag, "Specify at least one agent count")
		return output, nil
	}

	// Calculate total panes needed
	totalPanes := totalAgents
	if !opts.NoUserPane {
		totalPanes++
	}

	admission := pressure.EvaluateSpawnAdmission(collectSpawnAdmissionInput(opts, cfg, totalAgents, totalPanes))
	output.Admission = &admission
	if !opts.DryRun && admission.Decision != pressure.SpawnAdmissionAdmit {
		output.Error = fmt.Sprintf("spawn admission %s: %s", admission.Decision, admission.Reason)
		hint := admission.Hint
		if hint == "" {
			hint = "Reduce requested agents or wait for resource headroom"
		}
		output.RobotResponse = NewErrorResponse(fmt.Errorf("%s", output.Error), ErrCodeResourceBusy, hint)
		return output, nil
	}

	// Dry-run mode: show what would happen without executing
	if opts.DryRun {
		output.DryRun = true
		output.WouldCreate = []SpawnedAgent{}

		// Initialize name map for dry-run preview
		var dryRunNameMap *AgentNameMap
		if len(opts.CustomNames) > 0 {
			dryRunNameMap = NewAgentNameMapWithCustomNames(opts.Session, opts.CustomNames)
		} else {
			dryRunNameMap = NewAgentNameMap(opts.Session)
		}

		// Build list of what would be created
		paneIdx := 0
		if !opts.NoUserPane {
			userPane := fmt.Sprintf("0.%d", paneIdx)
			output.WouldCreate = append(output.WouldCreate, SpawnedAgent{
				Pane:  userPane,
				Name:  dryRunNameMap.AssignNew("user", userPane),
				Type:  "user",
				Title: fmt.Sprintf("%s__user", opts.Session),
				Ready: true,
			})
			paneIdx++
		}

		for i := 0; i < opts.CCCount; i++ {
			ccPane := fmt.Sprintf("0.%d", paneIdx)
			output.WouldCreate = append(output.WouldCreate, SpawnedAgent{
				Pane:  ccPane,
				Name:  dryRunNameMap.AssignNew("claude", ccPane),
				Type:  "claude",
				Title: fmt.Sprintf("%s__cc_%d", opts.Session, i+1),
			})
			paneIdx++
		}

		for i := 0; i < opts.CodCount; i++ {
			codPane := fmt.Sprintf("0.%d", paneIdx)
			output.WouldCreate = append(output.WouldCreate, SpawnedAgent{
				Pane:  codPane,
				Name:  dryRunNameMap.AssignNew("codex", codPane),
				Type:  "codex",
				Title: fmt.Sprintf("%s__cod_%d", opts.Session, i+1),
			})
			paneIdx++
		}

		for i := 0; i < opts.GmiCount; i++ {
			gmiPane := fmt.Sprintf("0.%d", paneIdx)
			output.WouldCreate = append(output.WouldCreate, SpawnedAgent{
				Pane:  gmiPane,
				Name:  dryRunNameMap.AssignNew("gemini", gmiPane),
				Type:  "gemini",
				Title: fmt.Sprintf("%s__gmi_%d", opts.Session, i+1),
			})
			paneIdx++
		}

		for i := 0; i < opts.AgyCount; i++ {
			agyPane := fmt.Sprintf("0.%d", paneIdx)
			output.WouldCreate = append(output.WouldCreate, SpawnedAgent{
				Pane:  agyPane,
				Name:  dryRunNameMap.AssignNew("antigravity", agyPane),
				Type:  "antigravity",
				Title: fmt.Sprintf("%s__agy_%d", opts.Session, i+1),
			})
			paneIdx++
		}

		output.Layout = "tiled"
		return output, nil
	}

	// Ensure directory exists (only for real spawns, not dry-run)
	if err := os.MkdirAll(dir, 0755); err != nil {
		output.Error = fmt.Sprintf("creating directory: %v", err)
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check directory permissions")
		return output, nil
	}

	// Create session if it doesn't exist
	sessionCreated := false
	if !backendSessionExists(opts.Session) {
		historyLimit := tmux.DefaultHistoryLimit
		if cfg != nil && cfg.Tmux.HistoryLimit > 0 {
			historyLimit = cfg.Tmux.HistoryLimit
		}
		if err := backendCreateSessionWithHistoryLimit(opts.Session, dir, historyLimit); err != nil {
			output.Error = fmt.Sprintf("creating session: %v", err)
			output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check backend availability and session name")
			return output, nil
		}
		sessionCreated = true
		auditSessionCreated = true
	}

	// Get current panes
	panes, err := backendGetPanes(opts.Session)
	if err != nil {
		output.Error = fmt.Sprintf("getting panes: %v", err)
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check session state")
		return output, nil
	}

	// On herdr, agent panes are created by agent.start (which splits as needed).
	// Pre-creating empty shell panes would leave unused panes behind.
	// On tmux, pre-create panes then launch agents into them via send-keys.
	useHerdrStart := backendName() == "herdr"
	existingPanes := len(panes)
	if !useHerdrStart && existingPanes < totalPanes {
		toAdd := totalPanes - existingPanes
		auditPanesAdded = toAdd
		for i := 0; i < toAdd; i++ {
			if _, err := backendSplitWindow(opts.Session, dir); err != nil {
				output.Error = fmt.Sprintf("creating pane: %v", err)
				output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check pane layout constraints")
				return output, nil
			}
		}
	}

	// Get updated pane list (tmux after splits; herdr after session create)
	panes, err = backendGetPanes(opts.Session)
	if err != nil {
		output.Error = fmt.Sprintf("getting panes: %v", err)
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Check session state")
		return output, nil
	}

	// Apply tiled layout (best-effort; herdr may return not-supported)
	_ = backendApplyTiledLayout(opts.Session)

	// Initialize agent name map
	var nameMap *AgentNameMap
	if len(opts.CustomNames) > 0 {
		nameMap = NewAgentNameMapWithCustomNames(opts.Session, opts.CustomNames)
	} else {
		nameMap = NewAgentNameMap(opts.Session)
	}

	// Start assigning agents (skip first pane if user pane)
	startIdx := 0
	if !opts.NoUserPane {
		startIdx = 1
		// Add user pane info
		if len(panes) > 0 {
			userPaneRef := fmt.Sprintf("%d.%d", panes[0].WindowIndex, panes[0].Index)
			userName := nameMap.AssignNew("user", userPaneRef)
			output.Agents = append(output.Agents, SpawnedAgent{
				Pane:      userPaneRef,
				Name:      userName,
				Type:      "user",
				Title:     panes[0].Title,
				Ready:     true,
				StartupMs: 0,
			})
		}
	}

	agentNum := startIdx
	agentCommands := getAgentCommands(cfg)

	// Launch Claude agents
	for i := 0; i < opts.CCCount; i++ {
		var agent SpawnedAgent
		if useHerdrStart {
			agent = launchAgentHerdr(opts.Session, "claude", i+1, dir)
		} else if agentNum < len(panes) {
			agent = launchAgent(panes[agentNum], opts.Session, "claude", i+1, dir, agentCommands["claude"])
			agentNum++
		} else {
			break
		}
		agent.Name = nameMap.AssignNew("claude", agent.Pane)
		output.Agents = append(output.Agents, agent)
	}

	// Launch Codex agents
	for i := 0; i < opts.CodCount; i++ {
		var agent SpawnedAgent
		if useHerdrStart {
			agent = launchAgentHerdr(opts.Session, "codex", i+1, dir)
		} else if agentNum < len(panes) {
			agent = launchAgent(panes[agentNum], opts.Session, "codex", i+1, dir, agentCommands["codex"])
			agentNum++
		} else {
			break
		}
		agent.Name = nameMap.AssignNew("codex", agent.Pane)
		output.Agents = append(output.Agents, agent)
	}

	// Launch Gemini agents
	for i := 0; i < opts.GmiCount; i++ {
		var agent SpawnedAgent
		if useHerdrStart {
			agent = launchAgentHerdr(opts.Session, "gemini", i+1, dir)
		} else if agentNum < len(panes) {
			agent = launchAgent(panes[agentNum], opts.Session, "gemini", i+1, dir, agentCommands["gemini"])
			agentNum++
		} else {
			break
		}
		agent.Name = nameMap.AssignNew("gemini", agent.Pane)
		output.Agents = append(output.Agents, agent)
	}

	// Launch Antigravity agents
	for i := 0; i < opts.AgyCount; i++ {
		var agent SpawnedAgent
		if useHerdrStart {
			agent = launchAgentHerdr(opts.Session, "antigravity", i+1, dir)
		} else if agentNum < len(panes) {
			agent = launchAgent(panes[agentNum], opts.Session, "antigravity", i+1, dir, agentCommands["antigravity"])
			agentNum++
		} else {
			break
		}
		agent.Name = nameMap.AssignNew("antigravity", agent.Pane)
		output.Agents = append(output.Agents, agent)
	}

	// Wait for agents to be ready if requested
	if opts.WaitReady {
		timeout := opts.ReadyTimeout
		if timeout <= 0 {
			timeout = 30 // default 30 seconds
		}
		waitForAgentsReady(output, time.Duration(timeout)*time.Second)
	}

	// Orchestrator work assignment mode
	if opts.AssignWork {
		output.Mode = "orchestrator"
		output.AssignStrategy = normalizeAssignStrategy(opts.AssignStrategy)
		assignments := assignWorkToAgents(output, dir, opts.Session, output.AssignStrategy, cfg, opts.RequireReservation, opts.ReservationPaths, opts.AssignmentDeps)
		output.Assignments = assignments
		finalizeSpawnAssignmentOutput(output)
	}

	output.TotalStartupMs = time.Since(startTime).Milliseconds()

	// Update layout based on what was created
	if sessionCreated {
		output.Layout = "tiled"
	}

	return output, nil
}

// PrintSpawn creates a session with agents and outputs structured JSON.
// This is a thin wrapper around GetSpawn() for CLI output.
func PrintSpawn(opts SpawnOptions, cfg *config.Config) error {
	output, err := GetSpawn(opts, cfg)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot spawn failed")
}

// launchAgent launches a single agent into an existing pane (tmux path) and returns its info.
func launchAgent(pane tmux.Pane, session, agentType string, num int, dir, command string) SpawnedAgent {
	startTime := time.Now()

	title := fmt.Sprintf("%s__%s_%d", session, agentTypeShort(agentType), num)
	agent := SpawnedAgent{
		Pane:  fmt.Sprintf("%d.%d", pane.WindowIndex, pane.Index),
		Type:  agentType,
		Title: title,
		Ready: false,
	}

	// Set pane title
	if err := backendSetPaneTitle(pane.ID, title); err != nil {
		agent.Error = fmt.Sprintf("setting title: %v", err)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}

	// Launch agent command
	safeCommand, err := tmux.SanitizePaneCommand(command)
	if err != nil {
		agent.Error = fmt.Sprintf("invalid command: %v", err)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}

	cmd, err := tmux.BuildPaneCommand(dir, safeCommand)
	if err != nil {
		agent.Error = fmt.Sprintf("building command: %v", err)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}

	// Use SendKeysForAgent to handle different submission methods (buffer vs send-keys)
	if err := backendSendKeysForAgent(pane.ID, cmd, true, tmux.AgentType(agentTypeShort(agentType))); err != nil {
		agent.Error = fmt.Sprintf("launching: %v", err)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}

	agent.StartupMs = time.Since(startTime).Milliseconds()
	return agent
}

// launchAgentHerdr creates a pane+process via herdr agent.start (NTM_BACKEND=herdr).
func launchAgentHerdr(session, agentType string, num int, dir string) SpawnedAgent {
	startTime := time.Now()
	short := agentTypeShort(agentType)
	title := fmt.Sprintf("%s__%s_%d", session, short, num)
	name := fmt.Sprintf("%s-%s_%d", session, short, num)
	agent := SpawnedAgent{
		Type:  agentType,
		Title: title,
		Ready: false,
	}

	argv := backendPreferredAgentArgv(agentType)
	if len(argv) == 0 {
		agent.Error = fmt.Sprintf("no preferred argv for agent type %s", agentType)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pane, err := backendStartAgent(ctx, session, name, tmux.AgentType(agentType), num, "", dir, argv, title)
	if err != nil {
		agent.Error = fmt.Sprintf("herdr agent.start: %v", err)
		agent.StartupMs = time.Since(startTime).Milliseconds()
		return agent
	}
	agent.Pane = fmt.Sprintf("%d.%d", pane.WindowIndex, pane.Index)
	if agent.Pane == "0.0" && pane.ID != "" {
		// Fall back to herdr pane id when window/index mapping is unavailable.
		agent.Pane = pane.ID
	}
	if pane.Title != "" {
		agent.Title = pane.Title
	}
	agent.StartupMs = time.Since(startTime).Milliseconds()
	return agent
}

// waitForAgentsReady polls agents for ready state.
func waitForAgentsReady(output *SpawnOutput, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		allReady := true

		// Refresh herdr agent_status + pane id map once per poll when applicable.
		herdrStatuses := backendHerdrAgentStatuses(output.Session)
		paneIDByRef := map[string]string{}
		if len(herdrStatuses) > 0 || backendName() == "herdr" {
			if panes, err := backendGetPanes(output.Session); err == nil {
				for _, p := range panes {
					ref := fmt.Sprintf("%d.%d", p.WindowIndex, p.Index)
					paneIDByRef[ref] = p.ID
					if p.ID != "" {
						paneIDByRef[p.ID] = p.ID
					}
				}
			}
		}

		for i := range output.Agents {
			if output.Agents[i].Type == "user" {
				continue // User pane is always ready
			}
			if output.Agents[i].Ready {
				continue // Already detected as ready
			}

			paneRef := output.Agents[i].Pane

			// Prefer herdr agent_status when we can resolve a pane id.
			if paneID := paneIDByRef[paneRef]; paneID != "" {
				if hs, ok := herdrStatuses[paneID]; ok {
					switch backendMapHerdrAgentState(hs) {
					case "idle":
						output.Agents[i].Ready = true
						continue
					}
				}
			}

			// Build capture target: herdr pane ids (wN:pM) pass through; else session:W.P.
			target := paneRef
			if !strings.Contains(paneRef, ":") {
				if paneID := paneIDByRef[paneRef]; paneID != "" {
					target = paneID
				} else {
					target = fmt.Sprintf("%s:%s", output.Session, paneRef)
				}
			}

			// Capture pane output (50 lines to catch Claude's TUI)
			captured, err := backendCapturePaneOutput(target, 50)
			if err != nil {
				allReady = false
				continue
			}

			// Check for ready indicators
			if isAgentReady(captured, output.Agents[i].Type) {
				output.Agents[i].Ready = true
			} else {
				allReady = false
			}
		}

		if allReady {
			return
		}

		time.Sleep(pollInterval)
	}
}

// isAgentReady checks if agent output indicates ready state.
// Note: agentType is accepted for future type-specific detection but currently unused.
func isAgentReady(output, _ string) bool {
	lower := strings.ToLower(output)

	// Common ready indicators (case-insensitive)
	lowerPatterns := []string{
		"claude>",
		"claude >",
		"codex>",
		"openai codex",
		"context left",
		"gemini>",
		">>>", // Python REPL
		"waiting for input",
		"ready",
		"how can i help",
		// Claude Code TUI indicators
		"claude code v",      // Version banner
		"welcome back",       // Greeting
		"bypass permissions", // Status line
		"try \"",             // Example prompt
	}

	for _, pattern := range lowerPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	for _, p := range promptPatterns {
		if p.MatchString(output) {
			return true
		}
	}

	return false
}

// agentTypeShort returns short form for pane naming.
func agentTypeShort(agentType string) string {
	switch tmux.AgentType(agentType).Canonical() {
	case tmux.AgentClaude:
		return "cc"
	case tmux.AgentCodex:
		return "cod"
	case tmux.AgentGemini:
		return "gmi"
	case tmux.AgentAntigravity:
		return "agy"
	case tmux.AgentCursor:
		return "cursor"
	case tmux.AgentWindsurf:
		return "windsurf"
	case tmux.AgentAider:
		return "aider"
	case tmux.AgentOllama:
		return "ollama"
	case tmux.AgentUser:
		return "user"
	default:
		return strings.TrimSpace(agentType)
	}
}

// getAgentCommands returns the commands to launch each agent type.
// Templates are rendered with empty vars (optional fields only).
func getAgentCommands(cfg *config.Config) map[string]string {
	defaults := map[string]string{
		"claude":      "claude",
		"codex":       "codex",
		"gemini":      "gemini",
		"antigravity": "agy",
	}

	if cfg != nil && cfg.Agents.Claude != "" {
		defaults["claude"] = cfg.Agents.Claude
	}
	if cfg != nil && cfg.Agents.Codex != "" {
		defaults["codex"] = cfg.Agents.Codex
	}
	if cfg != nil && cfg.Agents.Gemini != "" {
		defaults["gemini"] = cfg.Agents.Gemini
	}
	if cfg != nil && cfg.Agents.Antigravity != "" {
		defaults["antigravity"] = cfg.Agents.Antigravity
	}

	// Render templates with empty vars (all template fields are optional)
	vars := config.AgentTemplateVars{}
	for agentType, cmdTemplate := range defaults {
		if rendered, err := config.GenerateAgentCommand(cmdTemplate, vars); err == nil {
			defaults[agentType] = rendered
		}
		// On error, keep original command (non-template or invalid template)
	}

	return defaults
}

// loadLatestHandoff loads the most recent handoff for a session and returns recovery context.
// Returns nil if no handoff is found or an error occurs (non-fatal).
func loadLatestHandoff(workDir, sessionName string) (*SpawnRecovery, *recovery.HandoffContext) {
	reader := handoff.NewReader(workDir)
	h, path, err := reader.FindLatest(sessionName)
	if err != nil || h == nil {
		return nil, nil
	}

	// Convert to recovery context
	ctx := recovery.HandoffContextFromHandoff(h, path)
	if ctx == nil {
		return nil, nil
	}

	// Format the injection text for fresh spawn
	injectedText := recovery.GetInjectionForType(recovery.SessionFreshSpawn, ctx, nil)

	// Build spawn recovery info
	spawnRecovery := &SpawnRecovery{
		HandoffPath:  path,
		HandoffAge:   recovery.HumanizeDuration(ctx.Age),
		Goal:         ctx.Goal,
		Now:          ctx.Now,
		Status:       ctx.Status,
		Outcome:      ctx.Outcome,
		InjectedText: injectedText,
	}

	return spawnRecovery, ctx
}

// normalizeAssignStrategy validates and normalizes the assignment strategy.
func normalizeAssignStrategy(strategy string) string {
	s := strings.ToLower(strings.TrimSpace(strategy))
	switch s {
	case "top-n", "topn":
		return "top-n"
	case "diverse":
		return "diverse"
	case "dependency-aware", "dependency":
		return "dependency-aware"
	case "skill-matched", "skill":
		return "skill-matched"
	default:
		return "top-n" // Default strategy
	}
}

// assignWorkToAgents gets triage recommendations, claims beads, and sends work prompts.
func assignWorkToAgents(output *SpawnOutput, workDir, session, strategy string, cfg *config.Config, requireReservation bool, reservationPaths []string, customDeps *SpawnAssignmentDependencies) []SpawnAssignment {
	var assignments []SpawnAssignment
	deps := spawnAssignmentDeps(customDeps)

	// Get non-user agents that are ready
	var readyAgents []SpawnedAgent
	for _, agent := range output.Agents {
		if agent.Type == "user" {
			continue
		}
		// Include agents even if not marked ready (best effort)
		readyAgents = append(readyAgents, agent)
	}

	if len(readyAgents) == 0 {
		return assignments
	}

	// Get triage recommendations from bv
	triage, err := deps.FetchTriage(workDir)
	if err != nil {
		return spawnAgentPlanErrors(readyAgents, fmt.Errorf("load bv triage: %w", err))
	}
	if triage == nil {
		return spawnAgentPlanErrors(readyAgents, errors.New("load bv triage: empty response"))
	}

	// Get work items based on strategy
	workItems := getWorkItemsForStrategy(triage, strategy, len(readyAgents))
	if len(workItems) == 0 {
		return assignments
	}

	store, err := deps.LoadStore(session)
	if err != nil {
		return spawnAssignmentPlanErrors(readyAgents, workItems, fmt.Errorf("load assignment ledger: %w", err))
	}
	redactionConfig := config.Default().Redaction.ToRedactionLibConfig()
	if cfg != nil {
		redactionConfig = cfg.Redaction.ToRedactionLibConfig()
	}
	dispatchPort := newRobotAtomicPaneDispatchPort(session, deps.ListPanes, deps.ObserveSession, redactionConfig, deps.DispatchDeliverer, deps.DispatchPacer)
	claimPort := newRobotAtomicClaimPort(workDir, deps.ClaimBead)
	panes, err := deps.ListPanes(session)
	if err != nil {
		return spawnAssignmentPlanErrors(readyAgents, workItems, fmt.Errorf("load pane topology: %w", err))
	}
	multiWindow := tmux.PanesSpanMultipleWindows(panes)
	reservationPort := deps.ReservationPort
	resolveAgentName := deps.ResolveAgentName

	// Assign work to agents
	for i, agent := range readyAgents {
		if i >= len(workItems) {
			break
		}

		item := workItems[i]
		spawnAssignment := SpawnAssignment{
			Pane:      agent.Pane,
			AgentType: agent.Type,
			BeadID:    item.ID,
			BeadTitle: item.Title,
			Priority:  fmt.Sprintf("P%d", item.Priority),
		}
		resolved, resolveErr := tmux.ResolvePaneSelectors(panes, []string{agent.Pane}, true)
		if resolveErr != nil {
			spawnAssignment.ClaimError = fmt.Sprintf("resolve pane %s: %v", agent.Pane, resolveErr)
			assignments = append(assignments, spawnAssignment)
			continue
		}
		pane := resolved[0]
		spawnAssignment.Pane = pane.Ref().Canonical(multiWindow)
		target := pane.ID
		if target == "" {
			target = pane.Ref().Physical()
		}
		prompt := generateWorkPrompt(item)
		agentName := ""
		idempotencyKey := ""
		if replay := robotAtomicReplayIntent(store, item.ID, target, pane.Index, agent.Type, prompt, requireReservation, reservationPaths); replay != nil {
			agentName = replay.AgentName
			idempotencyKey = replay.IdempotencyKey
		} else {
			observation, observeErr := deps.ObserveSession(context.Background(), session)
			if observeErr != nil {
				spawnAssignment.ClaimError = fmt.Sprintf("observe pane %s before assignment: %v", spawnAssignment.Pane, observeErr)
				assignments = append(assignments, spawnAssignment)
				continue
			}
			if !observation.SafeToDispatch(target) {
				spawnAssignment.ClaimError = fmt.Sprintf("pane %s (%s) is not safe to dispatch", spawnAssignment.Pane, target)
				assignments = append(assignments, spawnAssignment)
				continue
			}

			agentName = strings.TrimSpace(agent.Name)
			if requireReservation {
				if reservationPort == nil {
					mailRuntime, runtimeErr := newRobotAgentMailReservationRuntime(context.Background(), workDir, session, nil)
					if runtimeErr != nil {
						spawnAssignment.ClaimError = runtimeErr.Error()
						assignments = append(assignments, spawnAssignment)
						continue
					}
					reservationPort = mailRuntime
					if resolveAgentName == nil {
						resolveAgentName = mailRuntime.ResolveRecipient
					}
				}
				if resolveAgentName == nil {
					spawnAssignment.ClaimError = "required reservation has no exact Agent Mail pane-identity resolver"
					assignments = append(assignments, spawnAssignment)
					continue
				}
				agentName, resolveErr = resolveAgentName(context.Background(), workDir, session, target, pane.Title)
				if resolveErr != nil {
					spawnAssignment.ClaimError = resolveErr.Error()
					assignments = append(assignments, spawnAssignment)
					continue
				}
				agentName = strings.TrimSpace(agentName)
			}
			if agentName == "" {
				spawnAssignment.ClaimError = fmt.Sprintf("pane %s (%s) has no canonical assignment identity", spawnAssignment.Pane, target)
				assignments = append(assignments, spawnAssignment)
				continue
			}
			var keyErr error
			idempotencyKey, keyErr = robotAtomicIdempotencyKey(
				store, item.ID, target, pane.Index, agent.Type, agentName, prompt,
				requireReservation, reservationPaths, deps.NewIdempotencyKey,
			)
			if keyErr != nil {
				spawnAssignment.ClaimError = keyErr.Error()
				assignments = append(assignments, spawnAssignment)
				continue
			}
		}
		spawnAssignment.IdempotencyKey = idempotencyKey
		coordinator := assignment.NewAtomicCoordinator(store, claimPort, reservationPort, dispatchPort, dispatchPort).
			WithWorkItemStatusPort(assignment.WorkItemStatusFunc(func(_ context.Context, beadID string) (string, error) {
				return deps.GetBeadStatus(workDir, beadID)
			}))
		result, executeErr := coordinator.Execute(context.Background(), spawnAtomicRequest(
			item, target, pane.Index, agent.Type, agentName, prompt, idempotencyKey, requireReservation, reservationPaths,
		))
		if result.Assignment != nil && result.Assignment.IdempotencyKey == idempotencyKey {
			spawnAssignment.Claimed = result.Assignment.ClaimState == assignment.ClaimClaimed
			spawnAssignment.ClaimActor = result.Assignment.ClaimActor
			spawnAssignment.DispatchReceiptID = result.Assignment.DispatchReceiptID
			spawnAssignment.ReservationIDs = append([]int(nil), result.Assignment.ReservationIDs...)
		}
		if executeErr != nil {
			if spawnAssignment.Claimed {
				spawnAssignment.PromptError = executeErr.Error()
			} else {
				spawnAssignment.ClaimError = executeErr.Error()
			}
		} else {
			spawnAssignment.PromptSent = result.Sent
		}

		assignments = append(assignments, spawnAssignment)
	}

	return assignments
}

func spawnAgentPlanErrors(agents []SpawnedAgent, err error) []SpawnAssignment {
	result := make([]SpawnAssignment, 0, len(agents))
	for _, agent := range agents {
		result = append(result, SpawnAssignment{
			Pane: agent.Pane, AgentType: agent.Type, ClaimError: err.Error(),
		})
	}
	return result
}

func finalizeSpawnAssignmentOutput(output *SpawnOutput) {
	if output == nil {
		return
	}
	failed := 0
	for _, spawnAssignment := range output.Assignments {
		if spawnAssignment.ClaimError != "" || spawnAssignment.PromptError != "" || !spawnAssignment.Claimed || !spawnAssignment.PromptSent {
			failed++
		}
	}
	if failed == 0 {
		return
	}
	output.Error = fmt.Sprintf("%d of %d spawn work assignments failed", failed, len(output.Assignments))
	output.RobotResponse = NewErrorResponse(
		fmt.Errorf("%s", output.Error),
		ErrCodeInternalError,
		"Inspect assignments[].claim_error and assignments[].prompt_error; failed targets were not dispatched",
	)
}

func spawnAtomicRequest(item workItem, target string, pane int, agentType, agentName, prompt, key string, requireReservation bool, reservationPaths []string) assignment.AtomicRequest {
	return assignment.AtomicRequest{
		BeadID: item.ID, BeadTitle: item.Title, Target: target, OccupancyKey: target, Pane: pane,
		AgentType: agentType, AgentName: agentName, Actor: agentName, Prompt: prompt,
		IdempotencyKey: key, RequireReservation: requireReservation, ReservationTTL: time.Hour,
		RequestedPaths: append([]string(nil), reservationPaths...),
	}
}

func spawnAssignmentPlanErrors(agents []SpawnedAgent, items []workItem, err error) []SpawnAssignment {
	limit := len(agents)
	if len(items) < limit {
		limit = len(items)
	}
	result := make([]SpawnAssignment, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, SpawnAssignment{
			Pane: agents[i].Pane, AgentType: agents[i].Type, BeadID: items[i].ID,
			BeadTitle: items[i].Title, Priority: fmt.Sprintf("P%d", items[i].Priority),
			ClaimError: err.Error(),
		})
	}
	return result
}

func spawnAssignmentDeps(custom *SpawnAssignmentDependencies) SpawnAssignmentDependencies {
	observer := statuspkg.NewSessionObserver(statuspkg.NewDetector())
	deps := SpawnAssignmentDependencies{
		FetchTriage:       bv.GetTriage,
		ListPanes:         backendGetPanes,
		LoadStore:         assignment.LoadStoreStrict,
		ClaimBead:         bv.ClaimBead,
		GetBeadStatus:     bv.GetBeadStatus,
		NewIdempotencyKey: assignment.NewAssignmentIdempotencyKey,
		ObserveSession:    observer.Observe,
		DispatchDeliverer: backendDispatchDeliverer(""),
	}
	if custom == nil {
		return deps
	}
	if custom.FetchTriage != nil {
		deps.FetchTriage = custom.FetchTriage
	}
	if custom.ListPanes != nil {
		deps.ListPanes = custom.ListPanes
	}
	if custom.LoadStore != nil {
		deps.LoadStore = custom.LoadStore
	}
	if custom.ClaimBead != nil {
		deps.ClaimBead = custom.ClaimBead
	}
	if custom.GetBeadStatus != nil {
		deps.GetBeadStatus = custom.GetBeadStatus
	}
	if custom.NewIdempotencyKey != nil {
		deps.NewIdempotencyKey = custom.NewIdempotencyKey
	}
	if custom.ReservationPort != nil {
		deps.ReservationPort = custom.ReservationPort
	}
	if custom.ResolveAgentName != nil {
		deps.ResolveAgentName = custom.ResolveAgentName
	}
	if custom.ObserveSession != nil {
		deps.ObserveSession = custom.ObserveSession
	}
	if custom.DispatchDeliverer != nil {
		deps.DispatchDeliverer = custom.DispatchDeliverer
	}
	if custom.DispatchPacer != nil {
		deps.DispatchPacer = custom.DispatchPacer
	}
	return deps
}

// workItem represents a work item from triage for assignment.
type workItem struct {
	ID       string
	Title    string
	Priority int
	Score    float64
	Type     string
	Reasons  []string
}

// getWorkItemsForStrategy returns work items based on the selected strategy.
func getWorkItemsForStrategy(triage *bv.TriageResponse, strategy string, count int) []workItem {
	var items []workItem

	switch strategy {
	case "diverse":
		// Get a mix of different task types
		items = getDiverseWorkItems(triage, count)
	case "dependency-aware":
		// Prioritize items that unblock others
		items = getDependencyAwareItems(triage, count)
	case "skill-matched":
		// This would ideally match agent types to task types
		// For now, fall through to top-n
		fallthrough
	case "top-n":
		fallthrough
	default:
		// Get top N recommendations by score
		items = getTopNWorkItems(triage, count)
	}

	return items
}

// getTopNWorkItems returns the top N recommendations by score.
func getTopNWorkItems(triage *bv.TriageResponse, count int) []workItem {
	var items []workItem

	for i, rec := range triage.Triage.Recommendations {
		if i >= count {
			break
		}
		items = append(items, workItem{
			ID:       rec.ID,
			Title:    rec.Title,
			Priority: rec.Priority,
			Score:    rec.Score,
			Type:     rec.Type,
			Reasons:  rec.Reasons,
		})
	}

	return items
}

// getDiverseWorkItems returns a diverse set of work items by type.
func getDiverseWorkItems(triage *bv.TriageResponse, count int) []workItem {
	var items []workItem
	seenTypes := make(map[string]bool)

	// First pass: get one of each type
	for _, rec := range triage.Triage.Recommendations {
		if len(items) >= count {
			break
		}
		if !seenTypes[rec.Type] {
			items = append(items, workItem{
				ID:       rec.ID,
				Title:    rec.Title,
				Priority: rec.Priority,
				Score:    rec.Score,
				Type:     rec.Type,
				Reasons:  rec.Reasons,
			})
			seenTypes[rec.Type] = true
		}
	}

	// Second pass: fill remaining slots with top items
	if len(items) < count {
		for _, rec := range triage.Triage.Recommendations {
			if len(items) >= count {
				break
			}
			// Check if already included
			found := false
			for _, existing := range items {
				if existing.ID == rec.ID {
					found = true
					break
				}
			}
			if !found {
				items = append(items, workItem{
					ID:       rec.ID,
					Title:    rec.Title,
					Priority: rec.Priority,
					Score:    rec.Score,
					Type:     rec.Type,
					Reasons:  rec.Reasons,
				})
			}
		}
	}

	return items
}

// getDependencyAwareItems prioritizes items that unblock the most work.
func getDependencyAwareItems(triage *bv.TriageResponse, count int) []workItem {
	var items []workItem

	// First, add blockers to clear (these unblock other work)
	for _, blocker := range triage.Triage.BlockersToClear {
		if len(items) >= count {
			break
		}
		if blocker.Actionable {
			items = append(items, workItem{
				ID:       blocker.ID,
				Title:    blocker.Title,
				Priority: 0, // Blockers get high priority
				Score:    float64(blocker.UnblocksCount),
				Type:     "blocker",
				Reasons:  []string{fmt.Sprintf("Unblocks %d items", blocker.UnblocksCount)},
			})
		}
	}

	// Then fill with top recommendations
	if len(items) < count {
		for _, rec := range triage.Triage.Recommendations {
			if len(items) >= count {
				break
			}
			// Check if already included
			found := false
			for _, existing := range items {
				if existing.ID == rec.ID {
					found = true
					break
				}
			}
			if !found {
				items = append(items, workItem{
					ID:       rec.ID,
					Title:    rec.Title,
					Priority: rec.Priority,
					Score:    rec.Score,
					Type:     rec.Type,
					Reasons:  rec.Reasons,
				})
			}
		}
	}

	return items
}

// generateWorkPrompt creates a prompt for an agent to work on a bead.
func generateWorkPrompt(item workItem) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Work on bead %s: %s\n\n", item.ID, item.Title))
	sb.WriteString("Use `br show " + item.ID + "` to see full details.\n")
	sb.WriteString("This bead has been marked as in_progress.\n")

	if len(item.Reasons) > 0 {
		sb.WriteString("\nContext:\n")
		for _, reason := range item.Reasons {
			sb.WriteString("- " + reason + "\n")
		}
	}

	sb.WriteString("\nWhen done, close it with: `br close " + item.ID + " --reason \"Completed\"`")

	return sb.String()
}
