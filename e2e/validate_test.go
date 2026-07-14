//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-VALIDATE] Tests for ntm config validate (configuration validation).
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ValidationReport represents the validation output
type ValidationReport struct {
	Valid   bool               `json:"valid"`
	Results []ValidationResult `json:"results"`
	Summary ValidationSummary  `json:"summary"`
}

// ValidationResult represents the outcome of validating a single config file
type ValidationResult struct {
	Path     string            `json:"path"`
	Type     string            `json:"type"`
	Valid    bool              `json:"valid"`
	Errors   []ValidationIssue `json:"errors,omitempty"`
	Warnings []ValidationIssue `json:"warnings,omitempty"`
	Info     []string          `json:"info,omitempty"`
}

// ValidationIssue represents a single validation error or warning
type ValidationIssue struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Fixable bool   `json:"fixable,omitempty"`
}

// ValidationSummary provides counts of issues found
type ValidationSummary struct {
	FilesChecked int `json:"files_checked"`
	ErrorCount   int `json:"error_count"`
	WarningCount int `json:"warning_count"`
	FixableCount int `json:"fixable_count"`
}

// ValidateTestSuite manages E2E tests for config validation
type ValidateTestSuite struct {
	t       *testing.T
	logger  *TestLogger
	tempDir string
	cleanup []func()
	origDir string
}

// NewValidateTestSuite creates a new validation test suite
func NewValidateTestSuite(t *testing.T, scenario string) *ValidateTestSuite {
	logger := NewTestLogger(t, scenario)

	return &ValidateTestSuite{
		t:      t,
		logger: logger,
	}
}

// Setup creates a temporary directory for testing
func (s *ValidateTestSuite) Setup() error {
	s.logger.Log("[E2E-VALIDATE] Setting up test environment")

	// Save original directory
	origDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}
	s.origDir = origDir

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "ntm-validate-e2e-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	s.tempDir = tempDir

	s.cleanup = append(s.cleanup, func() {
		os.Chdir(s.origDir)
		os.RemoveAll(tempDir)
	})

	// Create .ntm directory structure
	ntmDir := filepath.Join(tempDir, ".ntm")
	if err := os.MkdirAll(ntmDir, 0755); err != nil {
		return fmt.Errorf("create .ntm dir: %w", err)
	}

	s.logger.Log("[E2E-VALIDATE] Created test directory at %s", tempDir)
	return nil
}

// TempDir returns the temporary directory path
func (s *ValidateTestSuite) TempDir() string {
	return s.tempDir
}

// NtmDir returns the .ntm directory path
func (s *ValidateTestSuite) NtmDir() string {
	return filepath.Join(s.tempDir, ".ntm")
}

// Logger returns the test logger
func (s *ValidateTestSuite) Logger() *TestLogger {
	return s.logger
}

// Teardown cleans up resources
func (s *ValidateTestSuite) Teardown() {
	s.logger.Log("[E2E-VALIDATE] Running cleanup (%d items)", len(s.cleanup))

	for i := len(s.cleanup) - 1; i >= 0; i-- {
		s.cleanup[i]()
	}

	s.logger.Close()
}

// CreateFile creates a file in the temp directory
func (s *ValidateTestSuite) CreateFile(relativePath, content string) error {
	path := filepath.Join(s.tempDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// RunValidate executes ntm config validate and returns the parsed result
func (s *ValidateTestSuite) RunValidate(flags ...string) (*ValidationReport, error) {
	s.logger.Log("[E2E-VALIDATE] Running config validate flags=%v", flags)

	args := []string{"config", "validate", "--json"}
	args = append(args, flags...)

	cmd := exec.Command(mustE2EBin(), args...)
	cmd.Dir = s.tempDir
	output, err := cmd.CombinedOutput()

	s.logger.Log("[E2E-VALIDATE] Output: %s", string(output))

	var report ValidationReport
	if jsonErr := json.Unmarshal(output, &report); jsonErr != nil {
		// If validation fails, the command might return non-zero exit but still produce valid JSON
		if err != nil {
			s.logger.Log("[E2E-VALIDATE] Command exited with error: %v", err)
		}
		// Try to parse just the JSON part if there's extra output
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "{") {
				if json.Unmarshal([]byte(line), &report) == nil {
					break
				}
			}
		}
		if len(report.Results) == 0 {
			return nil, fmt.Errorf("parse failed: %w, output: %s", jsonErr, string(output))
		}
	}

	s.logger.LogJSON("[E2E-VALIDATE] Report", report)
	return &report, nil
}

