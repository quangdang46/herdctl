//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

const (
	spawnAssignmentProjectID     = 77
	spawnAssignmentAgentID       = 88
	spawnAssignmentReservationID = 701
	spawnAssignmentPath          = "internal/robot/**"
	spawnAssignmentRecipient     = "ExactRecipient"
	spawnAssignmentDisplayName   = "DisplayAlias"
	spawnAssignmentToken         = "e2e-registration-token"
)

// TestE2ESpawnAssignmentProductionCLI crosses the complete production spawn
// assignment path: a built ntm process, private real tmux server, real br
// database, persisted pane identity, concrete Agent Mail HTTP client, atomic
// ledger, and tmux prompt delivery. The replay is a second OS process.
func TestE2ESpawnAssignmentProductionCLI(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newSpawnAssignmentCLIFixture(t)
	args := fixture.spawnArgs()

	firstResult := fixture.runNTM(t, args...)
	first := decodeSpawnAssignmentOutput(t, firstResult)
	firstAssignment := assertSpawnAssignmentOutput(t, first, fixture)
	fixture.waitForMarkerCount(t, 1)
	fixture.assertBead(t, "in_progress", firstAssignment.ClaimActor)

	firstRecord := fixture.readAssignment(t)
	assertSpawnAssignmentRecord(t, firstRecord, firstAssignment, fixture)
	firstMCPCounts := fixture.stub.assertCleanMutationCount(t, 1)

	firstDispatchedAt := *firstRecord.DispatchedAt
	firstReservationExpiresAt := *firstRecord.ReservationExpiresAt

	// Re-run the identical spawn intent from a fresh process. The existing
	// session is intentionally reused; its launch command is harmless input to
	// the fake agent, while the work marker must not be delivered again.
	secondResult := fixture.runNTM(t, args...)
	second := decodeSpawnAssignmentOutput(t, secondResult)
	secondAssignment := assertSpawnAssignmentOutput(t, second, fixture)

	if secondAssignment.IdempotencyKey != firstAssignment.IdempotencyKey ||
		secondAssignment.ClaimActor != firstAssignment.ClaimActor ||
		secondAssignment.DispatchReceiptID != firstAssignment.DispatchReceiptID ||
		!equalInts(secondAssignment.ReservationIDs, firstAssignment.ReservationIDs) {
		t.Fatalf("spawn replay identity changed: first=%+v second=%+v", firstAssignment, secondAssignment)
	}
	fixture.assertMarkerCount(t, 1)
	fixture.assertBead(t, "in_progress", firstAssignment.ClaimActor)
	secondMCPCounts := fixture.stub.assertCleanMutationCount(t, 1)
	if secondMCPCounts.ensure < firstMCPCounts.ensure || secondMCPCounts.list < firstMCPCounts.list {
		t.Fatalf("Agent Mail discovery counters moved backwards: first=%+v second=%+v", firstMCPCounts, secondMCPCounts)
	}

	replayed := fixture.readAssignment(t)
	if replayed.ClaimAttempts != 1 || replayed.ReservationAttempts != 1 || replayed.DispatchAttempts != 1 ||
		replayed.IdempotencyKey != firstRecord.IdempotencyKey ||
		replayed.DispatchReceiptID != firstRecord.DispatchReceiptID ||
		replayed.DispatchedAt == nil || !replayed.DispatchedAt.Equal(firstDispatchedAt) ||
		replayed.ReservationExpiresAt == nil || !replayed.ReservationExpiresAt.Equal(firstReservationExpiresAt) {
		t.Fatalf("spawn replay mutated durable side-effect receipts: before=%+v after=%+v", firstRecord, replayed)
	}
}

type spawnAssignmentCLIFixture struct {
	ntmPath        string
	tmuxPath       string
	brPath         string
	session        string
	projectDir     string
	homeDir        string
	configDir      string
	env            []string
	paneID         string
	paneIndex      int
	beadID         string
	beadTitle      string
	marker         string
	expectedPrompt string
	stub           *spawnAssignmentMCPStub
}

type spawnAssignmentProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type spawnAssignmentOutput struct {
	Success        bool                      `json:"success"`
	Timestamp      string                    `json:"timestamp"`
	Session        string                    `json:"session"`
	WorkingDir     string                    `json:"working_dir"`
	Agents         []spawnAssignmentAgent    `json:"agents"`
	Mode           string                    `json:"mode"`
	AssignStrategy string                    `json:"assign_strategy"`
	Assignments    []spawnAssignmentResponse `json:"assignments"`
	Error          string                    `json:"error"`
	ErrorCode      string                    `json:"error_code"`
}

type spawnAssignmentAgent struct {
	Pane  string `json:"pane"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Ready bool   `json:"ready"`
	Error string `json:"error"`
}

type spawnAssignmentResponse struct {
	Pane              string `json:"pane"`
	AgentType         string `json:"agent_type"`
	BeadID            string `json:"bead_id"`
	BeadTitle         string `json:"bead_title"`
	Priority          string `json:"priority"`
	Claimed           bool   `json:"claimed"`
	PromptSent        bool   `json:"prompt_sent"`
	ClaimActor        string `json:"claim_actor"`
	IdempotencyKey    string `json:"idempotency_key"`
	DispatchReceiptID string `json:"dispatch_receipt_id"`
	ReservationIDs    []int  `json:"reservation_ids"`
	ClaimError        string `json:"claim_error"`
	PromptError       string `json:"prompt_error"`
}

type spawnAssignmentLedger struct {
	SessionName string                            `json:"session_name"`
	Assignments map[string]*spawnAssignmentRecord `json:"assignments"`
	Version     int                               `json:"version"`
}

type spawnAssignmentRecord struct {
	BeadID                string     `json:"bead_id"`
	BeadTitle             string     `json:"bead_title"`
	Pane                  int        `json:"pane"`
	AgentType             string     `json:"agent_type"`
	AgentName             string     `json:"agent_name"`
	Status                string     `json:"status"`
	PromptSent            string     `json:"prompt_sent"`
	IdempotencyKey        string     `json:"idempotency_key"`
	ClaimActor            string     `json:"claim_actor"`
	ClaimState            string     `json:"claim_state"`
	ClaimStatus           string     `json:"claim_status"`
	ClaimAttempts         int        `json:"claim_attempts"`
	ClaimedAt             *time.Time `json:"claimed_at"`
	ReservationRequired   bool       `json:"reservation_required"`
	ReservationInputPaths []string   `json:"reservation_input_paths"`
	ReservationState      string     `json:"reservation_state"`
	ReservationAttempts   int        `json:"reservation_attempts"`
	ReservationCompleted  bool       `json:"reservation_completed"`
	ReservationAgent      string     `json:"reservation_agent"`
	ReservationTarget     string     `json:"reservation_target"`
	ReservationRequested  []string   `json:"reservation_requested"`
	ReservedPaths         []string   `json:"reserved_paths"`
	ReservationIDs        []int      `json:"reservation_ids"`
	ReservationExpiresAt  *time.Time `json:"reservation_expires_at"`
	ReservationError      string     `json:"reservation_error"`
	DispatchState         string     `json:"dispatch_state"`
	DispatchTarget        string     `json:"dispatch_target"`
	OccupancyKey          string     `json:"occupancy_key"`
	PendingPrompt         string     `json:"pending_prompt"`
	DispatchAttempts      int        `json:"dispatch_attempts"`
	DispatchStartedAt     *time.Time `json:"dispatch_started_at"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	DispatchReceiptID     string     `json:"dispatch_receipt_id"`
}

