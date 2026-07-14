//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// SwarmPlanResponse represents the JSON output from ntm swarm --dry-run --json
type SwarmPlanResponse struct {
	ScanDir         string                    `json:"scan_dir"`
	TotalCC         int                       `json:"total_cc"`
	TotalCod        int                       `json:"total_cod"`
	TotalGmi        int                       `json:"total_gmi"`
	TotalAgents     int                       `json:"total_agents"`
	SessionsPerType int                       `json:"sessions_per_type"`
	PanesPerSession int                       `json:"panes_per_session"`
	Allocations     []SwarmAllocationResponse `json:"allocations"`
	Sessions        []SwarmSessionResponse    `json:"sessions"`
	DryRun          bool                      `json:"dry_run"`
	Error           string                    `json:"error,omitempty"`
}

// SwarmAllocationResponse represents a project allocation in swarm output
type SwarmAllocationResponse struct {
	Project     string `json:"project"`
	Path        string `json:"path"`
	OpenBeads   int    `json:"open_beads"`
	Tier        int    `json:"tier"`
	CCAgents    int    `json:"cc_agents"`
	CodAgents   int    `json:"cod_agents"`
	GmiAgents   int    `json:"gmi_agents"`
	TotalAgents int    `json:"total_agents"`
}

// SwarmSessionResponse represents a session in swarm output
type SwarmSessionResponse struct {
	Name      string              `json:"name"`
	AgentType string              `json:"agent_type"`
	PaneCount int                 `json:"pane_count"`
	Panes     []SwarmPaneResponse `json:"panes"`
}

// SwarmPaneResponse represents a pane in swarm session output
type SwarmPaneResponse struct {
	Index     int    `json:"index"`
	Project   string `json:"project"`
	AgentType string `json:"agent_type"`
}

// SwarmTestSuite manages E2E swarm test setup and cleanup
type SwarmTestSuite struct {
	t        *testing.T
	logger   *TestLogger
	testDir  string
	sessions []string // sessions to clean up
	cleanup  []func()
}

// NewSwarmTestSuite creates a new swarm test suite
func NewSwarmTestSuite(t *testing.T, scenario string) *SwarmTestSuite {
	logger := NewTestLogger(t, scenario)

	testDir, err := os.MkdirTemp("", "ntm_e2e_swarm_")
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to create temp dir: %v", err)
	}

	s := &SwarmTestSuite{
		t:       t,
		logger:  logger,
		testDir: testDir,
	}

	s.cleanup = append(s.cleanup, func() {
		logger.Log("[E2E-SWARM] Cleaning up test directory: %s", testDir)
		os.RemoveAll(testDir)
	})

	logger.Log("[E2E-SWARM] Created test suite with dir: %s", testDir)
	return s
}

// CreateTestProject creates a test project with beads
func (s *SwarmTestSuite) CreateTestProject(name string, openBeadCount int) string {
	projectDir := filepath.Join(s.testDir, name)
	beadsDir := filepath.Join(projectDir, ".beads")

	s.logger.Log("[E2E-SWARM] Creating test project: %s with %d beads", name, openBeadCount)

	// Create directories
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		s.t.Fatalf("[E2E-SWARM] Failed to create beads dir: %v", err)
	}

	// Create .git directory to make it look like a project
	gitDir := filepath.Join(projectDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		s.t.Fatalf("[E2E-SWARM] Failed to create .git dir: %v", err)
	}

	// Create issues.jsonl with open beads
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	f, err := os.Create(issuesPath)
	if err != nil {
		s.t.Fatalf("[E2E-SWARM] Failed to create issues.jsonl: %v", err)
	}
	defer f.Close()

	for i := 1; i <= openBeadCount; i++ {
		line := fmt.Sprintf(`{"id":"%s-%d","title":"Task %d","status":"open"}`+"\n", name, i, i)
		f.WriteString(line)
	}

	s.logger.Log("[E2E-SWARM] Created project %s at %s", name, projectDir)
	return projectDir
}