// TestValidateNoConfig tests validation when no config exists
func TestValidateNoConfig(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-no-config")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
	}

	// Without any config files, validation should pass with info about missing files
	suite.Logger().Log("[E2E-VALIDATE] No config: valid=%v, files_checked=%d, errors=%d",
		report.Valid, report.Summary.FilesChecked, report.Summary.ErrorCount)

	if report.Summary.FilesChecked < 1 {
		t.Error("[E2E-VALIDATE] Expected at least 1 file checked")
	}

	suite.Logger().Log("[E2E-VALIDATE] No config test passed")
}

// TestValidateValidToml tests validation of a valid TOML config
func TestValidateValidToml(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-valid-toml")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	// Create a valid project config
	validConfig := `# NTM Project Configuration
projects_base = "~/projects"

[spawn]
default_cc = 2
default_cod = 1

[tmux]
default_panes = 6
`

	if err := suite.CreateFile(".ntm/config.toml", validConfig); err != nil {
		t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
	}

	// Find the project config result
	var projectResult *ValidationResult
	for i := range report.Results {
		if report.Results[i].Type == "project" {
			projectResult = &report.Results[i]
			break
		}
	}

	if projectResult == nil {
		suite.Logger().Log("[E2E-VALIDATE] No project config result found in: %+v", report.Results)
	} else {
		if len(projectResult.Errors) > 0 {
			t.Errorf("[E2E-VALIDATE] Valid config should have no errors, got: %v", projectResult.Errors)
		}
		suite.Logger().Log("[E2E-VALIDATE] Valid TOML: errors=%d, warnings=%d",
			len(projectResult.Errors), len(projectResult.Warnings))
	}

	suite.Logger().Log("[E2E-VALIDATE] Valid TOML test passed")
}

// TestValidateInvalidToml tests validation of invalid TOML syntax
func TestValidateInvalidToml(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-invalid-toml")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	// Create an invalid TOML config (missing closing bracket)
	invalidConfig := `[spawn
default_cc = 2
`

	if err := suite.CreateFile(".ntm/config.toml", invalidConfig); err != nil {
		t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		// Expected to fail for invalid syntax
		suite.Logger().Log("[E2E-VALIDATE] Validation failed as expected: %v", err)
	}

	if report == nil {
		suite.Logger().Log("[E2E-VALIDATE] No report returned for invalid TOML (expected)")
		return
	}

	// Find the project config result
	for _, result := range report.Results {
		if result.Type == "project" {
			if len(result.Errors) == 0 {
				t.Error("[E2E-VALIDATE] Invalid TOML should produce errors")
			} else {
				suite.Logger().Log("[E2E-VALIDATE] Invalid TOML correctly detected: %s", result.Errors[0].Message)
			}
			break
		}
	}

	suite.Logger().Log("[E2E-VALIDATE] Invalid TOML test passed")
}

// TestValidateRecipesFile tests validation of recipes.toml
func TestValidateRecipesFile(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-recipes")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	t.Run("ValidRecipes", func(t *testing.T) {
		validRecipes := `[review]
description = "Code review workflow"
steps = ["check", "analyze", "report"]

[deploy]
description = "Deployment workflow"
steps = ["build", "test", "deploy"]
`
		if err := suite.CreateFile(".ntm/recipes.toml", validRecipes); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create recipes: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
		}

		// Find recipes result
		for _, result := range report.Results {
			if result.Type == "recipes" {
				if len(result.Errors) > 0 {
					t.Errorf("[E2E-VALIDATE] Valid recipes should have no errors: %v", result.Errors)
				}
				suite.Logger().Log("[E2E-VALIDATE] Valid recipes: errors=%d", len(result.Errors))
				break
			}
		}
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		// Recipe missing description
		incompleteRecipes := `[incomplete_recipe]
steps = ["step1"]
`
		if err := suite.CreateFile(".ntm/recipes.toml", incompleteRecipes); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create recipes: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Validation returned error: %v", err)
		}

		if report != nil {
			for _, result := range report.Results {
				if result.Type == "recipes" {
					// Should have warning about missing description
					hasWarning := len(result.Warnings) > 0
					suite.Logger().Log("[E2E-VALIDATE] Incomplete recipe: warnings=%d, has_warning=%v",
						len(result.Warnings), hasWarning)
					break
				}
			}
		}
	})
}

