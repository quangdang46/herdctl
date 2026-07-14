//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// robot_ensemble_test.go validates ensemble robot commands and JSON envelopes.
//
// Bead: bd-1ny8s - Task: Write E2E tests for ensemble Robot API
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

type robotEnvelope struct {
	Success   bool   `json:"success"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

type paginationInfo struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	Count   int  `json:"count"`
	Total   int  `json:"total"`
	HasMore bool `json:"has_more"`
}

type categoryRef struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type categoryInfo struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	ModeCount int    `json:"mode_count"`
}

type ensembleModeInfo struct {
	ID       string      `json:"id"`
	Code     string      `json:"code"`
	Tier     string      `json:"tier"`
	Name     string      `json:"name"`
	Category categoryRef `json:"category"`
}

type ensembleModesOutput struct {
	robotEnvelope
	Action            string             `json:"action"`
	Modes             []ensembleModeInfo `json:"modes"`
	Categories        []categoryInfo     `json:"categories"`
	DefaultTier       string             `json:"default_tier"`
	TotalModes        int                `json:"total_modes"`
	CoreModes         int                `json:"core_modes"`
	AdvancedModes     int                `json:"advanced_modes"`
	ExperimentalModes int                `json:"experimental_modes"`
	Pagination        *paginationInfo    `json:"pagination,omitempty"`
}

type presetInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Modes         []string `json:"modes"`
	ModeCount     int      `json:"mode_count"`
	AllowAdvanced bool     `json:"allow_advanced"`
	Tags          []string `json:"tags,omitempty"`
}

type ensemblePresetsOutput struct {
	robotEnvelope
	Action  string       `json:"action"`
	Presets []presetInfo `json:"presets"`
	Count   int          `json:"count"`
}

type ensembleSpawnMode struct {
	ID        string `json:"id"`
	Code      string `json:"code,omitempty"`
	Tier      string `json:"tier,omitempty"`
	Pane      string `json:"pane,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

type ensembleSpawnOutput struct {
	robotEnvelope
	Action     string              `json:"action"`
	Session    string              `json:"session"`
	Preset     string              `json:"preset,omitempty"`
	Question   string              `json:"question,omitempty"`
	Assignment string              `json:"assignment,omitempty"`
	Agents     map[string]int      `json:"agents,omitempty"`
	Modes      []ensembleSpawnMode `json:"modes"`
	Status     string              `json:"status,omitempty"`
}

type ensembleMetrics struct {
	CoverageMap      map[string]int `json:"coverage_map"`
	RedundancyScore  float64        `json:"redundancy_score,omitempty"`
	FindingsVelocity float64        `json:"findings_velocity,omitempty"`
	ConflictDensity  float64        `json:"conflict_density,omitempty"`
}

type ensembleModeState struct {
	ID        string `json:"id"`
	Code      string `json:"code,omitempty"`
	Tier      string `json:"tier,omitempty"`
	Name      string `json:"name,omitempty"`
	Pane      string `json:"pane,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Status    string `json:"status,omitempty"`
}

type ensembleState struct {
	Session  string              `json:"session"`
	Question string              `json:"question,omitempty"`
	Preset   string              `json:"preset,omitempty"`
	Status   string              `json:"status,omitempty"`
	Modes    []ensembleModeState `json:"modes"`
	Metrics  ensembleMetrics     `json:"metrics"`
}

type ensembleSummary struct {
	TotalModes    int `json:"total_modes"`
	Completed     int `json:"completed"`
	Working       int `json:"working"`
	Pending       int `json:"pending"`
	CoreModes     int `json:"core_modes"`
	AdvancedModes int `json:"advanced_modes"`
}

type ensembleOutput struct {
	robotEnvelope
	Ensemble ensembleState   `json:"ensemble"`
	Summary  ensembleSummary `json:"summary"`
}

type synthesisReport struct {
	Summary              string  `json:"summary"`
	Strategy             string  `json:"strategy"`
	Format               string  `json:"format"`
	FindingsCount        int     `json:"findings_count"`
	RecommendationsCount int     `json:"recommendations_count"`
	RisksCount           int     `json:"risks_count"`
	QuestionsCount       int     `json:"questions_count"`
	Confidence           float64 `json:"confidence"`
}

type synthesisAudit struct {
	ConflictCount     int      `json:"conflict_count"`
	UnresolvedCount   int      `json:"unresolved_count"`
	HighConflictPairs []string `json:"high_conflict_pairs"`
}

type ensembleSynthesizeOutput struct {
	robotEnvelope
	Action  string           `json:"action"`
	Session string           `json:"session"`
	Status  string           `json:"status"`
	Report  *synthesisReport `json:"report,omitempty"`
	Audit   *synthesisAudit  `json:"audit,omitempty"`
}

func runRobotEnsembleCmd(t *testing.T, suite *TestSuite, label string, args ...string) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ntm", args...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] %s timed out: args=%v", label, args)
		return output, fmt.Errorf("command timed out after 60s")
	}

	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] %s args=%v bytes=%d", label, args, len(output))
	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] %s raw=%s", label, strings.TrimSpace(string(output)))
	return output, err
}

func parseRobotJSON(t *testing.T, suite *TestSuite, label string, output []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(output, v); err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] %s JSON parse failed: %v output=%s", label, err, string(output))
	}
	suite.Logger().LogJSON("[E2E-ROBOT-ENSEMBLE] "+label+" parsed", v)
}

func supportsRobotFlag(flag string) bool {
	cmd := exec.Command(mustE2EBin(), "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), flag)
}

func TestE2E_RobotEnsemble_ModesAndPresets(t *testing.T) {
	CommonE2EPrerequisites(t)

	if !supportsRobotFlag("--robot-ensemble-modes") || !supportsRobotFlag("--robot-ensemble-presets") {
		t.Skip("robot ensemble modes/presets flags not supported by current ntm binary")
	}

	suite := NewTestSuite(t, "robot_ensemble_modes")
	defer suite.Teardown()

	// --robot-ensemble-modes (default core)
	output, err := runRobotEnsembleCmd(t, suite, "modes_default", "--robot-ensemble-modes")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_default failed: %v", err)
	}

	var modesDefault ensembleModesOutput
	parseRobotJSON(t, suite, "modes_default", output, &modesDefault)

	if !modesDefault.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_default failed: %s", modesDefault.Error)
	}
	for _, mode := range modesDefault.Modes {
		if strings.ToLower(mode.Tier) != "core" {
			t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_default should be core-only, got tier=%s", mode.Tier)
		}
	}

	// --robot-ensemble-modes --tier=all
	output, err = runRobotEnsembleCmd(t, suite, "modes_all", "--robot-ensemble-modes", "--tier=all")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_all failed: %v", err)
	}

	var modesAll ensembleModesOutput
	parseRobotJSON(t, suite, "modes_all", output, &modesAll)
	if !modesAll.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_all failed: %s", modesAll.Error)
	}
	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] tier_counts core=%d advanced=%d", modesAll.CoreModes, modesAll.AdvancedModes)

	// --robot-ensemble-modes --category=Formal
	output, err = runRobotEnsembleCmd(t, suite, "modes_formal", "--robot-ensemble-modes", "--category=Formal", "--tier=all")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_formal failed: %v", err)
	}

	var modesFormal ensembleModesOutput
	parseRobotJSON(t, suite, "modes_formal", output, &modesFormal)
	if !modesFormal.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_formal failed: %s", modesFormal.Error)
	}
	for _, mode := range modesFormal.Modes {
		if strings.ToLower(mode.Category.Name) != "formal" {
			t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_formal filter mismatch: %s", mode.Category.Name)
		}
	}

	// --robot-ensemble-modes pagination
	output, err = runRobotEnsembleCmd(t, suite, "modes_paged", "--robot-ensemble-modes", "--tier=all", "--limit=5", "--offset=2")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_paged failed: %v", err)
	}

	var modesPaged ensembleModesOutput
	parseRobotJSON(t, suite, "modes_paged", output, &modesPaged)
	if !modesPaged.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_paged failed: %s", modesPaged.Error)
	}
	if modesPaged.Pagination == nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_paged missing pagination")
	}
	if modesPaged.Pagination.Limit != 5 || modesPaged.Pagination.Offset != 2 {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_paged pagination mismatch: %+v", *modesPaged.Pagination)
	}

	// --robot-ensemble-presets
	output, err = runRobotEnsembleCmd(t, suite, "presets", "--robot-ensemble-presets")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] presets failed: %v", err)
	}

	var presets ensemblePresetsOutput
	parseRobotJSON(t, suite, "presets", output, &presets)
	if !presets.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] presets failed: %s", presets.Error)
	}
	if presets.Count != len(presets.Presets) {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] presets count mismatch: count=%d len=%d", presets.Count, len(presets.Presets))
	}
}

func TestE2E_RobotEnsemble_SpawnStateAndSynthesize(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	if !supportsRobotFlag("--robot-ensemble-spawn") || !supportsRobotFlag("--robot-ensemble") || !supportsRobotFlag("--robot-ensemble-synthesize") {
		t.Skip("robot ensemble spawn/state/synthesize flags not supported by current ntm binary")
	}

	suite := NewTestSuite(t, "robot_ensemble_spawn")
	defer suite.Teardown()

	agents := GetAvailableAgent()
	if agents == "" {
		t.Skip("no agent CLI available for ensemble spawn")
	}

	preset := "project-diagnosis"
	if supportsRobotFlag("--robot-ensemble-presets") {
		output, err := runRobotEnsembleCmd(t, suite, "presets_for_spawn", "--robot-ensemble-presets")
		if err == nil {
			var presets ensemblePresetsOutput
			parseRobotJSON(t, suite, "presets_for_spawn", output, &presets)
			if presets.Success && len(presets.Presets) > 0 {
				preset = presets.Presets[0].Name
			}
		}
	}

	session := suite.Session()
	suite.cleanup = append(suite.cleanup, func() {
		exec.Command(tmux.BinaryPath(), "kill-session", "-t", session).Run()
	})

	output, err := runRobotEnsembleCmd(t, suite, "spawn", fmt.Sprintf("--robot-ensemble-spawn=%s", session),
		fmt.Sprintf("--preset=%s", preset),
		"--question=E2E robot ensemble test",
		fmt.Sprintf("--agents=%s=1", agents),
	)
	if err != nil {
		suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] spawn command error: %v", err)
	}

	var spawn ensembleSpawnOutput
	parseRobotJSON(t, suite, "spawn", output, &spawn)

	if !spawn.Success {
		if spawn.ErrorCode == "NOT_IMPLEMENTED" {
			t.Skip("robot ensemble spawn not implemented in current binary")
		}
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] spawn failed: %s (%s)", spawn.Error, spawn.ErrorCode)
	}
	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] session=%s agents=%v", spawn.Session, spawn.Agents)

	output, err = runRobotEnsembleCmd(t, suite, "state", fmt.Sprintf("--robot-ensemble=%s", session))
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] state failed: %v", err)
	}

	var state ensembleOutput
	parseRobotJSON(t, suite, "state", output, &state)
	if !state.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] state failed: %s", state.Error)
	}
	if state.Ensemble.Session != session {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] state session mismatch: got=%s want=%s", state.Ensemble.Session, session)
	}
	if len(state.Ensemble.Modes) == 0 {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] state has no modes")
	}

	output, err = runRobotEnsembleCmd(t, suite, "synthesize", fmt.Sprintf("--robot-ensemble-synthesize=%s", session))
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] synthesize failed: %v", err)
	}

	var synth ensembleSynthesizeOutput
	parseRobotJSON(t, suite, "synthesize", output, &synth)
	if !synth.Success {
		suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] synthesize error_code=%s hint=%s", synth.ErrorCode, synth.Hint)
		if synth.ErrorCode == "" {
			t.Fatalf("[E2E-ROBOT-ENSEMBLE] synthesize missing error_code on failure")
		}
	}
}

func TestE2E_RobotEnsemble_AdvancedGating(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	if !supportsRobotFlag("--robot-ensemble-modes") || !supportsRobotFlag("--robot-ensemble-spawn") {
		t.Skip("robot ensemble modes/spawn flags not supported by current ntm binary")
	}

	suite := NewTestSuite(t, "robot_ensemble_advanced")
	defer suite.Teardown()

	output, err := runRobotEnsembleCmd(t, suite, "modes_all_for_advanced", "--robot-ensemble-modes", "--tier=all")
	if err != nil {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_all_for_advanced failed: %v", err)
	}

	var modesAll ensembleModesOutput
	parseRobotJSON(t, suite, "modes_all_for_advanced", output, &modesAll)
	if !modesAll.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] modes_all_for_advanced failed: %s", modesAll.Error)
	}

	advancedID := ""
	for _, mode := range modesAll.Modes {
		if strings.ToLower(mode.Tier) == "advanced" || strings.ToLower(mode.Tier) == "experimental" {
			advancedID = mode.ID
			break
		}
	}
	if advancedID == "" {
		t.Skip("no advanced modes available; skipping advanced gating test")
	}

	agents := GetAvailableAgent()
	if agents == "" {
		t.Skip("no agent CLI available for ensemble spawn")
	}

	failSession := fmt.Sprintf("%s_adv_fail", suite.Session())
	output, err = runRobotEnsembleCmd(t, suite, "spawn_advanced_blocked",
		fmt.Sprintf("--robot-ensemble-spawn=%s", failSession),
		fmt.Sprintf("--modes=%s", advancedID),
		"--question=E2E advanced gating test",
		fmt.Sprintf("--agents=%s=1", agents),
	)
	if err != nil {
		suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] spawn_advanced_blocked command error: %v", err)
	}

	var blocked ensembleSpawnOutput
	parseRobotJSON(t, suite, "spawn_advanced_blocked", output, &blocked)
	if blocked.Success {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] expected advanced gating failure without --allow-advanced")
	}
	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] gating error_code=%s hint=%s", blocked.ErrorCode, blocked.Hint)
	if blocked.ErrorCode == "" {
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] gating failure missing error_code")
	}

	okSession := fmt.Sprintf("%s_adv_ok", suite.Session())
	suite.cleanup = append(suite.cleanup, func() {
		exec.Command(tmux.BinaryPath(), "kill-session", "-t", okSession).Run()
	})

	output, err = runRobotEnsembleCmd(t, suite, "spawn_advanced_allowed",
		fmt.Sprintf("--robot-ensemble-spawn=%s", okSession),
		fmt.Sprintf("--modes=%s", advancedID),
		"--allow-advanced",
		"--question=E2E advanced gating allowed",
		fmt.Sprintf("--agents=%s=1", agents),
	)
	if err != nil {
		suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] spawn_advanced_allowed command error: %v", err)
	}

	var allowed ensembleSpawnOutput
	parseRobotJSON(t, suite, "spawn_advanced_allowed", output, &allowed)
	if !allowed.Success {
		if allowed.ErrorCode == "NOT_IMPLEMENTED" {
			t.Skip("robot ensemble spawn not implemented in current binary")
		}
		t.Fatalf("[E2E-ROBOT-ENSEMBLE] expected advanced spawn success: %s (%s)", allowed.Error, allowed.ErrorCode)
	}
	suite.Logger().Log("[E2E-ROBOT-ENSEMBLE] session=%s agents=%v", allowed.Session, allowed.Agents)
}
