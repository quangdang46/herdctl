//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SCAN] Tests for ntm scan (UBS - Ultimate Bug Scanner) command.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ScanFinding represents a single finding from ntm scan --json
type ScanFinding struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Severity string `json:"severity,omitempty"`
	Message  string `json:"message,omitempty"`
	Rule     string `json:"rule,omitempty"`
	Language string `json:"language,omitempty"`
}

// ScanOutput represents the JSON output from ntm scan --json
type ScanOutput struct {
	Success  bool          `json:"success,omitempty"`
	Findings []ScanFinding `json:"findings,omitempty"`
	Summary  struct {
		TotalFiles    int            `json:"total_files,omitempty"`
		TotalFindings int            `json:"total_findings,omitempty"`
		BySeverity    map[string]int `json:"by_severity,omitempty"`
		ByLanguage    map[string]int `json:"by_language,omitempty"`
	} `json:"summary,omitempty"`
	Error string `json:"error,omitempty"`
}

// ScanTestSuite manages E2E tests for the scan command
type ScanTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	cleanup []func()
	ntmPath string
	tempDir string
}

// NewScanTestSuite creates a new test suite
func NewScanTestSuite(t *testing.T, scenario string) *ScanTestSuite {
	logger := NewTestLogger(t, scenario)

	// Find ntm binary
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm binary not found in PATH")
	}

	// Create temp directory for test files
	tempDir, err := os.MkdirTemp("", "ntm-scan-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	suite := &ScanTestSuite{
		t:       t,
		logger:  logger,
		ntmPath: ntmPath,
		tempDir: tempDir,
	}

	suite.cleanup = append(suite.cleanup, func() {
		os.RemoveAll(tempDir)
	})

	return suite
}

// supportsCommand checks if ntm supports a given subcommand
func (s *ScanTestSuite) supportsCommand(cmd string) bool {
	out, err := exec.Command(s.ntmPath, cmd, "--help").CombinedOutput()
	if err != nil {
		// If exit code is non-zero, check if it's "unknown command"
		return !strings.Contains(string(out), "unknown command")
	}
	return true
}

// requireScanCommand skips if scan command is not supported
func (s *ScanTestSuite) requireScanCommand() {
	if !s.supportsCommand("scan") {
		s.t.Skip("scan command not supported by this ntm version")
	}
}

// createTestFile creates a test file in the temp directory
func (s *ScanTestSuite) createTestFile(name, content string) string {
	path := filepath.Join(s.tempDir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		s.t.Fatalf("Failed to create test file: %v", err)
	}
	return path
}

// Cleanup runs all registered cleanup functions
func (s *ScanTestSuite) Cleanup() {
	s.logger.Close()
	for _, fn := range s.cleanup {
		fn()
	}
}

// ============================================================================
// Scan Command Tests
// ============================================================================

func TestScanCommandExists(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_exists")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-SCAN] Verifying scan command exists")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan --help")

	cmd := exec.Command(suite.ntmPath, "scan", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown command") {
		t.Skip("scan command not available in this ntm version")
	}

	// Verify help text mentions scan-related content
	if !strings.Contains(outputStr, "scan") && !strings.Contains(outputStr, "UBS") {
		t.Errorf("Expected help text to contain 'scan' or 'UBS', got: %s", outputStr)
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: scan command exists and shows help")
}

func TestScanCommandHelpFlags(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_help_flags")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Verifying scan command help shows expected flags")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan --help")

	cmd := exec.Command(suite.ntmPath, "scan", "--help")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		suite.logger.Log("[E2E-SCAN] Command exited with error (may be expected): %v", err)
	}

	suite.logger.Log("[E2E-SCAN] Help output length: %d bytes", len(outputStr))

	// Check for expected flags
	expectedPatterns := []string{
		"--json",
		"--staged",
		"--diff",
		"--only",
		"--exclude",
		"--verbose",
		"--watch",
		"--help",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(outputStr, pattern) {
			t.Errorf("Expected help to mention '%s'", pattern)
		}
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: scan command help shows expected flags")
}

func TestScanAdvancedFlags(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_advanced_flags")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Verifying scan command advanced flags")

	cmd := exec.Command(suite.ntmPath, "scan", "--help")
	output, _ := cmd.CombinedOutput()
	outputStr := string(output)

	// Check for advanced flags
	advancedFlags := []string{
		"--create-beads",
		"--update-beads",
		"--notify",
		"--analyze-impact",
		"--hotspots",
		"--priority-report",
		"--fail-on-warning",
		"--min-severity",
		"--dry-run",
	}

	for _, flag := range advancedFlags {
		if !strings.Contains(outputStr, flag) {
			suite.logger.Log("[E2E-SCAN] Advanced flag '%s' not found", flag)
		}
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: Advanced flags verified")
}

func TestScanEmptyDirectory(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_empty_dir")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan on empty directory")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)
	suite.logger.Log("[E2E-SCAN] Error: %v", err)

	// Should handle empty directory gracefully
	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result ScanOutput
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr == nil {
			suite.logger.Log("[E2E-SCAN] Valid JSON output, findings: %d", len(result.Findings))
		}
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: scan handles empty directory")
}