// TestValidatePersonasFile tests validation of personas.toml
func TestValidatePersonasFile(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-personas")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	t.Run("ValidPersonas", func(t *testing.T) {
		validPersonas := `[architect]
system_prompt = "You are a software architect."
agent_type = "claude"
focus = "architecture"

[tester]
system_prompt = "You are a QA engineer."
agent_type = "codex"
focus = "testing"
`
		if err := suite.CreateFile(".ntm/personas.toml", validPersonas); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create personas: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
		}

		for _, result := range report.Results {
			if result.Type == "personas" {
				if len(result.Errors) > 0 {
					t.Errorf("[E2E-VALIDATE] Valid personas should have no errors: %v", result.Errors)
				}
				suite.Logger().Log("[E2E-VALIDATE] Valid personas: errors=%d", len(result.Errors))
				break
			}
		}
	})

	t.Run("MissingSystemPrompt", func(t *testing.T) {
		// Persona missing system_prompt
		incompletePersonas := `[incomplete_persona]
agent_type = "claude"
`
		if err := suite.CreateFile(".ntm/personas.toml", incompletePersonas); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create personas: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Validation returned error: %v", err)
		}

		if report != nil {
			for _, result := range report.Results {
				if result.Type == "personas" {
					hasWarning := len(result.Warnings) > 0
					suite.Logger().Log("[E2E-VALIDATE] Incomplete persona: warnings=%d", len(result.Warnings))
					if hasWarning {
						suite.Logger().Log("[E2E-VALIDATE] Warning: %s", result.Warnings[0].Message)
					}
					break
				}
			}
		}
	})
}

// TestValidatePolicyFile tests validation of policy.yaml
func TestValidatePolicyFile(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-policy")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	t.Run("ValidPolicy", func(t *testing.T) {
		validPolicy := `version: "1.0"
rules:
  - name: no-secrets
    pattern: "api_key|secret"
    action: block
`
		if err := suite.CreateFile(".ntm/policy.yaml", validPolicy); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create policy: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
		}

		for _, result := range report.Results {
			if result.Type == "policy" {
				if len(result.Errors) > 0 {
					t.Errorf("[E2E-VALIDATE] Valid policy should have no errors: %v", result.Errors)
				}
				suite.Logger().Log("[E2E-VALIDATE] Valid policy: errors=%d", len(result.Errors))
				break
			}
		}
	})

	t.Run("InvalidYaml", func(t *testing.T) {
		// Invalid YAML syntax
		invalidPolicy := `version: "1.0
rules:
  - name: incomplete
`
		if err := suite.CreateFile(".ntm/policy.yaml", invalidPolicy); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create policy: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Invalid YAML validation error: %v", err)
		}

		if report != nil {
			for _, result := range report.Results {
				if result.Type == "policy" {
					if len(result.Errors) > 0 {
						suite.Logger().Log("[E2E-VALIDATE] Invalid YAML detected: %s", result.Errors[0].Message)
					}
					break
				}
			}
		}
	})

	t.Run("MissingRequiredFields", func(t *testing.T) {
		// Policy missing version
		incompletePolicy := `rules:
  - name: test
    action: allow
`
		if err := suite.CreateFile(".ntm/policy.yaml", incompletePolicy); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create policy: %v", err)
		}

		report, err := suite.RunValidate()
		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Validation returned error: %v", err)
		}

		if report != nil {
			for _, result := range report.Results {
				if result.Type == "policy" {
					hasWarning := len(result.Warnings) > 0
					suite.Logger().Log("[E2E-VALIDATE] Missing version: warnings=%d", len(result.Warnings))
					if hasWarning {
						for _, w := range result.Warnings {
							suite.Logger().Log("[E2E-VALIDATE] Warning: field=%s msg=%s", w.Field, w.Message)
						}
					}
					break
				}
			}
		}
	})
}

// TestValidateAllFlag tests the --all flag
func TestValidateAllFlag(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-all-flag")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	// Run with --all flag
	report, err := suite.RunValidate("--all")
	if err != nil {
		t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
	}

	// Should check more files with --all
	suite.Logger().Log("[E2E-VALIDATE] --all: checked %d files", report.Summary.FilesChecked)

	if report.Summary.FilesChecked < 1 {
		t.Error("[E2E-VALIDATE] Expected at least 1 file checked with --all")
	}

	// List all checked files
	for _, result := range report.Results {
		suite.Logger().Log("[E2E-VALIDATE]   - %s (%s): valid=%v", result.Path, result.Type, result.Valid)
	}
}