type spawnAssignmentBead struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func newSpawnAssignmentCLIFixture(t *testing.T) *spawnAssignmentCLIFixture {
	t.Helper()

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("br is required for spawn assignment E2E: %v", err)
	}

	root := t.TempDir()
	fixture := &spawnAssignmentCLIFixture{
		ntmPath:    ntmPath,
		tmuxPath:   tmuxPath,
		brPath:     brPath,
		session:    fmt.Sprintf("ntm-e2e-spawn-assign-%d-%d", os.Getpid(), time.Now().UnixNano()),
		projectDir: filepath.Join(root, "project"),
		homeDir:    filepath.Join(root, "home"),
		configDir:  filepath.Join(root, "config"),
		marker:     fmt.Sprintf("NTM_SPAWN_ASSIGN_%d", time.Now().UnixNano()),
	}
	fixture.beadTitle = fixture.marker

	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{
		fixture.projectDir,
		fixture.homeDir,
		fixture.configDir,
		filepath.Join(root, "data"),
		filepath.Join(root, "tmux"),
		fakeBin,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}

	fixture.env = spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                fixture.homeDir,
		"XDG_CONFIG_HOME":     fixture.configDir,
		"XDG_DATA_HOME":       filepath.Join(root, "data"),
		"TMUX_TMPDIR":         filepath.Join(root, "tmux"),
		"PATH":                fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AGENT_MAIL_URL":      "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":    "",
		"HTTP_PROXY":          "",
		"HTTPS_PROXY":         "",
		"ALL_PROXY":           "",
		"NO_PROXY":            "127.0.0.1,localhost",
		"NO_COLOR":            "1",
		"TERM":                "xterm-256color",
		"NTM_CONFIG":          "",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "",
		"TOON_DEFAULT_FORMAT": "",
	})

	writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))
	fixture.mustBR(t, "init", "--prefix=spawne2e", "--json")
	fixture.beadID = strings.TrimSpace(string(fixture.mustBR(
		t, "create", fixture.beadTitle, "--type=task", "--priority=1", "--silent",
	)))
	if fixture.beadID == "" || strings.ContainsAny(fixture.beadID, " \t\r\n") {
		t.Fatalf("unexpected br create output %q", fixture.beadID)
	}
	fixture.assertBead(t, "open", "")
	writeSpawnFakeBV(t, filepath.Join(fakeBin, "bv"), fixture.beadID, fixture.beadTitle)

	fixture.stub = &spawnAssignmentMCPStub{
		projectDir: fixture.projectDir,
		beadID:     fixture.beadID,
		recipient:  spawnAssignmentRecipient,
		path:       spawnAssignmentPath,
		token:      spawnAssignmentToken,
	}
	server := httptest.NewUnstartedServer(fixture.stub)
	server.Config.ReadHeaderTimeout = 2 * time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 2 * time.Second
	server.Config.IdleTimeout = 2 * time.Second
	server.Start()
	t.Cleanup(server.Close)
	fixture.env = spawnAssignmentMergeEnv(fixture.env, map[string]string{
		"AGENT_MAIL_URL": server.URL + "/mcp/",
	})

	tmuxConfig := filepath.Join(root, "tmux.conf")
	tmuxConfigBody := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g renumber-windows off",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(tmuxConfig, []byte(tmuxConfigBody), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	fixture.mustTMUX(t, "-f", tmuxConfig, "new-session", "-d", "-s", fixture.session,
		"-x", "160", "-y", "48", "-c", fixture.projectDir, "bash --noprofile --norc")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = fixture.runTMUX(ctx, "kill-server")
	})
	fixture.waitForInitialPane(t)
	fixture.seedAgentRegistry(t)
	fixture.expectedPrompt = expectedSpawnWorkPrompt(fixture.beadID, fixture.beadTitle)

	return fixture
}

func (f *spawnAssignmentCLIFixture) spawnArgs() []string {
	return []string{
		"--robot-format=json",
		"--robot-spawn=" + f.session,
		"--spawn-cc=1",
		"--spawn-no-user",
		"--spawn-dir=" + f.projectDir,
		"--spawn-wait",
		"--timeout=8s",
		"--spawn-assign-work",
		"--strategy=top-n",
		"--spawn-names=" + spawnAssignmentDisplayName,
		"--require-reservation",
		"--reservation-paths=" + spawnAssignmentPath,
	}
}

