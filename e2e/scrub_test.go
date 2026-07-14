//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SCRUB] Tests for ntm scrub secret scanning.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type scrubResult struct {
	Roots        []string       `json:"roots"`
	FilesScanned int            `json:"files_scanned"`
	Findings     []scrubFinding `json:"findings"`
	Warnings     []string       `json:"warnings,omitempty"`
}

type scrubFinding struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Preview  string `json:"preview"`
}

func TestE2E_ScrubScansArtifactsSafely(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	logger := NewTestLogger(t, "scrub")
	t.Cleanup(func() {
		logger.Close()
	})

	tempDir := t.TempDir()
	artifactPath := filepath.Join(tempDir, "artifact.txt")
	content := strings.Join([]string{
		"openai_key=" + fakeOpenAIKey,
		"github_token=" + fakeGitHubToken,
		fakeAWSSecret,
		fakePassword,
		fakeJWT,
	}, "\n")

	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		t.Fatalf("[E2E-SCRUB] failed to write artifact: %v", err)
	}

	logger.Log("[E2E-SCRUB] Created artifact: %s", artifactPath)

	cmd := exec.Command(mustE2EBin(), "scrub", "--path", tempDir, "--format", "json")
	cmd.Env = append(os.Environ(), "HOME="+tempDir, "XDG_CONFIG_HOME="+tempDir)
	output, err := cmd.CombinedOutput()
	logger.Log("[E2E-SCRUB] Running: ntm scrub --path %s --format json", tempDir)
	logger.Log("[E2E-SCRUB] Output: %s", string(output))
	if err != nil {
		t.Fatalf("[E2E-SCRUB] scrub command failed: %v", err)
	}

	var res scrubResult
	if jsonErr := json.Unmarshal(output, &res); jsonErr != nil {
		t.Fatalf("[E2E-SCRUB] failed to parse JSON: %v", jsonErr)
	}
	logger.Log("[E2E-SCRUB] roots=%v files=%d findings=%d", res.Roots, res.FilesScanned, len(res.Findings))

	if res.FilesScanned < 1 {
		t.Fatalf("[E2E-SCRUB] expected files_scanned >= 1, got %d", res.FilesScanned)
	}
	if len(res.Findings) == 0 {
		t.Fatalf("[E2E-SCRUB] expected findings, got 0")
	}

	secretMarkers := []string{fakeOpenAIKey, fakeGitHubToken, fakeAWSSecret, fakePassword, fakeJWT}
	categorySet := make(map[string]struct{})

	for _, f := range res.Findings {
		categorySet[f.Category] = struct{}{}
		if f.Path == "" {
			t.Fatalf("[E2E-SCRUB] empty path in finding")
		}
		if !strings.HasPrefix(f.Path, tempDir) {
			t.Fatalf("[E2E-SCRUB] finding path outside temp dir: %s", f.Path)
		}
		if f.Preview == "" {
			t.Fatalf("[E2E-SCRUB] empty preview for %s", f.Path)
		}
		if !strings.Contains(f.Preview, redactedMarker) {
			t.Fatalf("[E2E-SCRUB] preview missing redaction marker: %s", f.Preview)
		}
		for _, secret := range secretMarkers {
			if strings.Contains(f.Preview, secret) {
				t.Fatalf("[E2E-SCRUB] preview leaked raw secret: %s", secret)
			}
		}
		logger.Log("[E2E-SCRUB] finding category=%s line=%d preview=%s", f.Category, f.Line, f.Preview)
	}

	if _, ok := categorySet["OPENAI_KEY"]; !ok {
		t.Fatalf("[E2E-SCRUB] expected OPENAI_KEY finding")
	}
	if _, ok := categorySet["PASSWORD"]; !ok {
		t.Fatalf("[E2E-SCRUB] expected PASSWORD finding")
	}

	if len(res.Warnings) > 0 {
		logger.Log("[E2E-SCRUB] warnings: %v", res.Warnings)
	}
}
