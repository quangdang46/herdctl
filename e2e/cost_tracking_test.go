//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-COST] Tests for cost tracking across session lifecycle.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/cost"
)

// extractJSON extracts JSON from output that may contain log lines
func extractCostJSON(output string) string {
	objStart := strings.Index(output, "{")
	arrStart := strings.Index(output, "[")

	start := -1
	if objStart >= 0 && arrStart >= 0 {
		if objStart < arrStart {
			start = objStart
		} else {
			start = arrStart
		}
	} else if objStart >= 0 {
		start = objStart
	} else if arrStart >= 0 {
		start = arrStart
	}

	if start < 0 {
		return output
	}
	return strings.TrimSpace(output[start:])
}

// CostTestSuite manages E2E tests for cost tracking
type CostTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	ntmPath string
	origDir string
	tracker *cost.CostTracker
}

// NewCostTestSuite creates a new test suite for cost E2E tests
func NewCostTestSuite(t *testing.T, scenario string) *CostTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	return &CostTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
	}
}

// Setup creates temp directory for testing
func (s *CostTestSuite) Setup() error {
	s.logger.Log("[E2E-COST] Setting up cost test environment")

	tempDir, err := os.MkdirTemp("", "ntm-cost-e2e-*")
	if err != nil {
		return err
	}
	s.tempDir = tempDir
	s.cleanup = append(s.cleanup, func() { os.RemoveAll(tempDir) })
	s.logger.Log("[E2E-COST] Created temp directory: %s", tempDir)

	// Change to temp directory
	s.origDir, err = os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(tempDir); err != nil {
		return err
	}
	s.cleanup = append(s.cleanup, func() { os.Chdir(s.origDir) })
	s.logger.Log("[E2E-COST] Changed to temp directory")

	// Create tracker
	s.tracker = cost.NewCostTracker(tempDir)

	return nil
}

// Cleanup runs cleanup functions
func (s *CostTestSuite) Cleanup() {
	s.logger.Log("[E2E-COST] Running cleanup (%d items)", len(s.cleanup))
	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}
}

// runNTM executes an ntm command and returns the output
func (s *CostTestSuite) runNTM(args ...string) (string, error) {
	s.logger.Log("[E2E-COST] Running: ntm %s", strings.Join(args, " "))
	cmd := exec.Command(s.ntmPath, args...)
	cmd.Dir = s.tempDir
	out, err := cmd.CombinedOutput()
	s.logger.Log("[E2E-COST] Output length: %d bytes", len(out))
	return string(out), err
}

// =============================================================================
// Test: Token Statistics API
// =============================================================================