func (f *spawnAssignmentCLIFixture) runNTM(t *testing.T, args ...string) spawnAssignmentProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.ntmPath, args...)
	cmd.Dir = f.projectDir
	cmd.Env = append([]string(nil), f.env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm spawn assignment timed out: %q", args)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run ntm spawn assignment: %v", err)
		}
	}
	t.Logf("[E2E-SPAWN-ASSIGN] exit=%d stdout=%s stderr=%s", exitCode,
		truncateString(stdout.String(), 800), truncateString(stderr.String(), 800))
	return spawnAssignmentProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func decodeSpawnAssignmentOutput(t *testing.T, result spawnAssignmentProcessResult) spawnAssignmentOutput {
	t.Helper()
	if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("spawn assignment exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	var output spawnAssignmentOutput
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(result.stdout)))
	if err := decoder.Decode(&output); err != nil {
		t.Fatalf("decode spawn JSON: %v raw=%s", err, result.stdout)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("spawn output contains trailing data: err=%v raw=%s", err, result.stdout)
	}
	return output
}

func assertSpawnAssignmentOutput(t *testing.T, output spawnAssignmentOutput, f *spawnAssignmentCLIFixture) spawnAssignmentResponse {
	t.Helper()
	if !output.Success || output.Timestamp == "" || output.Session != f.session ||
		output.WorkingDir != f.projectDir || output.Mode != "orchestrator" ||
		output.AssignStrategy != "top-n" || output.Error != "" || output.ErrorCode != "" {
		t.Fatalf("spawn output envelope = %+v", output)
	}
	if len(output.Agents) != 1 {
		t.Fatalf("spawn agents = %+v", output.Agents)
	}
	agent := output.Agents[0]
	wantTitle := f.session + "__cc_1"
	if agent.Pane != fmt.Sprintf("0.%d", f.paneIndex) || agent.Name != spawnAssignmentDisplayName ||
		agent.Type != "claude" || agent.Title != wantTitle || !agent.Ready || agent.Error != "" {
		t.Fatalf("spawn agent = %+v", agent)
	}
	if len(output.Assignments) != 1 {
		t.Fatalf("spawn assignments = %+v", output.Assignments)
	}
	assignment := output.Assignments[0]
	if assignment.Pane != fmt.Sprint(f.paneIndex) || assignment.AgentType != "claude" ||
		assignment.BeadID != f.beadID || assignment.BeadTitle != f.beadTitle || assignment.Priority != "P1" ||
		!assignment.Claimed || !assignment.PromptSent || assignment.ClaimActor == "" ||
		assignment.IdempotencyKey == "" || assignment.DispatchReceiptID == "" ||
		!equalInts(assignment.ReservationIDs, []int{spawnAssignmentReservationID}) ||
		assignment.ClaimError != "" || assignment.PromptError != "" {
		t.Fatalf("spawn assignment = %+v", assignment)
	}
	decodedKey, err := hex.DecodeString(assignment.IdempotencyKey)
	if err != nil || len(decodedKey) != 32 {
		t.Fatalf("idempotency key %q is not 256-bit hex: bytes=%d err=%v", assignment.IdempotencyKey, len(decodedKey), err)
	}
	wantActor := spawnAssignmentRecipient + "/ntm-" + assignment.IdempotencyKey[:12]
	if assignment.ClaimActor != wantActor {
		t.Fatalf("claim actor = %q, want exact registered identity %q", assignment.ClaimActor, wantActor)
	}
	if !strings.Contains(assignment.DispatchReceiptID, f.paneID) {
		t.Fatalf("dispatch receipt %q does not identify stable pane %q", assignment.DispatchReceiptID, f.paneID)
	}
	return assignment
}

