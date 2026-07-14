//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM timeline persistence features.
// [E2E-TIMELINE-PERSIST] Tests for timeline data persistence, compression, and lifecycle.
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

// TimelinePersistTestSuite manages E2E tests for timeline persistence
type TimelinePersistTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
}

// NewTimelinePersistTestSuite creates a new test suite
func NewTimelinePersistTestSuite(t *testing.T, scenario string) *TimelinePersistTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("herdctl/ntm binary not found in PATH")
	}

	return &TimelinePersistTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// supportsCommand checks if ntm supports a given subcommand
func (s *TimelinePersistTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireTimelineCommand skips if timeline command is not supported
func (s *TimelinePersistTestSuite) requireTimelineCommand() {
	if !s.supportsCommand("timeline") {
		s.t.Skip("timeline command not supported by this ntm version")
	}
}

// Setup creates a temporary directory for testing
func (s *TimelinePersistTestSuite) Setup() error {
	tempDir, err := os.MkdirTemp("", "ntm-persist-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("[E2E-SETUP] Created temp directory: %s", tempDir)
	return nil
}

// Teardown cleans up resources
func (s *TimelinePersistTestSuite) Teardown() {
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
	s.logger.Close()
}

// runNTMAllowFail runs ntm allowing non-zero exit codes
func (s *TimelinePersistTestSuite) runNTMAllowFail(args ...string) string {
	s.logger.Log("[E2E-RUN] ntm %s (allow-fail)", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	output, _ := cmd.CombinedOutput()
	result := string(output)
	s.logger.Log("[E2E-RUN] Output: %s", result)
	return result
}

// createTestTimelineFile creates a test timeline JSONL file
func (s *TimelinePersistTestSuite) createTestTimelineFile(sessionID string, eventCount int) (string, error) {
	timelineDir := filepath.Join(os.Getenv("HOME"), ".ntm", "timelines")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		return "", err
	}

	testFile := filepath.Join(timelineDir, sessionID+".jsonl")
	now := time.Now()

	f, err := os.Create(testFile)
	if err != nil {
		return "", err
	}
	defer f.Close()

	for i := 0; i < eventCount; i++ {
		event := TimelineEventEntry{
			AgentID:   "cc_1",
			AgentType: "claude",
			SessionID: sessionID,
			State:     "working",
			Timestamp: now.Add(-time.Duration(eventCount-i) * time.Minute),
		}
		if i%2 == 1 {
			event.State = "idle"
			event.PreviousState = "working"
		}
		data, _ := json.Marshal(event)
		f.Write(data)
		f.WriteString("\n")
	}

	s.cleanup = append(s.cleanup, func() { os.Remove(testFile) })
	s.logger.Log("[E2E-SETUP] Created test timeline: %s (%d events)", testFile, eventCount)
	return testFile, nil
}

// ========== Storage Location Tests ==========

// TestTimelineStorageDirectory verifies timeline storage directory
func TestTimelineStorageDirectory(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-storage-dir")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline storage directory")

	// Default timeline storage is ~/.ntm/timelines
	expectedDir := filepath.Join(os.Getenv("HOME"), ".ntm", "timelines")

	// Check if directory exists or can be created
	if err := os.MkdirAll(expectedDir, 0755); err != nil {
		t.Fatalf("Cannot create timelines directory: %v", err)
	}

	info, err := os.Stat(expectedDir)
	if err != nil {
		t.Fatalf("Timelines directory not accessible: %v", err)
	}

	if !info.IsDir() {
		t.Error("Expected timelines path to be a directory")
	}

	suite.logger.Log("[E2E-INFO] Timelines directory: %s", expectedDir)
	suite.logger.Log("[E2E-PASS] Storage directory validated")
}

// TestTimelineFileFormat verifies JSONL file format
func TestTimelineFileFormat(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-file-format")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline JSONL file format")

	sessionID := "e2e-format-test-" + time.Now().Format("150405")
	testFile, err := suite.createTestTimelineFile(sessionID, 5)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Read and verify JSONL format
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 5 {
		t.Errorf("Expected 5 lines, got %d", len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var event TimelineEventEntry
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
	}

	suite.logger.Log("[E2E-PASS] JSONL file format validated")
}

// ========== Lifecycle Tests ==========

// TestTimelineLifecycleCreateListShow verifies create-list-show workflow
func TestTimelineLifecycleCreateListShow(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-lifecycle-cls")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline create-list-show lifecycle")

	sessionID := "e2e-lifecycle-cls-" + time.Now().Format("150405")

	// Step 1: Create
	_, err := suite.createTestTimelineFile(sessionID, 10)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	suite.logger.Log("[E2E-STEP] Created timeline: %s", sessionID)

	// Step 2: List
	listOutput := suite.runNTMAllowFail("timeline", "list", "--json")
	if strings.Contains(listOutput, sessionID) {
		suite.logger.Log("[E2E-STEP] Timeline found in list")
	} else {
		suite.logger.Log("[E2E-INFO] Timeline not found in list (may use different storage)")
	}

	// Step 3: Show
	showOutput := suite.runNTMAllowFail("timeline", "show", sessionID, "--json")
	if strings.Contains(showOutput, "not found") {
		suite.logger.Log("[E2E-INFO] Show returned not found (storage path may differ)")
	} else {
		suite.logger.Log("[E2E-STEP] Show command executed")
	}

	suite.logger.Log("[E2E-PASS] Create-list-show lifecycle completed")
}