// RunSwarmDryRun executes ntm swarm --dry-run --json and returns parsed output
func (s *SwarmTestSuite) RunSwarmDryRun(scanDir string, extraArgs ...string) (*SwarmPlanResponse, error) {
	args := []string{"swarm", "--scan-dir=" + scanDir, "--dry-run", "--json"}
	args = append(args, extraArgs...)

	s.logger.Log("[E2E-SWARM] Running: ntm %s", strings.Join(args, " "))

	cmd := exec.Command(mustE2EBin(), args...)
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-SWARM] Command output (%d bytes): %s", len(output), string(output))

	if err != nil {
		// Check if it's just a "no projects found" error which is expected in some tests
		if strings.Contains(string(output), "no projects found") {
			return nil, fmt.Errorf("no projects found: %s", string(output))
		}
		// Try to parse the output anyway for error info
		var resp SwarmPlanResponse
		if jsonErr := json.Unmarshal(output, &resp); jsonErr == nil && resp.Error != "" {
			return &resp, fmt.Errorf("swarm error: %s", resp.Error)
		}
		return nil, fmt.Errorf("swarm command failed: %w, output: %s", err, string(output))
	}

	var resp SwarmPlanResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w, output: %s", err, string(output))
	}

	s.logger.LogJSON("[E2E-SWARM] Parsed response", resp)
	return &resp, nil
}

// CreateTmuxSession creates a real tmux session for testing
func (s *SwarmTestSuite) CreateTmuxSession(name string) error {
	s.logger.Log("[E2E-SWARM] Creating tmux session: %s", name)

	cmd := exec.Command(tmux.BinaryPath(), "new-session", "-d", "-s", name, "-x", "120", "-y", "30")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create session: %w, output: %s", err, string(output))
	}

	s.sessions = append(s.sessions, name)
	s.cleanup = append(s.cleanup, func() {
		s.logger.Log("[E2E-SWARM] Killing session: %s", name)
		exec.Command(tmux.BinaryPath(), "kill-session", "-t", name).Run()
	})

	return nil
}

// SessionExists checks if a tmux session exists
func (s *SwarmTestSuite) SessionExists(name string) bool {
	cmd := exec.Command(tmux.BinaryPath(), "has-session", "-t", name)
	return cmd.Run() == nil
}

// GetSessionPaneCount returns the number of panes in a session
func (s *SwarmTestSuite) GetSessionPaneCount(name string) (int, error) {
	cmd := exec.Command(tmux.BinaryPath(), "list-panes", "-t", name)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

// AddPane splits a window to add another pane
func (s *SwarmTestSuite) AddPane(session string) error {
	cmd := exec.Command(tmux.BinaryPath(), "split-window", "-t", session)
	return cmd.Run()
}

// KillSession kills a tmux session
func (s *SwarmTestSuite) KillSession(name string) error {
	s.logger.Log("[E2E-SWARM] Killing session: %s", name)
	cmd := exec.Command(tmux.BinaryPath(), "kill-session", "-t", name)
	return cmd.Run()
}

// Teardown cleans up all resources
func (s *SwarmTestSuite) Teardown() {
	s.logger.Log("[E2E-SWARM] Running teardown (%d cleanup items)", len(s.cleanup))

	// Run cleanup in reverse order
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}

	s.logger.Close()
}

// TestE2E_SwarmProjectScanning tests BeadScanner project discovery
func TestE2E_SwarmProjectScanning(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_project_scanning")
	defer suite.Teardown()

	// Create test projects with different bead counts
	suite.CreateTestProject("proj_high", 450)   // Tier 1: ≥400 beads
	suite.CreateTestProject("proj_medium", 150) // Tier 2: ≥100 beads
	suite.CreateTestProject("proj_low", 30)     // Tier 3: <100 beads

	suite.logger.Log("[E2E-SWARM] Test: Verify project scanning discovers all projects")

	// Run swarm dry-run
	resp, err := suite.RunSwarmDryRun(suite.testDir)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm dry-run failed: %v", err)
	}

	// Verify all projects were discovered
	suite.logger.Log("[E2E-SWARM] Found %d allocations", len(resp.Allocations))

	if len(resp.Allocations) != 3 {
		t.Errorf("[E2E-SWARM] Expected 3 projects, got %d", len(resp.Allocations))
	}

	// Verify project names were found
	projectNames := make(map[string]bool)
	for _, alloc := range resp.Allocations {
		projectNames[alloc.Project] = true
		suite.logger.Log("[E2E-SWARM] Found project: %s with %d beads (tier %d)",
			alloc.Project, alloc.OpenBeads, alloc.Tier)
	}

	expectedProjects := []string{"proj_high", "proj_medium", "proj_low"}
	for _, name := range expectedProjects {
		if !projectNames[name] {
			t.Errorf("[E2E-SWARM] Expected project %s not found in allocations", name)
		}
	}

	suite.logger.Log("[E2E-SWARM] PASS: Project scanning discovered all projects")
}