func assertSpawnAssignmentRecord(t *testing.T, record *spawnAssignmentRecord, response spawnAssignmentResponse, f *spawnAssignmentCLIFixture) {
	t.Helper()
	if record.BeadID != f.beadID || record.BeadTitle != f.beadTitle || record.Pane != f.paneIndex ||
		record.AgentType != "claude" || record.AgentName != spawnAssignmentRecipient || record.Status != "assigned" ||
		record.PromptSent != f.expectedPrompt || record.IdempotencyKey != response.IdempotencyKey ||
		record.ClaimActor != response.ClaimActor || record.ClaimState != "claimed" ||
		record.ClaimStatus != "in_progress" || record.ClaimAttempts != 1 || record.ClaimedAt == nil {
		t.Fatalf("durable claim identity/state = %+v", record)
	}
	if !record.ReservationRequired || !equalStrings(record.ReservationInputPaths, []string{spawnAssignmentPath}) ||
		record.ReservationState != "reserved" || record.ReservationAttempts != 1 || !record.ReservationCompleted ||
		record.ReservationAgent != spawnAssignmentRecipient || record.ReservationTarget != f.paneID ||
		!equalStrings(record.ReservationRequested, []string{spawnAssignmentPath}) ||
		!equalStrings(record.ReservedPaths, []string{spawnAssignmentPath}) ||
		!equalInts(record.ReservationIDs, []int{spawnAssignmentReservationID}) ||
		record.ReservationExpiresAt == nil || !record.ReservationExpiresAt.After(time.Now()) || record.ReservationError != "" {
		t.Fatalf("durable reservation receipt = %+v", record)
	}
	if record.DispatchState != "sent" || record.DispatchTarget != f.paneID || record.OccupancyKey != f.paneID ||
		record.PendingPrompt != "" || record.DispatchAttempts != 1 || record.DispatchStartedAt == nil ||
		record.DispatchedAt == nil || record.DispatchReceiptID != response.DispatchReceiptID {
		t.Fatalf("durable dispatch receipt = %+v", record)
	}
	if record.ClaimedAt.After(*record.DispatchStartedAt) || record.DispatchStartedAt.After(*record.DispatchedAt) {
		t.Fatalf("claim-reserve-dispatch order violated: claim=%s dispatch-start=%s dispatched=%s",
			record.ClaimedAt, record.DispatchStartedAt, record.DispatchedAt)
	}
}

func (f *spawnAssignmentCLIFixture) seedAgentRegistry(t *testing.T) {
	t.Helper()
	registry := agentmail.NewSessionAgentRegistry(f.session, f.projectDir)
	registry.AddAgent(f.session+"__cc_1", f.paneID, spawnAssignmentRecipient)
	registry.SetRegistrationToken(spawnAssignmentRecipient, spawnAssignmentToken)
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatalf("marshal Agent Mail pane registry: %v", err)
	}
	path := filepath.Join(f.configDir, "ntm", "sessions", f.session,
		agentmail.ProjectSlugFromPath(f.projectDir), "agent_registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create Agent Mail registry directory: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write Agent Mail pane registry: %v", err)
	}
}

func (f *spawnAssignmentCLIFixture) waitForInitialPane(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		output, err := f.tmuxOutput(ctx, "list-panes", "-t", f.session,
			"-F", "#{window_index}|#{pane_index}|#{pane_id}")
		cancel()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(output)), "|")
			var window int
			if len(parts) == 3 {
				if _, scanErr := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &window, &f.paneIndex); scanErr == nil &&
					window == 0 && strings.HasPrefix(parts[2], "%") {
					f.paneID = parts[2]
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("private tmux session did not expose its initial pane")
}

func (f *spawnAssignmentCLIFixture) waitForMarkerCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.markerCount(t) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	f.assertMarkerCount(t, want)
}