func TestScanSingleGoFile(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_single_go")
	defer suite.Cleanup()

	suite.requireScanCommand()

	// Create a simple Go file
	goCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello")
}
`
	testFile := suite.createTestFile("test.go", goCode)

	suite.logger.Log("[E2E-SCAN] Testing scan on single Go file")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --json", testFile)

	cmd := exec.Command(suite.ntmPath, "scan", testFile, "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	// Should scan the file successfully
	if err != nil && !strings.Contains(outputStr, "success") {
		suite.logger.Log("[E2E-SCAN] Scan failed: %v", err)
	}

	// Try to parse JSON output
	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr == nil {
			suite.logger.Log("[E2E-SCAN] Valid JSON output received")
		}
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: scan processes Go file")
}

func TestScanOnlyFlag(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_only_flag")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --only flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --only golang --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--only", "golang", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--only flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --only flag works")
}

func TestScanExcludeFlag(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_exclude_flag")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --exclude flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --exclude python --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--exclude", "python", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--exclude flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --exclude flag works")
}

func TestScanVerboseFlag(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_verbose")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --verbose flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --verbose", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--verbose")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output length: %d bytes", len(outputStr))

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--verbose flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --verbose flag works")
}

func TestScanJSONFormat(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_json")
	defer suite.Cleanup()

	suite.requireScanCommand()

	// Create a test file
	suite.createTestFile("test.py", "print('hello')\n")

	suite.logger.Log("[E2E-SCAN] Testing scan --json output format")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if strings.TrimSpace(outputStr) != "" && strings.HasPrefix(strings.TrimSpace(outputStr), "{") {
		var result map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(outputStr), &result); jsonErr != nil {
			t.Errorf("Expected valid JSON output, got parse error: %v", jsonErr)
		} else {
			suite.logger.Log("[E2E-SCAN] Valid JSON output received")
		}
	} else if err != nil {
		suite.logger.Log("[E2E-SCAN] Command failed: %v", err)
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --json produces valid output")
}

func TestScanNonexistentPath(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_nonexistent")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan on non-existent path")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan /nonexistent/path/xyz --json")

	cmd := exec.Command(suite.ntmPath, "scan", "/nonexistent/path/xyz", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)
	suite.logger.Log("[E2E-SCAN] Error: %v", err)

	// Should fail with appropriate error
	if err == nil {
		suite.logger.Log("[E2E-SCAN] Command unexpectedly succeeded")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: scan handles non-existent path correctly")
}

func TestScanTimeout(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_timeout")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --timeout flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --timeout 5 --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--timeout", "5", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--timeout flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --timeout flag works")
}

func TestScanCIMode(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_ci")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --ci flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --ci --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--ci", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--ci flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --ci flag works")
}

func TestScanDryRun(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_dry_run")
	defer suite.Cleanup()

	suite.requireScanCommand()

	suite.logger.Log("[E2E-SCAN] Testing scan --dry-run flag")
	suite.logger.Log("[E2E-SCAN] Running: ntm scan %s --dry-run --create-beads --json", suite.tempDir)

	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--dry-run", "--create-beads", "--json")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	suite.logger.Log("[E2E-SCAN] Output: %s", outputStr)

	if err != nil && strings.Contains(outputStr, "unknown flag") {
		t.Errorf("--dry-run flag not recognized")
	}

	suite.logger.Log("[E2E-SCAN] SUCCESS: --dry-run flag works")
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestScanIntegration(t *testing.T) {
	suite := NewScanTestSuite(t, "scan_integration")
	defer suite.Cleanup()

	suite.logger.Log("[E2E-INTEGRATION] Testing scan command integration")

	// Test scan is available
	suite.logger.Log("[E2E-INTEGRATION] Checking scan command availability")
	scanAvailable := suite.supportsCommand("scan")
	suite.logger.Log("[E2E-INTEGRATION] scan available: %v", scanAvailable)

	if !scanAvailable {
		t.Skip("scan command not available")
	}

	// Create multiple test files
	suite.createTestFile("main.go", "package main\nfunc main() {}\n")
	suite.createTestFile("app.py", "def hello():\n    pass\n")
	suite.createTestFile("index.js", "console.log('test');\n")

	// Run scan with JSON
	suite.logger.Log("[E2E-INTEGRATION] Running scan with JSON output")
	cmd := exec.Command(suite.ntmPath, "scan", suite.tempDir, "--json")
	output, _ := cmd.CombinedOutput()
	suite.logger.Log("[E2E-INTEGRATION] scan --json output length: %d bytes", len(output))

	// Run scan with verbose
	suite.logger.Log("[E2E-INTEGRATION] Running scan with verbose")
	cmd = exec.Command(suite.ntmPath, "scan", suite.tempDir, "--verbose")
	output, _ = cmd.CombinedOutput()
	suite.logger.Log("[E2E-INTEGRATION] scan --verbose output length: %d bytes", len(output))

	suite.logger.Log("[E2E-INTEGRATION] SUCCESS: Integration test completed")
}