// TestE2E_SwarmAllocationCalculation tests tier-based allocation
func TestE2E_SwarmAllocationCalculation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_allocation_calculation")
	defer suite.Teardown()

	// Create projects with specific bead counts for each tier
	suite.CreateTestProject("tier1_proj", 500) // Tier 1
	suite.CreateTestProject("tier2_proj", 200) // Tier 2
	suite.CreateTestProject("tier3_proj", 50)  // Tier 3

	suite.logger.Log("[E2E-SWARM] Test: Verify allocation calculation based on tiers")

	resp, err := suite.RunSwarmDryRun(suite.testDir)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm dry-run failed: %v", err)
	}

	// Verify allocations by tier
	for _, alloc := range resp.Allocations {
		suite.logger.Log("[E2E-SWARM] Project %s: beads=%d, tier=%d, cc=%d, cod=%d, gmi=%d, total=%d",
			alloc.Project, alloc.OpenBeads, alloc.Tier,
			alloc.CCAgents, alloc.CodAgents, alloc.GmiAgents, alloc.TotalAgents)

		// Verify tier is correctly assigned
		switch alloc.Project {
		case "tier1_proj":
			if alloc.Tier != 1 {
				t.Errorf("[E2E-SWARM] Expected tier 1 for tier1_proj, got %d", alloc.Tier)
			}
			// Tier 1 should have highest allocation
			if alloc.TotalAgents == 0 {
				t.Error("[E2E-SWARM] Tier 1 project should have agents allocated")
			}
		case "tier2_proj":
			if alloc.Tier != 2 {
				t.Errorf("[E2E-SWARM] Expected tier 2 for tier2_proj, got %d", alloc.Tier)
			}
		case "tier3_proj":
			if alloc.Tier != 3 {
				t.Errorf("[E2E-SWARM] Expected tier 3 for tier3_proj, got %d", alloc.Tier)
			}
		}

		// Verify total agents equals sum of agent types
		expectedTotal := alloc.CCAgents + alloc.CodAgents + alloc.GmiAgents
		if alloc.TotalAgents != expectedTotal {
			t.Errorf("[E2E-SWARM] TotalAgents mismatch for %s: got %d, expected %d",
				alloc.Project, alloc.TotalAgents, expectedTotal)
		}
	}

	// Verify aggregate totals
	suite.logger.Log("[E2E-SWARM] Totals: CC=%d, Cod=%d, Gmi=%d, Total=%d",
		resp.TotalCC, resp.TotalCod, resp.TotalGmi, resp.TotalAgents)

	expectedTotal := resp.TotalCC + resp.TotalCod + resp.TotalGmi
	if resp.TotalAgents != expectedTotal {
		t.Errorf("[E2E-SWARM] Total agents mismatch: got %d, expected %d",
			resp.TotalAgents, expectedTotal)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Allocation calculation is correct")
}