func (f *spawnAssignmentCLIFixture) assertMarkerCount(t *testing.T, want int) {
	t.Helper()
	if got := f.markerCount(t); got != want {
		t.Fatalf("work prompt marker count = %d, want %d; pane=%q", got, want, f.capturePane(t))
	}
}

func (f *spawnAssignmentCLIFixture) markerCount(t *testing.T) int {
	t.Helper()
	return strings.Count(f.capturePane(t), f.marker)
}

func (f *spawnAssignmentCLIFixture) capturePane(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := f.tmuxOutput(ctx, "capture-pane", "-p", "-t", f.paneID, "-S", "-2000")
	if err != nil {
		t.Fatalf("capture fake spawned agent: %v", err)
	}
	return string(output)
}

func (f *spawnAssignmentCLIFixture) assertBead(t *testing.T, wantStatus, wantAssignee string) {
	t.Helper()
	output := f.mustBR(t, "show", f.beadID, "--json")
	var rows []spawnAssignmentBead
	if err := json.Unmarshal(output, &rows); err != nil {
		var row spawnAssignmentBead
		if objectErr := json.Unmarshal(output, &row); objectErr != nil {
			t.Fatalf("decode br show: array=%v object=%v raw=%s", err, objectErr, output)
		}
		rows = []spawnAssignmentBead{row}
	}
	if len(rows) != 1 || rows[0].ID != f.beadID || rows[0].Status != wantStatus || rows[0].Assignee != wantAssignee {
		t.Fatalf("bead state = %+v, want id=%s status=%s assignee=%s", rows, f.beadID, wantStatus, wantAssignee)
	}
}

func (f *spawnAssignmentCLIFixture) readAssignment(t *testing.T) *spawnAssignmentRecord {
	t.Helper()
	path := filepath.Join(f.homeDir, ".ntm", "sessions", f.session, "assignments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spawn assignment ledger: %v", err)
	}
	var ledger spawnAssignmentLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode spawn assignment ledger: %v raw=%s", err, data)
	}
	if ledger.SessionName != f.session || ledger.Version < 4 {
		t.Fatalf("spawn assignment ledger header = session:%q version:%d", ledger.SessionName, ledger.Version)
	}
	record := ledger.Assignments[f.beadID]
	if record == nil {
		t.Fatalf("spawn assignment ledger missing %s: %+v", f.beadID, ledger.Assignments)
	}
	return record
}

func (f *spawnAssignmentCLIFixture) mustBR(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.brPath, args...)
	cmd.Dir = f.projectDir
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("br command timed out: %q", args)
	}
	if err != nil {
		t.Fatalf("br %q: %v output=%s", args, err, output)
	}
	return output
}

func (f *spawnAssignmentCLIFixture) mustTMUX(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.runTMUX(ctx, args...); err != nil {
		t.Fatalf("tmux %s: %v", strings.Join(args, " "), err)
	}
}

func (f *spawnAssignmentCLIFixture) runTMUX(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *spawnAssignmentCLIFixture) tmuxOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func writeSpawnFakeClaude(t *testing.T, path string) {
	t.Helper()
	content := `#!/bin/sh
stty -echo
printf 'Claude Code v0.0.0\nclaude>\n'
while IFS= read -r line; do
    printf 'RECEIVED:%s\n' "$line"
    printf 'claude>\n'
done
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake Claude executable: %v", err)
	}
}

func writeSpawnFakeBV(t *testing.T, path, beadID, title string) {
	t.Helper()
	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"data_hash":    "spawn-assignment-e2e",
		"triage": map[string]any{
			"meta": map[string]any{
				"version": "e2e", "generated_at": time.Now().UTC().Format(time.RFC3339Nano),
				"phase2_ready": true, "issue_count": 1, "compute_time_ms": 1,
			},
			"quick_ref": map[string]any{
				"open_count": 1, "actionable_count": 1, "blocked_count": 0,
				"in_progress_count": 0, "top_picks": []any{},
			},
			"recommendations": []map[string]any{{
				"id": beadID, "title": title, "type": "task", "status": "ready",
				"priority": 1, "score": 100.0, "action": "claim",
				"reasons": []string{"spawn CLI E2E"},
			}},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fake bv triage: %v", err)
	}
	content := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" != \"--robot-triage\" ]; then\n  echo \"unexpected bv args: $*\" >&2\n  exit 64\nfi\nprintf '%%s\\n' '%s'\n", encoded)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake bv executable: %v", err)
	}
}

func expectedSpawnWorkPrompt(beadID, title string) string {
	return fmt.Sprintf("Work on bead %s: %s\n\nUse `br show %s` to see full details.\n"+
		"This bead has been marked as in_progress.\n\nContext:\n- spawn CLI E2E\n\n"+
		"When done, close it with: `br close %s --reason \"Completed\"`", beadID, title, beadID, beadID)
}

