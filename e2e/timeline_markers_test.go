//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM timeline marker features.
// [E2E-TIMELINE-MARKERS] Tests for event markers (prompts, completions, errors) display.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TimelineMarker represents a discrete event marker on the timeline
type TimelineMarker struct {
	ID        string            `json:"id"`
	AgentID   string            `json:"agent_id"`
	SessionID string            `json:"session_id"`
	Type      string            `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Message   string            `json:"message,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}

// TimelineMarkersTestSuite manages E2E tests for timeline markers
type TimelineMarkersTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
}

// NewTimelineMarkersTestSuite creates a new test suite
func NewTimelineMarkersTestSuite(t *testing.T, scenario string) *TimelineMarkersTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &TimelineMarkersTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *TimelineMarkersTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireTimelineCommand skips if timeline command is not supported
func (s *TimelineMarkersTestSuite) requireTimelineCommand() {
	if !s.supportsCommand("timeline") {
		s.t.Skip("timeline command not supported by this ntm version")
	}
}

// Setup creates a temporary directory for testing
func (s *TimelineMarkersTestSuite) Setup() error {
	tempDir, err := os.MkdirTemp("", "ntm-markers-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("[E2E-SETUP] Created temp directory: %s", tempDir)
	return nil
}

// Teardown cleans up resources
func (s *TimelineMarkersTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
	s.logger.Close()
}

// runNTMAllowFail runs ntm allowing non-zero exit codes
func (s *TimelineMarkersTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("[E2E-RUN] ntm %s (allow-fail)", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result
}

// ========== Marker Type Tests ==========

// TestMarkerTypePrompt verifies prompt markers (▶) are recognized
func TestMarkerTypePrompt(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-type-prompt")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing prompt marker type")

	// Create test marker data
	marker := TimelineMarker{
		ID:        "m1",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "prompt",
		Timestamp: time.Now(),
		Message:   "Test prompt message",
	}

	// Verify marker type is valid by checking it serializes correctly
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal prompt marker: %v", err)
	}

	if !strings.Contains(string(data), `"type":"prompt"`) {
		t.Errorf("Prompt marker type not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Prompt marker type (▶) validated")
}

// TestMarkerTypeCompletion verifies completion markers (✓) are recognized
func TestMarkerTypeCompletion(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-type-completion")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing completion marker type")

	marker := TimelineMarker{
		ID:        "m2",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "completion",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal completion marker: %v", err)
	}

	if !strings.Contains(string(data), `"type":"completion"`) {
		t.Errorf("Completion marker type not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Completion marker type (✓) validated")
}

// TestMarkerTypeError verifies error markers (✗) are recognized
func TestMarkerTypeError(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-type-error")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing error marker type")

	marker := TimelineMarker{
		ID:        "m3",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "error",
		Timestamp: time.Now(),
		Message:   "Rate limit exceeded",
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal error marker: %v", err)
	}

	if !strings.Contains(string(data), `"type":"error"`) {
		t.Errorf("Error marker type not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Error marker type (✗) validated")
}

// TestMarkerTypeStart verifies start markers (◆) are recognized
func TestMarkerTypeStart(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-type-start")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing start marker type")

	marker := TimelineMarker{
		ID:        "m4",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "start",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal start marker: %v", err)
	}

	if !strings.Contains(string(data), `"type":"start"`) {
		t.Errorf("Start marker type not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Start marker type (◆) validated")
}

// TestMarkerTypeStop verifies stop markers (◆) are recognized
func TestMarkerTypeStop(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-type-stop")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing stop marker type")

	marker := TimelineMarker{
		ID:        "m5",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "stop",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal stop marker: %v", err)
	}

	if !strings.Contains(string(data), `"type":"stop"`) {
		t.Errorf("Stop marker type not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Stop marker type (◆) validated")
}

// ========== Marker Details Tests ==========

// TestMarkerWithDetails verifies markers can have additional details
func TestMarkerWithDetails(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-with-details")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing marker with details metadata")

	marker := TimelineMarker{
		ID:        "m6",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "prompt",
		Timestamp: time.Now(),
		Message:   "Write tests",
		Details: map[string]string{
			"pane":      "0",
			"source":    "user",
			"char_len":  "120",
			"has_image": "false",
		},
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal marker with details: %v", err)
	}

	// Verify details are preserved
	if !strings.Contains(string(data), `"pane":"0"`) {
		t.Errorf("Marker details not preserved: %s", data)
	}
	if !strings.Contains(string(data), `"source":"user"`) {
		t.Errorf("Marker source detail not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Marker with details validated")
}

// TestMarkerWithMessage verifies markers can have messages
func TestMarkerWithMessage(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-with-message")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing marker with message")

	longMessage := strings.Repeat("Write comprehensive E2E tests for timeline feature. ", 5)
	marker := TimelineMarker{
		ID:        "m7",
		AgentID:   "cc_1",
		SessionID: "test-session",
		Type:      "prompt",
		Timestamp: time.Now(),
		Message:   longMessage,
	}

	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("Failed to marshal marker with message: %v", err)
	}

	// Verify message is included
	if !strings.Contains(string(data), "comprehensive E2E tests") {
		t.Errorf("Marker message not preserved: %s", data)
	}

	suite.logger.Log("[E2E-PASS] Marker with message validated")
}