// TestE2E_SwarmSessionPlanning tests session generation
func TestE2E_SwarmSessionPlanning(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_session_planning")
	defer suite.Teardown()

	// Create a project with enough beads to generate sessions
	suite.CreateTestProject("main_proj", 100)

	suite.logger.Log("[E2E-SWARM] Test: Verify session planning and naming")

	resp, err := suite.RunSwarmDryRun(suite.testDir, "--sessions-per-type=2")
	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm dry-run failed: %v", err)
	}

	suite.logger.Log("[E2E-SWARM] Sessions planned: %d", len(resp.Sessions))
	suite.logger.Log("[E2E-SWARM] Sessions per type: %d, Panes per session: %d",
		resp.SessionsPerType, resp.PanesPerSession)

	// Verify session naming convention
	for _, sess := range resp.Sessions {
		suite.logger.Log("[E2E-SWARM] Session: %s (type=%s, panes=%d)",
			sess.Name, sess.AgentType, sess.PaneCount)

		// Check session name follows pattern: {type}_agents_{num}
		validPrefixes := []string{"cc_agents_", "cod_agents_", "gmi_agents_"}
		hasValidPrefix := false
		for _, prefix := range validPrefixes {
			if strings.HasPrefix(sess.Name, prefix) {
				hasValidPrefix = true
				break
			}
		}

		if !hasValidPrefix {
			t.Errorf("[E2E-SWARM] Invalid session name format: %s", sess.Name)
		}

		// Verify pane count matches panes array length
		if sess.PaneCount != len(sess.Panes) {
			t.Errorf("[E2E-SWARM] Pane count mismatch for %s: count=%d, panes=%d",
				sess.Name, sess.PaneCount, len(sess.Panes))
		}
	}

	// Verify sessions_per_type was respected
	if resp.SessionsPerType != 2 {
		t.Errorf("[E2E-SWARM] Expected sessions_per_type=2, got %d", resp.SessionsPerType)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Session planning is correct")
}

// TestE2E_SwarmRealSessionCreation tests actual tmux session creation
func TestE2E_SwarmRealSessionCreation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_real_session")
	defer suite.Teardown()

	testSession := fmt.Sprintf("e2e_swarm_test_%d", time.Now().Unix())

	suite.logger.Log("[E2E-SWARM] Test: Real tmux session creation and management")

	// Create a test session
	err := suite.CreateTmuxSession(testSession)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to create session: %v", err)
	}

	// Verify session exists
	if !suite.SessionExists(testSession) {
		t.Fatal("[E2E-SWARM] Session should exist after creation")
	}
	suite.logger.Log("[E2E-SWARM] Session %s created and verified", testSession)

	// Add panes
	for i := 0; i < 2; i++ {
		if err := suite.AddPane(testSession); err != nil {
			t.Fatalf("[E2E-SWARM] Failed to add pane: %v", err)
		}
	}

	// Verify pane count
	paneCount, err := suite.GetSessionPaneCount(testSession)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to get pane count: %v", err)
	}

	suite.logger.Log("[E2E-SWARM] Session has %d panes", paneCount)
	if paneCount < 3 {
		t.Errorf("[E2E-SWARM] Expected at least 3 panes, got %d", paneCount)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Real session creation works")
}

// TestE2E_SwarmGracefulShutdown tests session cleanup
func TestE2E_SwarmGracefulShutdown(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_graceful_shutdown")
	defer suite.Teardown()

	testSession := fmt.Sprintf("e2e_swarm_shutdown_%d", time.Now().Unix())

	suite.logger.Log("[E2E-SWARM] Test: Graceful session shutdown")

	// Create session
	err := suite.CreateTmuxSession(testSession)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to create session: %v", err)
	}

	// Verify it exists
	if !suite.SessionExists(testSession) {
		t.Fatal("[E2E-SWARM] Session should exist before shutdown")
	}

	// Kill the session
	err = suite.KillSession(testSession)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to kill session: %v", err)
	}

	// Give tmux a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify it's gone
	if suite.SessionExists(testSession) {
		t.Error("[E2E-SWARM] Session should not exist after shutdown")
	}

	suite.logger.Log("[E2E-SWARM] PASS: Graceful shutdown works")
}