func spawnAssignmentIsolatedEnv(overrides map[string]string) []string {
	replaced := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {}, "PATH": {},
		"TMUX": {}, "TMUX_PANE": {}, "TMUX_TMPDIR": {},
		"NTM_CONFIG": {}, "NTM_OUTPUT_FORMAT": {}, "NTM_ROBOT_FORMAT": {}, "TOON_DEFAULT_FORMAT": {},
		"AGENT_MAIL_URL": {}, "AGENT_MAIL_TOKEN": {},
		"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
	}
	for key := range overrides {
		replaced[key] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := replaced[key]; !skip {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func spawnAssignmentMergeEnv(base []string, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type spawnAssignmentMCPStub struct {
	mu         sync.Mutex
	projectDir string
	beadID     string
	recipient  string
	path       string
	token      string
	ensure     int
	list       int
	reserve    int
	errors     []string
}

func (s *spawnAssignmentMCPStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/mcp/" {
		s.failRPC(w, nil, -32600, fmt.Sprintf("unexpected HTTP request %s %s", r.Method, r.URL.Path))
		return
	}
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	reader := http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(reader).Decode(&request); err != nil {
		s.failRPC(w, nil, -32700, "decode request: "+err.Error())
		return
	}
	if request.JSONRPC != "2.0" {
		s.failRPC(w, request.ID, -32600, "jsonrpc must be 2.0")
		return
	}

	switch request.Method {
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			s.failRPC(w, request.ID, -32602, "decode tool call: "+err.Error())
			return
		}
		s.handleTool(w, request.ID, params.Name, params.Arguments)
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			s.failRPC(w, request.ID, -32602, "decode resource read: "+err.Error())
			return
		}
		s.handleResource(w, request.ID, params.URI)
	default:
		s.failRPC(w, request.ID, -32601, "unexpected method: "+request.Method)
	}
}