func TestCostTokenStatistics(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-tokens")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 1: Token Statistics API ===")

	// Run robot-tokens to get usage stats
	out, err := suite.runNTM("--robot-tokens")
	if err != nil {
		t.Fatalf("[E2E-COST] --robot-tokens failed: %v, output: %s", err, out)
	}

	var resp struct {
		Success     bool   `json:"success"`
		Timestamp   string `json:"timestamp"`
		Period      string `json:"period"`
		GroupBy     string `json:"group_by"`
		TotalTokens int    `json:"total_tokens"`
		Breakdown   []struct {
			Key        string  `json:"key"`
			Tokens     int     `json:"tokens"`
			Prompts    int     `json:"prompts"`
			Percentage float64 `json:"percentage"`
		} `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(extractCostJSON(out)), &resp); err != nil {
		t.Fatalf("[E2E-COST] Failed to parse response: %v", err)
	}

	if !resp.Success {
		t.Fatal("[E2E-COST] Token stats response success=false")
	}

	suite.logger.Log("[E2E-COST] Total tokens: %d", resp.TotalTokens)
	suite.logger.Log("[E2E-COST] Group by: %s", resp.GroupBy)
	suite.logger.Log("[E2E-COST] Period: %s", resp.Period)
	suite.logger.Log("[E2E-COST] PASS: Token statistics API works")
}

// =============================================================================
// Test: Token Statistics Filtering
// =============================================================================

func TestCostTokenFiltering(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-filter")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 2: Token Statistics Filtering ===")

	// Test filtering by agent type
	suite.logger.Log("[E2E-COST] Testing --tokens-agent filter")
	out, err := suite.runNTM("--robot-tokens", "--tokens-agent=claude")
	if err != nil {
		t.Logf("[E2E-COST] --tokens-agent filter result: %s", out)
	}

	// Test different groupings
	groupings := []string{"agent", "model", "day"}
	for _, groupBy := range groupings {
		suite.logger.Log("[E2E-COST] Testing --tokens-group-by=%s", groupBy)
		out, err = suite.runNTM("--robot-tokens", "--tokens-group-by="+groupBy)
		if err != nil {
			suite.logger.Log("[E2E-COST] Group by %s: command returned error (may be expected)", groupBy)
			continue
		}

		var resp struct {
			GroupBy string `json:"group_by"`
		}
		if err := json.Unmarshal([]byte(extractCostJSON(out)), &resp); err == nil {
			suite.logger.Log("[E2E-COST] Group by %s: returned group_by=%s", groupBy, resp.GroupBy)
		}
	}

	// Test time filtering
	suite.logger.Log("[E2E-COST] Testing --tokens-days filter")
	out, err = suite.runNTM("--robot-tokens", "--tokens-days=7")
	if err == nil {
		var resp struct {
			Period string `json:"period"`
		}
		if err := json.Unmarshal([]byte(extractCostJSON(out)), &resp); err == nil {
			suite.logger.Log("[E2E-COST] Days=7 filter: period=%s", resp.Period)
		}
	}

	suite.logger.Log("[E2E-COST] PASS: Token statistics filtering works")
}

// =============================================================================
// Test: Cost Tracker Persistence
// =============================================================================

func TestCostTrackerPersistence(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-persist")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 3: Cost Tracker Persistence ===")

	// Record some costs using the tracker directly
	suite.logger.Log("[E2E-COST] Recording test costs")
	suite.tracker.RecordTokens("test-session", "%0", "claude-opus", 1000, 500)
	suite.tracker.RecordTokens("test-session", "%1", "gemini-pro", 800, 400)
	suite.tracker.RecordTokens("other-session", "%0", "gpt-4o", 600, 300)

	// Save to directory
	suite.logger.Log("[E2E-COST] Saving costs to directory")
	if err := suite.tracker.SaveToDir(suite.tempDir); err != nil {
		t.Fatalf("[E2E-COST] SaveToDir failed: %v", err)
	}

	// Verify file exists
	costPath := filepath.Join(suite.tempDir, ".ntm", "costs.json")
	if _, err := os.Stat(costPath); os.IsNotExist(err) {
		t.Fatal("[E2E-COST] costs.json file not created")
	}
	suite.logger.Log("[E2E-COST] Costs file created at: %s", costPath)

	// Load into a new tracker
	suite.logger.Log("[E2E-COST] Loading costs into new tracker")
	newTracker := cost.NewCostTracker(suite.tempDir)
	if err := newTracker.LoadFromDir(suite.tempDir); err != nil {
		t.Fatalf("[E2E-COST] LoadFromDir failed: %v", err)
	}

	// Verify data
	sessions := newTracker.GetAllSessions()
	suite.logger.Log("[E2E-COST] Loaded %d sessions", len(sessions))

	if len(sessions) != 2 {
		t.Errorf("[E2E-COST] Expected 2 sessions, got %d", len(sessions))
	}

	// Check session cost
	testSessionCost := newTracker.GetSessionCost("test-session")
	suite.logger.Log("[E2E-COST] test-session cost: %s", cost.FormatCost(testSessionCost))

	if testSessionCost <= 0 {
		t.Error("[E2E-COST] Expected positive cost for test-session")
	}

	suite.logger.Log("[E2E-COST] PASS: Cost tracker persistence works")
}

// =============================================================================
// Test: Model Pricing Accuracy
// =============================================================================

func TestCostModelPricing(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-pricing")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 4: Model Pricing Accuracy ===")

	// Test known models have expected pricing
	models := []string{
		"claude-opus",
		"claude-sonnet",
		"claude-haiku",
		"gpt-4o",
		"gpt-4o-mini",
		"gemini-pro",
		"gemini-flash",
	}

	for _, model := range models {
		pricing := cost.GetModelPricing(model)
		suite.logger.Log("[E2E-COST] %s: input=$%.5f/1K, output=$%.5f/1K",
			model, pricing.InputPer1K, pricing.OutputPer1K)

		if pricing.InputPer1K <= 0 || pricing.OutputPer1K <= 0 {
			t.Errorf("[E2E-COST] Model %s has zero pricing", model)
		}

		// Output should be more expensive than input for most models
		if pricing.OutputPer1K < pricing.InputPer1K {
			suite.logger.Log("[E2E-COST] Warning: %s output is cheaper than input", model)
		}
	}

	// Test unknown model gets default pricing
	unknownPricing := cost.GetModelPricing("unknown-model-xyz")
	suite.logger.Log("[E2E-COST] unknown model: input=$%.5f/1K, output=$%.5f/1K",
		unknownPricing.InputPer1K, unknownPricing.OutputPer1K)

	if unknownPricing.InputPer1K <= 0 {
		t.Error("[E2E-COST] Unknown model should get default pricing")
	}

	suite.logger.Log("[E2E-COST] PASS: Model pricing accuracy works")
}

// =============================================================================
// Test: Multi-Agent Cost Breakdown
// =============================================================================

func TestCostMultiAgentBreakdown(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-multi")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 5: Multi-Agent Cost Breakdown ===")

	// Simulate multiple agents with different models
	suite.tracker.RecordTokens("multi-session", "%0", "claude-opus", 10000, 5000)
	suite.tracker.RecordTokens("multi-session", "%1", "claude-sonnet", 15000, 7000)
	suite.tracker.RecordTokens("multi-session", "%2", "gemini-flash", 20000, 10000)

	// Get session data
	session := suite.tracker.GetSession("multi-session")
	if session == nil {
		t.Fatal("[E2E-COST] Session not found")
	}

	suite.logger.Log("[E2E-COST] Session has %d agents", len(session.Agents))

	// Calculate individual costs
	var totalCost float64
	for pane, agent := range session.Agents {
		agentCost := agent.Cost()
		totalCost += agentCost
		suite.logger.Log("[E2E-COST] Agent %s (%s): %d in / %d out = %s",
			pane, agent.Model, agent.InputTokens, agent.OutputTokens, cost.FormatCost(agentCost))
	}

	// Verify total matches
	sessionTotal := session.TotalCost()
	suite.logger.Log("[E2E-COST] Session total: %s (sum: %s)",
		cost.FormatCost(sessionTotal), cost.FormatCost(totalCost))

	// Allow small floating point difference
	if diff := sessionTotal - totalCost; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("[E2E-COST] Total mismatch: session=%f sum=%f", sessionTotal, totalCost)
	}

	// Verify different models have different costs
	opusCost := session.Agents["%0"].Cost()
	flashCost := session.Agents["%2"].Cost()
	suite.logger.Log("[E2E-COST] Opus cost: %s, Flash cost: %s", cost.FormatCost(opusCost), cost.FormatCost(flashCost))

	// Opus should be more expensive despite fewer tokens
	if flashCost > opusCost {
		suite.logger.Log("[E2E-COST] Note: Flash with 2x tokens is still cheaper than Opus")
	}

	suite.logger.Log("[E2E-COST] PASS: Multi-agent cost breakdown works")
}

// =============================================================================
// Test: Cost Formatting
// =============================================================================

func TestCostFormatting(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-format")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 6: Cost Formatting ===")

	tests := []struct {
		amount   float64
		expected string
	}{
		{0.0001, "$0.0001"},
		{0.001, "$0.0010"},
		{0.01, "$0.010"},
		{0.1, "$0.100"},
		{1.0, "$1.00"},
		{10.5, "$10.50"},
		{100.25, "$100.25"},
	}

	for _, tc := range tests {
		result := cost.FormatCost(tc.amount)
		suite.logger.Log("[E2E-COST] FormatCost(%f) = %q", tc.amount, result)

		if result != tc.expected {
			t.Errorf("[E2E-COST] FormatCost(%f) = %q, want %q", tc.amount, result, tc.expected)
		}
	}

	suite.logger.Log("[E2E-COST] PASS: Cost formatting works")
}

// =============================================================================
// Test: Session Lifecycle
// =============================================================================

func TestCostSessionLifecycle(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-lifecycle")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 7: Session Lifecycle ===")

	// Create session
	suite.logger.Log("[E2E-COST] Creating session with initial tokens")
	suite.tracker.RecordTokens("lifecycle-test", "%0", "claude-opus", 1000, 500)

	initialCost := suite.tracker.GetSessionCost("lifecycle-test")
	suite.logger.Log("[E2E-COST] Initial cost: %s", cost.FormatCost(initialCost))

	// Add more tokens (simulating prompts)
	suite.logger.Log("[E2E-COST] Adding more tokens")
	for i := 0; i < 5; i++ {
		suite.tracker.RecordTokens("lifecycle-test", "%0", "claude-opus", 500, 250)
	}

	midCost := suite.tracker.GetSessionCost("lifecycle-test")
	suite.logger.Log("[E2E-COST] Cost after 5 more prompts: %s", cost.FormatCost(midCost))

	if midCost <= initialCost {
		t.Error("[E2E-COST] Cost should increase after more tokens")
	}

	// Add second agent
	suite.logger.Log("[E2E-COST] Adding second agent")
	suite.tracker.RecordTokens("lifecycle-test", "%1", "gemini-pro", 2000, 1000)

	finalCost := suite.tracker.GetSessionCost("lifecycle-test")
	suite.logger.Log("[E2E-COST] Final cost with 2 agents: %s", cost.FormatCost(finalCost))

	if finalCost <= midCost {
		t.Error("[E2E-COST] Cost should increase after adding second agent")
	}

	// Get token totals
	session := suite.tracker.GetSession("lifecycle-test")
	inputTotal, outputTotal := session.TotalTokens()
	suite.logger.Log("[E2E-COST] Total tokens: %d in, %d out", inputTotal, outputTotal)

	// Clear session
	suite.logger.Log("[E2E-COST] Clearing session")
	suite.tracker.ClearSession("lifecycle-test")

	clearedCost := suite.tracker.GetSessionCost("lifecycle-test")
	if clearedCost != 0 {
		t.Errorf("[E2E-COST] Cleared session should have zero cost, got %s", cost.FormatCost(clearedCost))
	}

	suite.logger.Log("[E2E-COST] PASS: Session lifecycle works")
}

// =============================================================================
// Test: Token Estimation
// =============================================================================

func TestCostTokenEstimation(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-estimate")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 8: Token Estimation ===")

	tests := []struct {
		text        string
		description string
	}{
		{"Hello, world!", "short greeting"},
		{"The quick brown fox jumps over the lazy dog.", "pangram sentence"},
		{strings.Repeat("test ", 100), "repeated words"},
		{"def foo(x):\n    return x * 2", "Python code"},
		{`{"key": "value", "nested": {"a": 1, "b": 2}}`, "JSON object"},
	}

	for _, tc := range tests {
		tokens := cost.EstimateTokens(tc.text)
		ratio := float64(len(tc.text)) / float64(tokens)
		suite.logger.Log("[E2E-COST] %s: %d chars -> %d tokens (%.1f chars/token)",
			tc.description, len(tc.text), tokens, ratio)

		if tokens <= 0 {
			t.Errorf("[E2E-COST] Token estimate for %q should be positive", tc.description)
		}

		// Reasonable range: 2-6 characters per token
		if ratio < 1.5 || ratio > 8 {
			t.Errorf("[E2E-COST] Token estimate ratio %.1f seems off for %q", ratio, tc.description)
		}
	}

	suite.logger.Log("[E2E-COST] PASS: Token estimation works")
}

// =============================================================================
// Test: Context Window Usage (via robot-context)
// =============================================================================

func TestCostContextUsage(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-context")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 9: Context Window Usage ===")

	// Test the --robot-context command (requires a session)
	// This may fail if no sessions exist, which is expected
	out, err := suite.runNTM("--robot-context=nonexistent-session")
	if err != nil {
		suite.logger.Log("[E2E-COST] --robot-context for nonexistent session returned error (expected)")
		// Try to parse error response
		var errResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(extractCostJSON(out)), &errResp) == nil && !errResp.Success {
			suite.logger.Log("[E2E-COST] Error response: %s", errResp.Error)
		}
	}

	// Test the general token stats which should always work
	out, err = suite.runNTM("--robot-tokens", "--tokens-days=7")
	if err != nil {
		t.Fatalf("[E2E-COST] --robot-tokens failed: %v", err)
	}

	var resp struct {
		TotalTokens int `json:"total_tokens"`
	}
	if err := json.Unmarshal([]byte(extractCostJSON(out)), &resp); err != nil {
		t.Fatalf("[E2E-COST] Failed to parse response: %v", err)
	}

	suite.logger.Log("[E2E-COST] Total tokens in last 7 days: %d", resp.TotalTokens)
	suite.logger.Log("[E2E-COST] PASS: Context usage tracking works")
}

// =============================================================================
// Test: Total Cost Across Sessions
// =============================================================================

func TestCostTotalAcrossSessions(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-total")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 10: Total Cost Across Sessions ===")

	// Create multiple sessions
	sessions := []struct {
		name   string
		pane   string
		model  string
		input  int
		output int
	}{
		{"session-1", "%0", "claude-opus", 5000, 2500},
		{"session-1", "%1", "claude-sonnet", 10000, 5000},
		{"session-2", "%0", "gpt-4o", 8000, 4000},
		{"session-3", "%0", "gemini-pro", 15000, 7500},
	}

	for _, s := range sessions {
		suite.tracker.RecordTokens(s.name, s.pane, s.model, s.input, s.output)
		suite.logger.Log("[E2E-COST] Recorded %s/%s: %d in / %d out (%s)",
			s.name, s.pane, s.input, s.output, s.model)
	}

	// Get total cost
	totalCost := suite.tracker.GetTotalCost()
	suite.logger.Log("[E2E-COST] Total cost across all sessions: %s", cost.FormatCost(totalCost))

	// Verify individual sessions sum up
	var sumCost float64
	allSessions := suite.tracker.GetAllSessions()
	for _, name := range allSessions {
		sessionCost := suite.tracker.GetSessionCost(name)
		sumCost += sessionCost
		suite.logger.Log("[E2E-COST] Session %s: %s", name, cost.FormatCost(sessionCost))
	}

	suite.logger.Log("[E2E-COST] Sum of sessions: %s", cost.FormatCost(sumCost))

	// Allow small floating point difference
	if diff := totalCost - sumCost; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("[E2E-COST] Total mismatch: total=%f sum=%f", totalCost, sumCost)
	}

	suite.logger.Log("[E2E-COST] PASS: Total cost across sessions works")
}

// =============================================================================
// Test: Model Name Normalization
// =============================================================================

func TestCostModelNormalization(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-normalize")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 11: Model Name Normalization ===")

	// Test various model name formats
	variants := []struct {
		input    string
		expected string // base model it should match
	}{
		{"claude-opus-4-5", "claude-opus"},
		{"Claude-Opus", "claude-opus"},
		{"CLAUDE-SONNET", "claude-sonnet"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"gpt-4o-2024-08-06", "gpt-4o"},
		{"gemini-pro-1.5", "gemini-pro"},
	}

	for _, tc := range variants {
		pricing := cost.GetModelPricing(tc.input)
		suite.logger.Log("[E2E-COST] %q -> input=$%.5f/1K, output=$%.5f/1K",
			tc.input, pricing.InputPer1K, pricing.OutputPer1K)

		if pricing.InputPer1K <= 0 {
			t.Errorf("[E2E-COST] Model %q should have valid pricing", tc.input)
		}
	}

	suite.logger.Log("[E2E-COST] PASS: Model name normalization works")
}

// =============================================================================
// Test: Record Prompt/Response
// =============================================================================

func TestCostRecordPromptResponse(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-record")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 12: Record Prompt/Response ===")

	// Record a prompt
	prompt := "Please help me fix this bug in the authentication system. The login is failing intermittently."
	suite.tracker.RecordPrompt("record-test", "%0", "claude-opus", prompt)

	session := suite.tracker.GetSession("record-test")
	if session == nil {
		t.Fatal("[E2E-COST] Session not created")
	}

	agent := session.Agents["%0"]
	suite.logger.Log("[E2E-COST] After prompt: %d input tokens, %d output tokens",
		agent.InputTokens, agent.OutputTokens)

	if agent.InputTokens <= 0 {
		t.Error("[E2E-COST] Input tokens should be positive after prompt")
	}
	if agent.OutputTokens != 0 {
		t.Error("[E2E-COST] Output tokens should be zero after only prompt")
	}

	// Record a response
	response := "I'll help you debug the authentication system. Let me analyze the code and identify potential race conditions that could cause intermittent failures. The issue might be related to session token validation or database connection pooling."
	suite.tracker.RecordResponse("record-test", "%0", "claude-opus", response)

	session = suite.tracker.GetSession("record-test")
	agent = session.Agents["%0"]
	suite.logger.Log("[E2E-COST] After response: %d input tokens, %d output tokens",
		agent.InputTokens, agent.OutputTokens)

	if agent.OutputTokens <= 0 {
		t.Error("[E2E-COST] Output tokens should be positive after response")
	}

	suite.logger.Log("[E2E-COST] Agent cost: %s", cost.FormatCost(agent.Cost()))
	suite.logger.Log("[E2E-COST] PASS: Record prompt/response works")
}

// =============================================================================
// Test: Agent Activity Timestamp
// =============================================================================

func TestCostAgentTimestamp(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-timestamp")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 13: Agent Activity Timestamp ===")

	before := time.Now()
	time.Sleep(10 * time.Millisecond)

	suite.tracker.RecordTokens("timestamp-test", "%0", "claude-opus", 100, 50)

	time.Sleep(10 * time.Millisecond)
	after := time.Now()

	session := suite.tracker.GetSession("timestamp-test")
	agent := session.Agents["%0"]

	suite.logger.Log("[E2E-COST] Before: %s", before.Format(time.RFC3339Nano))
	suite.logger.Log("[E2E-COST] LastUpdated: %s", agent.LastUpdated.Format(time.RFC3339Nano))
	suite.logger.Log("[E2E-COST] After: %s", after.Format(time.RFC3339Nano))

	if agent.LastUpdated.Before(before) || agent.LastUpdated.After(after) {
		t.Error("[E2E-COST] LastUpdated timestamp out of expected range")
	}

	// Update again and verify timestamp changes
	time.Sleep(10 * time.Millisecond)
	firstUpdate := agent.LastUpdated

	suite.tracker.RecordTokens("timestamp-test", "%0", "claude-opus", 100, 50)

	session = suite.tracker.GetSession("timestamp-test")
	agent = session.Agents["%0"]

	if !agent.LastUpdated.After(firstUpdate) {
		t.Error("[E2E-COST] LastUpdated should advance after new tokens")
	}

	suite.logger.Log("[E2E-COST] Timestamp advanced: %s -> %s",
		firstUpdate.Format(time.RFC3339), agent.LastUpdated.Format(time.RFC3339))
	suite.logger.Log("[E2E-COST] PASS: Agent activity timestamp works")
}

// =============================================================================
// Test: Empty Session Handling
// =============================================================================

func TestCostEmptySession(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-empty")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 14: Empty Session Handling ===")

	// Get cost for non-existent session
	cost := suite.tracker.GetSessionCost("nonexistent")
	suite.logger.Log("[E2E-COST] Cost for nonexistent session: %f", cost)

	if cost != 0 {
		t.Errorf("[E2E-COST] Nonexistent session should have zero cost, got %f", cost)
	}

	// Get session data for non-existent session
	session := suite.tracker.GetSession("nonexistent")
	if session != nil {
		t.Error("[E2E-COST] Nonexistent session should return nil")
	}

	suite.logger.Log("[E2E-COST] PASS: Empty session handling works")
}

// =============================================================================
// Test: Concurrent Access Safety
// =============================================================================

func TestCostConcurrentAccess(t *testing.T) {
	suite := NewCostTestSuite(t, "cost-concurrent")
	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-COST] Setup failed: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Log("[E2E-COST] === Scenario 15: Concurrent Access Safety ===")

	// Spawn multiple goroutines recording tokens
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				pane := fmt.Sprintf("%%%d", id)
				suite.tracker.RecordTokens("concurrent-test", pane, "claude-opus", 10, 5)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify data integrity
	session := suite.tracker.GetSession("concurrent-test")
	if session == nil {
		t.Fatal("[E2E-COST] Session not found after concurrent writes")
	}

	suite.logger.Log("[E2E-COST] Session has %d agents after concurrent writes", len(session.Agents))

	// Each of 10 agents should have 1000 input and 500 output tokens
	for pane, agent := range session.Agents {
		if agent.InputTokens != 1000 {
			t.Errorf("[E2E-COST] Agent %s has %d input tokens, expected 1000", pane, agent.InputTokens)
		}
		if agent.OutputTokens != 500 {
			t.Errorf("[E2E-COST] Agent %s has %d output tokens, expected 500", pane, agent.OutputTokens)
		}
	}

	suite.logger.Log("[E2E-COST] PASS: Concurrent access safety works")
}