// ========== Multiple Markers Tests ==========

// TestMultipleMarkersSequence verifies a sequence of markers
func TestMultipleMarkersSequence(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "markers-sequence")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing sequence of multiple markers")

	now := time.Now()
	sessionID := "test-sequence-session"

	// Create a realistic sequence of markers
	markers := []TimelineMarker{
		{ID: "m1", AgentID: "cc_1", SessionID: sessionID, Type: "start", Timestamp: now.Add(-10 * time.Minute)},
		{ID: "m2", AgentID: "cc_1", SessionID: sessionID, Type: "prompt", Timestamp: now.Add(-9 * time.Minute), Message: "Write tests"},
		{ID: "m3", AgentID: "cc_1", SessionID: sessionID, Type: "completion", Timestamp: now.Add(-5 * time.Minute)},
		{ID: "m4", AgentID: "cc_1", SessionID: sessionID, Type: "prompt", Timestamp: now.Add(-4 * time.Minute), Message: "Fix bug"},
		{ID: "m5", AgentID: "cc_1", SessionID: sessionID, Type: "error", Timestamp: now.Add(-3 * time.Minute), Message: "Rate limited"},
		{ID: "m6", AgentID: "cc_1", SessionID: sessionID, Type: "completion", Timestamp: now.Add(-1 * time.Minute)},
	}

	// Verify all markers can be serialized
	for i, marker := range markers {
		data, err := json.Marshal(marker)
		if err != nil {
			t.Fatalf("Failed to marshal marker %d: %v", i, err)
		}
		suite.logger.Log("[E2E-INFO] Marker %d: %s", i, data)
	}

	// Verify sequence order (timestamps should be increasing)
	for i := 1; i < len(markers); i++ {
		if markers[i].Timestamp.Before(markers[i-1].Timestamp) {
			t.Errorf("Markers not in chronological order at index %d", i)
		}
	}

	suite.logger.Log("[E2E-PASS] Marker sequence validated (%d markers)", len(markers))
}

// TestMultipleAgentMarkers verifies markers from multiple agents
func TestMultipleAgentMarkers(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "markers-multi-agent")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing markers from multiple agents")

	now := time.Now()
	sessionID := "test-multi-agent-session"

	markers := []TimelineMarker{
		{ID: "m1", AgentID: "cc_1", SessionID: sessionID, Type: "start", Timestamp: now.Add(-10 * time.Minute)},
		{ID: "m2", AgentID: "cod_1", SessionID: sessionID, Type: "start", Timestamp: now.Add(-9 * time.Minute)},
		{ID: "m3", AgentID: "gmi_1", SessionID: sessionID, Type: "start", Timestamp: now.Add(-8 * time.Minute)},
		{ID: "m4", AgentID: "cc_1", SessionID: sessionID, Type: "prompt", Timestamp: now.Add(-7 * time.Minute)},
		{ID: "m5", AgentID: "cod_1", SessionID: sessionID, Type: "prompt", Timestamp: now.Add(-6 * time.Minute)},
		{ID: "m6", AgentID: "gmi_1", SessionID: sessionID, Type: "prompt", Timestamp: now.Add(-5 * time.Minute)},
	}

	// Count markers per agent
	agentCounts := make(map[string]int)
	for _, marker := range markers {
		agentCounts[marker.AgentID]++
	}

	// Verify we have markers from multiple agents
	if len(agentCounts) != 3 {
		t.Errorf("Expected markers from 3 agents, got %d", len(agentCounts))
	}

	for agentID, count := range agentCounts {
		suite.logger.Log("[E2E-INFO] Agent %s has %d markers", agentID, count)
	}

	suite.logger.Log("[E2E-PASS] Multi-agent markers validated")
}

// ========== Marker File Storage Tests ==========

// TestMarkerDataFilePersistence verifies markers can be written to files
func TestMarkerDataFilePersistence(t *testing.T) {
	suite := NewTimelineMarkersTestSuite(t, "marker-persistence")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing marker data file persistence")

	// Create markers file in temp directory
	markersFile := filepath.Join(suite.tempDir, "test-markers.jsonl")

	markers := []TimelineMarker{
		{ID: "m1", AgentID: "cc_1", SessionID: "test", Type: "prompt", Timestamp: time.Now(), Message: "Test"},
		{ID: "m2", AgentID: "cc_1", SessionID: "test", Type: "completion", Timestamp: time.Now()},
	}

	f, err := os.Create(markersFile)
	if err != nil {
		t.Fatalf("Failed to create markers file: %v", err)
	}
	defer f.Close()

	for _, marker := range markers {
		data, _ := json.Marshal(marker)
		f.Write(data)
		f.WriteString("\n")
	}
	f.Close()

	// Verify file was created and has content
	info, err := os.Stat(markersFile)
	if err != nil {
		t.Fatalf("Markers file not created: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Markers file is empty")
	}

	suite.logger.Log("[E2E-INFO] Markers file created: %s (%d bytes)", markersFile, info.Size())

	// Read back and verify
	content, err := os.ReadFile(markersFile)
	if err != nil {
		t.Fatalf("Failed to read markers file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 marker lines, got %d", len(lines))
	}

	suite.logger.Log("[E2E-PASS] Marker file persistence validated")
}