// TestE2E_SwarmNoProjectsFound tests handling when no projects exist
func TestE2E_SwarmNoProjectsFound(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_no_projects")
	defer suite.Teardown()

	// Create an empty subdirectory (no projects)
	emptyDir := filepath.Join(suite.testDir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("[E2E-SWARM] Failed to create empty dir: %v", err)
	}

	suite.logger.Log("[E2E-SWARM] Test: Handling empty scan directory")

	// This should fail gracefully
	_, err := suite.RunSwarmDryRun(emptyDir)
	if err == nil {
		t.Error("[E2E-SWARM] Expected error for empty directory")
	} else {
		suite.logger.Log("[E2E-SWARM] Got expected error: %v", err)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Empty directory handled correctly")
}

// TestE2E_SwarmExplicitProjects tests --projects flag
func TestE2E_SwarmExplicitProjects(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_explicit_projects")
	defer suite.Teardown()

	// Create multiple projects
	proj1 := suite.CreateTestProject("explicit_proj1", 100)
	suite.CreateTestProject("explicit_proj2", 50)
	suite.CreateTestProject("explicit_proj3", 25)

	suite.logger.Log("[E2E-SWARM] Test: Using --projects to specify explicit projects")

	// Only include proj1
	resp, err := suite.RunSwarmDryRun(suite.testDir, "--projects="+proj1)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm dry-run failed: %v", err)
	}

	// Should only have one project
	if len(resp.Allocations) != 1 {
		t.Errorf("[E2E-SWARM] Expected 1 allocation with explicit project, got %d", len(resp.Allocations))
	}

	if len(resp.Allocations) > 0 && resp.Allocations[0].Project != "explicit_proj1" {
		t.Errorf("[E2E-SWARM] Expected explicit_proj1, got %s", resp.Allocations[0].Project)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Explicit projects filter works")
}

// TestE2E_SwarmPlanOutputFile tests --output flag for plan file
func TestE2E_SwarmPlanOutputFile(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_plan_output")
	defer suite.Teardown()

	suite.CreateTestProject("output_proj", 100)
	outputPath := filepath.Join(suite.testDir, "swarm_plan.json")

	suite.logger.Log("[E2E-SWARM] Test: Writing plan to output file")

	// Run with --output flag
	cmd := exec.Command(mustE2EBin(), "swarm",
		"--scan-dir="+suite.testDir,
		"--dry-run",
		"--output="+outputPath)

	output, err := cmd.CombinedOutput()
	suite.logger.Log("[E2E-SWARM] Command output: %s", string(output))

	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm command failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Fatal("[E2E-SWARM] Output file was not created")
	}

	// Read and parse the file
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Failed to read output file: %v", err)
	}

	var plan map[string]interface{}
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("[E2E-SWARM] Output file is not valid JSON: %v", err)
	}

	suite.logger.Log("[E2E-SWARM] Output file contains valid JSON with %d keys", len(plan))

	// Verify essential fields exist
	essentialFields := []string{"scan_dir", "allocations", "sessions"}
	for _, field := range essentialFields {
		if _, ok := plan[field]; !ok {
			t.Errorf("[E2E-SWARM] Output file missing essential field: %s", field)
		}
	}

	suite.logger.Log("[E2E-SWARM] PASS: Plan output file works correctly")
}

// TestE2E_SwarmProjectSorting tests that projects are sorted by bead count
func TestE2E_SwarmProjectSorting(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewSwarmTestSuite(t, "swarm_project_sorting")
	defer suite.Teardown()

	// Create projects in non-sorted order
	suite.CreateTestProject("proj_small", 10)
	suite.CreateTestProject("proj_large", 500)
	suite.CreateTestProject("proj_medium", 200)

	suite.logger.Log("[E2E-SWARM] Test: Verify projects are sorted by bead count descending")

	resp, err := suite.RunSwarmDryRun(suite.testDir)
	if err != nil {
		t.Fatalf("[E2E-SWARM] Swarm dry-run failed: %v", err)
	}

	// Verify sorting: should be large, medium, small
	if len(resp.Allocations) != 3 {
		t.Fatalf("[E2E-SWARM] Expected 3 allocations, got %d", len(resp.Allocations))
	}

	// Check descending order
	for i := 0; i < len(resp.Allocations)-1; i++ {
		if resp.Allocations[i].OpenBeads < resp.Allocations[i+1].OpenBeads {
			t.Errorf("[E2E-SWARM] Projects not sorted correctly: %s (%d) < %s (%d)",
				resp.Allocations[i].Project, resp.Allocations[i].OpenBeads,
				resp.Allocations[i+1].Project, resp.Allocations[i+1].OpenBeads)
		}
		suite.logger.Log("[E2E-SWARM] Allocation %d: %s (%d beads)",
			i+1, resp.Allocations[i].Project, resp.Allocations[i].OpenBeads)
	}

	suite.logger.Log("[E2E-SWARM] PASS: Projects correctly sorted by bead count")
}

type beadClaimProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type beadClaimProcessEnvelope struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code"`
	BeadID    string `json:"bead_id"`
	NewStatus string `json:"new_status"`
	Claimed   bool   `json:"claimed"`
	Actor     string `json:"actor"`
}

type beadClaimProcessState struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func TestE2ERobotBeadClaimBuiltBinaryRealBR(t *testing.T) {
	SkipIfShort(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("br is required for robot bead-claim E2E: %v", err)
	}

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	cacheDir := filepath.Join(root, "cache")
	for _, dir := range []string{projectDir, homeDir, configDir, dataDir, cacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create isolated directory %s: %v", dir, err)
		}
	}

	env := beadClaimProcessEnv(map[string]string{
		"HOME":                homeDir,
		"XDG_CONFIG_HOME":     configDir,
		"XDG_DATA_HOME":       dataDir,
		"XDG_CACHE_HOME":      cacheDir,
		"PWD":                 projectDir,
		"NO_COLOR":            "1",
		"NTM_CONFIG":          "",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "json",
		"TOON_DEFAULT_FORMAT": "",
	})

	mustRunBeadClaimBR(t, brPath, projectDir, env, "init", "--prefix=claim-e2e", "--json")
	beadID := strings.TrimSpace(string(mustRunBeadClaimBR(
		t, brPath, projectDir, env,
		"create", "Robot bead claim process E2E", "--type=task", "--priority=1", "--silent",
	)))
	if beadID == "" || strings.ContainsAny(beadID, " \t\r\n") {
		t.Fatalf("unexpected br create output %q", beadID)
	}
	assertBeadClaimProcessState(t, readBeadClaimProcessState(t, brPath, projectDir, env, beadID), beadID, "open", "")

	const firstActor = "BlueLake"
	first := runBeadClaimProcess(t, ntmPath, projectDir, env,
		"--robot-bead-claim="+beadID, "--bead-assignee="+firstActor, "--robot-format=json")
	if first.exitCode != 0 || len(bytes.TrimSpace(first.stderr)) != 0 {
		t.Fatalf("initial claim exit=%d stdout=%s stderr=%s", first.exitCode, first.stdout, first.stderr)
	}
	firstEnvelope := decodeBeadClaimProcessEnvelope(t, first.stdout)
	assertBeadClaimProcessSuccess(t, firstEnvelope, beadID, firstActor)
	claimedState := readBeadClaimProcessState(t, brPath, projectDir, env, beadID)
	assertBeadClaimProcessState(t, claimedState, beadID, "in_progress", firstActor)

	repeat := runBeadClaimProcess(t, ntmPath, projectDir, env,
		"--robot-bead-claim="+beadID, "--bead-assignee="+firstActor, "--robot-format=json")
	if repeat.exitCode != 0 || len(bytes.TrimSpace(repeat.stderr)) != 0 {
		t.Fatalf("same-actor repeat exit=%d stdout=%s stderr=%s", repeat.exitCode, repeat.stdout, repeat.stderr)
	}
	repeatEnvelope := decodeBeadClaimProcessEnvelope(t, repeat.stdout)
	assertBeadClaimProcessSuccess(t, repeatEnvelope, beadID, firstActor)
	repeatedState := readBeadClaimProcessState(t, brPath, projectDir, env, beadID)
	assertBeadClaimProcessState(t, repeatedState, beadID, "in_progress", firstActor)
	if repeatedState != claimedState {
		t.Fatalf("same-actor repeat changed semantic bead state: before=%+v after=%+v", claimedState, repeatedState)
	}

	const otherActor = "RedStone"
	conflict := runBeadClaimProcess(t, ntmPath, projectDir, env,
		"--robot-bead-claim="+beadID, "--bead-assignee="+otherActor, "--robot-format=json")
	if conflict.exitCode != 1 {
		t.Errorf("other-actor conflict exit=%d, want 1; stdout=%s stderr=%s", conflict.exitCode, conflict.stdout, conflict.stderr)
	}
	if len(bytes.TrimSpace(conflict.stderr)) != 0 {
		t.Errorf("other-actor conflict stderr=%q, want empty", conflict.stderr)
	}
	conflictEnvelope := decodeBeadClaimProcessEnvelope(t, conflict.stdout)
	if conflictEnvelope.Success || conflictEnvelope.Claimed || conflictEnvelope.ErrorCode != "RESOURCE_BUSY" ||
		conflictEnvelope.BeadID != beadID || conflictEnvelope.Actor != otherActor {
		t.Errorf("other-actor conflict envelope=%+v", conflictEnvelope)
	}
	conflictState := readBeadClaimProcessState(t, brPath, projectDir, env, beadID)
	assertBeadClaimProcessState(t, conflictState, beadID, "in_progress", firstActor)
	if conflictState != claimedState {
		t.Fatalf("other-actor conflict changed semantic bead state: before=%+v after=%+v", claimedState, conflictState)
	}
}