func (s *spawnAssignmentMCPStub) handleTool(w http.ResponseWriter, id any, name string, args map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch name {
	case "health_check":
		s.writeResult(w, id, map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		})
	case "ensure_project":
		if got, _ := args["human_key"].(string); got != s.projectDir {
			s.failRPCLocked(w, id, -32602, fmt.Sprintf("ensure_project human_key=%q want=%q", got, s.projectDir))
			return
		}
		s.ensure++
		s.writeResult(w, id, map[string]any{
			"id": spawnAssignmentProjectID, "slug": "spawn-assignment-e2e",
			"human_key": s.projectDir, "created_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	case "fetch_inbox", "list_file_reservations", "list_reservations":
		s.writeResult(w, id, []any{})
	case "file_reservation_paths":
		paths, ok := anyStringSlice(args["paths"])
		ttl, ttlOK := args["ttl_seconds"].(float64)
		if project, _ := args["project_key"].(string); project != s.projectDir ||
			args["agent_name"] != s.recipient || !ok || !equalStrings(paths, []string{s.path}) ||
			args["exclusive"] != true || !ttlOK || int(ttl) != 3600 ||
			args["reason"] != "bead assignment: "+s.beadID || args["registration_token"] != s.token {
			s.failRPCLocked(w, id, -32602, fmt.Sprintf("invalid reservation arguments: %#v", args))
			return
		}
		s.reserve++
		now := time.Now().UTC()
		s.writeResult(w, id, map[string]any{
			"granted": []map[string]any{{
				"id": spawnAssignmentReservationID, "path_pattern": s.path,
				"agent_name": s.recipient, "project_id": spawnAssignmentProjectID,
				"exclusive": true, "reason": "bead assignment: " + s.beadID,
				"created_ts": now.Format(time.RFC3339Nano),
				"expires_ts": now.Add(time.Hour).Format(time.RFC3339Nano),
			}},
			"conflicts": []any{},
		})
	default:
		s.failRPCLocked(w, id, -32601, "unexpected tool: "+name)
	}
}

func (s *spawnAssignmentMCPStub) handleResource(w http.ResponseWriter, id any, resourceURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(resourceURI, "resource://file_reservations/") {
		if !strings.Contains(resourceURI, url.QueryEscape(s.projectDir)) && !strings.Contains(resourceURI, url.PathEscape(s.projectDir)) {
			s.failRPCLocked(w, id, -32602, "unexpected reservation resource URI: "+resourceURI)
			return
		}
		s.writeResult(w, id, map[string]any{
			"contents": []map[string]any{{
				"uri": resourceURI, "mimeType": "application/json", "text": "[]",
			}},
		})
		return
	}
	const prefix = "resource://agents/"
	if !strings.HasPrefix(resourceURI, prefix) {
		s.failRPCLocked(w, id, -32602, "unexpected resource URI: "+resourceURI)
		return
	}
	project, err := url.PathUnescape(strings.TrimPrefix(resourceURI, prefix))
	if err != nil || project != s.projectDir {
		s.failRPCLocked(w, id, -32602, fmt.Sprintf("agents project=%q err=%v want=%q", project, err, s.projectDir))
		return
	}
	s.list++
	agentsJSON, err := json.Marshal(map[string]any{
		"agents": []map[string]any{{
			"id": spawnAssignmentAgentID, "name": s.recipient, "program": "claude-code",
			"model": "e2e", "task_description": "spawn assignment E2E",
			"project_id":     spawnAssignmentProjectID,
			"inception_ts":   time.Now().UTC().Format(time.RFC3339Nano),
			"last_active_ts": time.Now().UTC().Format(time.RFC3339Nano),
		}},
	})
	if err != nil {
		s.failRPCLocked(w, id, -32603, "encode agents resource: "+err.Error())
		return
	}
	s.writeResult(w, id, map[string]any{
		"contents": []map[string]any{{
			"uri": resourceURI, "mimeType": "application/json", "text": string(agentsJSON),
		}},
	})
}

func (s *spawnAssignmentMCPStub) writeResult(w http.ResponseWriter, id, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *spawnAssignmentMCPStub) failRPC(w http.ResponseWriter, id any, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRPCLocked(w, id, code, message)
}

func (s *spawnAssignmentMCPStub) failRPCLocked(w http.ResponseWriter, id any, code int, message string) {
	s.errors = append(s.errors, message)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

type spawnAssignmentMCPCounts struct {
	ensure  int
	list    int
	reserve int
}

func (s *spawnAssignmentMCPStub) assertCleanMutationCount(t *testing.T, reserve int) spawnAssignmentMCPCounts {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) != 0 || s.ensure == 0 || s.list == 0 || s.reserve != reserve {
		t.Fatalf("Agent Mail MCP calls ensure/list/reserve=%d/%d/%d want discovery>0/reserve=%d errors=%v",
			s.ensure, s.list, s.reserve, reserve, s.errors)
	}
	return spawnAssignmentMCPCounts{ensure: s.ensure, list: s.list, reserve: s.reserve}
}

func anyStringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		result = append(result, text)
	}
	return result, true
}