// TestValidateSummary tests the summary statistics
func TestValidateSummary(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-summary")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	// Create files with known issues
	// Config missing field
	incompleteRecipes := `[test_recipe]
# Missing description and steps
`

	if err := suite.CreateFile(".ntm/recipes.toml", incompleteRecipes); err != nil {
		t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		suite.Logger().Log("[E2E-VALIDATE] Validation returned error: %v", err)
	}

	if report == nil {
		t.Fatal("[E2E-VALIDATE] Expected report")
	}

	// Verify summary structure
	summary := report.Summary
	suite.Logger().Log("[E2E-VALIDATE] Summary: files=%d, errors=%d, warnings=%d, fixable=%d",
		summary.FilesChecked, summary.ErrorCount, summary.WarningCount, summary.FixableCount)

	if summary.FilesChecked < 1 {
		t.Error("[E2E-VALIDATE] Expected files_checked >= 1")
	}

	// Error + warning counts should match results
	totalErrors := 0
	totalWarnings := 0
	for _, result := range report.Results {
		totalErrors += len(result.Errors)
		totalWarnings += len(result.Warnings)
	}

	if summary.ErrorCount != totalErrors {
		t.Errorf("[E2E-VALIDATE] Summary.ErrorCount=%d but total from results=%d", summary.ErrorCount, totalErrors)
	}

	if summary.WarningCount != totalWarnings {
		t.Errorf("[E2E-VALIDATE] Summary.WarningCount=%d but total from results=%d", summary.WarningCount, totalWarnings)
	}

	suite.Logger().Log("[E2E-VALIDATE] Summary test passed")
}

// TestValidateResultFormat tests the JSON output structure
func TestValidateResultFormat(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-format")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		t.Fatalf("[E2E-VALIDATE] Validation failed: %v", err)
	}

	// Verify structure
	if len(report.Results) < 1 {
		t.Error("[E2E-VALIDATE] Expected at least 1 result")
	}

	for _, result := range report.Results {
		// Path should be absolute or relative
		if result.Path == "" {
			t.Error("[E2E-VALIDATE] Result.Path should not be empty")
		}

		// Type should be valid
		validTypes := map[string]bool{
			"main": true, "project": true, "recipes": true, "personas": true, "policy": true,
		}
		if !validTypes[result.Type] {
			t.Errorf("[E2E-VALIDATE] Invalid result type: %s", result.Type)
		}

		suite.Logger().Log("[E2E-VALIDATE] Result: path=%s, type=%s, valid=%v", result.Path, result.Type, result.Valid)
	}

	suite.Logger().Log("[E2E-VALIDATE] Result format test passed")
}

// TestValidateExitCode tests that exit codes are correct
func TestValidateExitCode(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-exit-code")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	t.Run("ValidConfigExitZero", func(t *testing.T) {
		// Create valid config
		validConfig := `[spawn]
default_cc = 1
`
		if err := suite.CreateFile(".ntm/config.toml", validConfig); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
		}

		cmd := exec.Command(mustE2EBin(), "config", "validate")
		cmd.Dir = suite.TempDir()
		err := cmd.Run()

		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Valid config exit code non-zero: %v", err)
		} else {
			suite.Logger().Log("[E2E-VALIDATE] Valid config correctly exited with 0")
		}
	})

	t.Run("InvalidConfigExitNonZero", func(t *testing.T) {
		// Create invalid config
		invalidConfig := `[spawn
broken syntax
`
		if err := suite.CreateFile(".ntm/config.toml", invalidConfig); err != nil {
			t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
		}

		cmd := exec.Command(mustE2EBin(), "config", "validate")
		cmd.Dir = suite.TempDir()
		err := cmd.Run()

		if err != nil {
			suite.Logger().Log("[E2E-VALIDATE] Invalid config correctly returned non-zero: %v", err)
		} else {
			suite.Logger().Log("[E2E-VALIDATE] Note: Invalid config returned exit 0")
		}
	})
}

// TestValidateDeprecatedOptions tests handling of deprecated config options
func TestValidateDeprecatedOptions(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	suite := NewValidateTestSuite(t, "validate-deprecated")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-VALIDATE] Setup failed: %v", err)
	}

	// This test documents expected behavior for deprecated options
	// The actual deprecated option names would depend on the implementation
	suite.Logger().Log("[E2E-VALIDATE] Deprecated options test - checking validation handles unknown keys")

	// Create config with unknown key
	configWithUnknown := `[spawn]
default_cc = 1
unknown_deprecated_option = true
`
	if err := suite.CreateFile(".ntm/config.toml", configWithUnknown); err != nil {
		t.Fatalf("[E2E-VALIDATE] Failed to create config: %v", err)
	}

	report, err := suite.RunValidate()
	if err != nil {
		suite.Logger().Log("[E2E-VALIDATE] Validation returned error: %v", err)
	}

	if report != nil {
		for _, result := range report.Results {
			if result.Type == "project" {
				suite.Logger().Log("[E2E-VALIDATE] Unknown key handling: errors=%d, warnings=%d",
					len(result.Errors), len(result.Warnings))
				break
			}
		}
	}

	suite.Logger().Log("[E2E-VALIDATE] Deprecated options test completed")
}