func runBeadClaimProcess(t *testing.T, ntmPath, dir string, env []string, args ...string) beadClaimProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ntmPath, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm bead-claim timed out: args=%q stdout=%s stderr=%s", args, stdout.Bytes(), stderr.Bytes())
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run ntm bead-claim %q: %v", args, err)
		}
	}
	return beadClaimProcessResult{
		stdout:   append([]byte(nil), stdout.Bytes()...),
		stderr:   append([]byte(nil), stderr.Bytes()...),
		exitCode: exitCode,
	}
}

func decodeBeadClaimProcessEnvelope(t *testing.T, output []byte) beadClaimProcessEnvelope {
	t.Helper()
	var envelope beadClaimProcessEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("decode exactly one ntm bead-claim JSON document: %v; output=%q", err, output)
	}
	return envelope
}

func assertBeadClaimProcessSuccess(t *testing.T, envelope beadClaimProcessEnvelope, beadID, actor string) {
	t.Helper()
	if !envelope.Success || !envelope.Claimed || envelope.ErrorCode != "" || envelope.BeadID != beadID ||
		envelope.NewStatus != "in_progress" || envelope.Actor != actor {
		t.Fatalf("bead-claim success envelope=%+v", envelope)
	}
}

func readBeadClaimProcessState(t *testing.T, brPath, dir string, env []string, beadID string) beadClaimProcessState {
	t.Helper()
	output := mustRunBeadClaimBR(t, brPath, dir, env, "show", beadID, "--json")
	var rows []beadClaimProcessState
	if err := json.Unmarshal(output, &rows); err != nil {
		var row beadClaimProcessState
		if objectErr := json.Unmarshal(output, &row); objectErr != nil {
			t.Fatalf("decode br show state: array=%v object=%v output=%q", err, objectErr, output)
		}
		rows = []beadClaimProcessState{row}
	}
	if len(rows) != 1 {
		t.Fatalf("br show %s returned %d rows: %s", beadID, len(rows), output)
	}
	return rows[0]
}

func assertBeadClaimProcessState(t *testing.T, state beadClaimProcessState, beadID, status, assignee string) {
	t.Helper()
	if state.ID != beadID || state.Status != status || state.Assignee != assignee {
		t.Fatalf("bead state=%+v, want id=%s status=%s assignee=%s", state, beadID, status, assignee)
	}
}

func mustRunBeadClaimBR(t *testing.T, brPath, dir string, env []string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, brPath, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("br command timed out: %q", args)
	}
	if err != nil {
		t.Fatalf("br %q: %v output=%s", args, err, output)
	}
	return output
}

func beadClaimProcessEnv(overrides map[string]string) []string {
	replaced := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {}, "XDG_CACHE_HOME": {}, "PWD": {}, "OLDPWD": {},
		"GIT_DIR": {}, "GIT_WORK_TREE": {}, "BR_DB": {}, "BD_DB": {}, "BEADS_DB": {},
		"TMUX": {}, "TMUX_PANE": {}, "NTM_CONFIG": {}, "NTM_OUTPUT_FORMAT": {}, "NTM_ROBOT_FORMAT": {},
		"TOON_DEFAULT_FORMAT": {}, "AGENT_NAME": {},
	}
	for key := range overrides {
		replaced[key] = struct{}{}
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := replaced[key]; !skip {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