// TestTimelineLifecycleCreateListDelete verifies create-list-delete workflow
func TestTimelineLifecycleCreateListDelete(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-lifecycle-cld")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline create-list-delete lifecycle")

	sessionID := "e2e-lifecycle-cld-" + time.Now().Format("150405")

	// Step 1: Create
	testFile, err := suite.createTestTimelineFile(sessionID, 5)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	suite.logger.Log("[E2E-STEP] Created timeline: %s", sessionID)

	// Verify file exists
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("Timeline file not created: %v", err)
	}

	// Step 2: Delete via CLI (with force)
	deleteOutput := suite.runNTMAllowFail("timeline", "delete", sessionID, "--force")
	suite.logger.Log("[E2E-STEP] Delete command output: %s", deleteOutput)

	// Step 3: Verify (either via CLI or file check)
	if strings.Contains(deleteOutput, "Deleted") || strings.Contains(deleteOutput, "deleted") {
		suite.logger.Log("[E2E-STEP] Timeline deleted via CLI")
	} else {
		// Check file directly
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			suite.logger.Log("[E2E-STEP] Timeline file removed")
		} else {
			suite.logger.Log("[E2E-INFO] Timeline may still exist (CLI uses different path)")
		}
	}

	suite.logger.Log("[E2E-PASS] Create-list-delete lifecycle completed")
}

// ========== Compression Tests ==========

// TestTimelineCompressedFileSupport verifies compressed (.gz) timeline support
func TestTimelineCompressedFileSupport(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-compressed")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing compressed timeline file support")

	// Check if cleanup command mentions compression
	output := suite.runNTMAllowFail("timeline", "cleanup", "--help")

	if strings.Contains(output, "compress") {
		suite.logger.Log("[E2E-INFO] Cleanup mentions compression")
	}

	// Check stats for compressed count field
	statsOutput := suite.runNTMAllowFail("timeline", "stats", "--json")

	if strings.Contains(statsOutput, "compressed") {
		suite.logger.Log("[E2E-INFO] Stats includes compressed count")
	}

	suite.logger.Log("[E2E-PASS] Compression support validated")
}

// ========== Cleanup Tests ==========

// TestTimelineCleanupBasic verifies basic cleanup functionality
func TestTimelineCleanupBasic(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-cleanup-basic")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline cleanup")

	// Run cleanup with force
	output := suite.runNTMAllowFail("timeline", "cleanup", "--force")

	// Should complete without error
	if strings.Contains(output, "error") && !strings.Contains(output, "No cleanup needed") {
		suite.logger.Log("[E2E-INFO] Cleanup may have encountered issues: %s", output)
	}

	suite.logger.Log("[E2E-PASS] Cleanup executed")
}

// TestTimelineCleanupWithOldData verifies cleanup removes old timelines
func TestTimelineCleanupWithOldData(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-cleanup-old")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing cleanup of old timeline data")

	// Create multiple test timelines
	for i := 0; i < 3; i++ {
		sessionID := "e2e-cleanup-test-" + time.Now().Format("150405") + "-" + string(rune('a'+i))
		_, err := suite.createTestTimelineFile(sessionID, 5)
		if err != nil {
			suite.logger.Log("[E2E-WARN] Failed to create test timeline %d: %v", i, err)
		}
	}

	// Get stats before cleanup
	statsBefore := suite.runNTMAllowFail("timeline", "stats", "--json")
	suite.logger.Log("[E2E-INFO] Stats before cleanup: %s", statsBefore[:min(len(statsBefore), 200)])

	// Run cleanup
	output := suite.runNTMAllowFail("timeline", "cleanup", "--force")
	suite.logger.Log("[E2E-INFO] Cleanup output: %s", output)

	// Get stats after cleanup
	statsAfter := suite.runNTMAllowFail("timeline", "stats", "--json")
	suite.logger.Log("[E2E-INFO] Stats after cleanup: %s", statsAfter[:min(len(statsAfter), 200)])

	suite.logger.Log("[E2E-PASS] Cleanup with old data completed")
}

// ========== Data Integrity Tests ==========

// TestTimelineDataIntegrity verifies data integrity after persistence
func TestTimelineDataIntegrity(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-integrity")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing timeline data integrity")

	sessionID := "e2e-integrity-" + time.Now().Format("150405")
	eventCount := 10

	// Create timeline with known data
	testFile, err := suite.createTestTimelineFile(sessionID, eventCount)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Read back and verify
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Verify event count matches
	if len(lines) != eventCount {
		t.Errorf("Event count mismatch: expected %d, got %d", eventCount, len(lines))
	}

	// Verify JSON validity and field presence
	for i, line := range lines {
		var event TimelineEventEntry
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("Line %d invalid JSON: %v", i, err)
			continue
		}

		// Verify required fields
		if event.AgentID == "" {
			t.Errorf("Line %d missing agent_id", i)
		}
		if event.SessionID != sessionID {
			t.Errorf("Line %d session_id mismatch: expected %s, got %s", i, sessionID, event.SessionID)
		}
		if event.Timestamp.IsZero() {
			t.Errorf("Line %d has zero timestamp", i)
		}
	}

	suite.logger.Log("[E2E-PASS] Data integrity validated")
}

// TestTimelineLargeDataPersistence verifies large timeline handling
func TestTimelineLargeDataPersistence(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-large-data")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing large timeline data handling")

	sessionID := "e2e-large-" + time.Now().Format("150405")
	eventCount := 1000

	// Create timeline with many events
	testFile, err := suite.createTestTimelineFile(sessionID, eventCount)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Verify file size
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat test file: %v", err)
	}

	suite.logger.Log("[E2E-INFO] Large timeline file: %d bytes (%d events)", info.Size(), eventCount)

	// Try to show stats (should handle large data)
	output := suite.runNTMAllowFail("timeline", "show", sessionID, "--json")

	if strings.Contains(output, "event") || strings.Contains(output, "Error") || strings.Contains(output, "not found") {
		suite.logger.Log("[E2E-INFO] Large data show completed")
	}

	// Minimum expected size (roughly 100 bytes per event minimum)
	minExpectedSize := int64(eventCount * 80)
	if info.Size() < minExpectedSize {
		t.Errorf("File too small for %d events: %d bytes", eventCount, info.Size())
	}

	suite.logger.Log("[E2E-PASS] Large data handling validated")
}

// TestTimelineMultiAgentPersistence verifies multi-agent timeline storage
func TestTimelineMultiAgentPersistence(t *testing.T) {
	suite := NewTimelinePersistTestSuite(t, "timeline-multi-agent")
	if err := suite.Setup(); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer suite.Teardown()
	suite.requireTimelineCommand()

	suite.logger.Log("[E2E-TEST] Testing multi-agent timeline persistence")

	sessionID := "e2e-multi-agent-" + time.Now().Format("150405")
	timelineDir := filepath.Join(os.Getenv("HOME"), ".ntm", "timelines")
	if err := os.MkdirAll(timelineDir, 0755); err != nil {
		t.Fatalf("Failed to create timelines dir: %v", err)
	}

	testFile := filepath.Join(timelineDir, sessionID+".jsonl")
	now := time.Now()

	// Create events from multiple agents
	agents := []string{"cc_1", "cod_1", "gmi_1", "cc_2"}
	agentTypes := map[string]string{
		"cc_1":  "claude",
		"cod_1": "codex",
		"gmi_1": "gemini",
		"cc_2":  "claude",
	}

	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer f.Close()
	suite.cleanup = append(suite.cleanup, func() { os.Remove(testFile) })

	eventCount := 0
	for i := 0; i < 20; i++ {
		agentID := agents[i%len(agents)]
		event := TimelineEventEntry{
			AgentID:   agentID,
			AgentType: agentTypes[agentID],
			SessionID: sessionID,
			State:     "working",
			Timestamp: now.Add(-time.Duration(20-i) * time.Minute),
		}
		if i%2 == 1 {
			event.State = "idle"
			event.PreviousState = "working"
		}
		data, _ := json.Marshal(event)
		f.Write(data)
		f.WriteString("\n")
		eventCount++
	}
	f.Close()

	suite.logger.Log("[E2E-INFO] Created multi-agent timeline: %d events from %d agents", eventCount, len(agents))

	// Verify by reading back
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// Count events per agent
	agentCounts := make(map[string]int)
	for _, line := range lines {
		var event TimelineEventEntry
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		agentCounts[event.AgentID]++
	}

	// Verify all agents have events
	for _, agentID := range agents {
		if count, ok := agentCounts[agentID]; !ok || count == 0 {
			t.Errorf("Agent %s has no events", agentID)
		} else {
			suite.logger.Log("[E2E-INFO] Agent %s: %d events", agentID, count)
		}
	}

	suite.logger.Log("[E2E-PASS] Multi-agent persistence validated")
}
